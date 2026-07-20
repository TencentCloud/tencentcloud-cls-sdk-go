package consumer

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	cls "github.com/tencentcloud/tencentcloud-cls-sdk-go"
)

// ConsumerWorker consumer Worker main process
// responsible for managing parallel consumption of multiple partitions
type ConsumerWorker struct {
	ConsumerClient              *ConsumerClient
	ConsumerOption              *ConsumerOption
	Processor                   Processor
	ProcessorLock               sync.Mutex
	PartitionWorkers            map[string]*PartitionConsumerWorker
	PartitionWorkersLock        sync.RWMutex
	HeartbeatWorker             *HeartbeatWorker
	isShutdown                  bool
	ShutdownLock                sync.Mutex
	LastOwnedConsumerFinishTime int64 // all partitions completed timestamp
	Logger                      cls.Logger
	ctx                         context.Context
	cancel                      context.CancelFunc
}

// ConsumerOption consumer configuration options
type ConsumerOption struct {
	Endpoint    string
	AccessKeyID string
	AccessKey   string
	Region      string
	// Internal: if true, the underlying YunApi client uses the VPC internal endpoint
	// "cls.internal.tencentcloudapi.com" for cloud API calls (consumer group / heartbeat
	// / offsets). Region is still required and is sent via the X-TC-Region request header.
	Internal             bool
	LogsetID             string
	TopicIDs             []string
	ConsumerGroup        string
	ConsumerName         string
	HeartbeatInterval    int
	DataFetchInterval    int
	OffsetStartTime      string
	MaxFetchLogGroupSize int
	OffsetEndTime        string
	ConsumerGroupTimeout int
	StartTime            int64
	EndTime              int64
	// Query is the optional DSL pre-filter expression sent with every PullLogs request,
	// e.g. log_keep(op_and(op_gt(v("status"), 400), str_exist(v("message"), "access failed"))).
	// Only logs that match the expression are returned by the server. Leave empty to
	// disable server-side filtering. See:
	// https://cloud.tencent.com/document/product/614/37908
	Query                string
}

// NewConsumerWorker create consumer Worker
func NewConsumerWorker(consumerOption *ConsumerOption, processor Processor) *ConsumerWorker {
	// Always pass the real region down: the SDK uses Region to fill the X-TC-Region
	// public request header (required by the server). Internal endpoint switching is
	// handled inside NewYunApiLogClient based on the internal flag.
	yunapi := cls.NewYunApiLogClient(
		consumerOption.AccessKeyID,
		consumerOption.AccessKey,
		consumerOption.Internal, "", "",
		consumerOption.Region,
	)

	pullLogs := cls.NewPullLogsClient(
		consumerOption.Endpoint,
		consumerOption.AccessKeyID,
		consumerOption.AccessKey,
		"", "",
	)

	consumerClient := NewConsumerClient(
		yunapi,
		pullLogs,
		consumerOption.LogsetID,
		consumerOption.TopicIDs,
		consumerOption.ConsumerGroup,
		consumerOption.ConsumerName,
		consumerOption.Region,
	)
	// create context
	ctx, cancel := context.WithCancel(context.Background())
	return &ConsumerWorker{
		ConsumerClient:              consumerClient,
		ConsumerOption:              consumerOption,
		Processor:                   processor,
		PartitionWorkers:            make(map[string]*PartitionConsumerWorker),
		isShutdown:                  false,
		LastOwnedConsumerFinishTime: 0, // initialize intelligent stop timestamp
		Logger:                      cls.GetZapLoggerAdapter(),
		ctx:                         ctx,
		cancel:                      cancel,
	}
}

// Run start consumer Worker with context
func (cw *ConsumerWorker) Run(ctx context.Context) error {
	cw.ShutdownLock.Lock()
	if cw.isShutdown {
		cw.ShutdownLock.Unlock()
		return fmt.Errorf("consumer worker is already shutdown")
	}
	// Release the placeholder context created in the constructor before overwriting it,
	// otherwise its cancel func leaks. Safe to cancel here: run() has not started yet.
	if cw.cancel != nil {
		cw.cancel()
	}
	// merge context
	mergedCtx, cancel := context.WithCancel(ctx)
	cw.ctx = mergedCtx
	cw.cancel = cancel
	cw.ShutdownLock.Unlock()
	return cw.Start()
}

// Start start consumer Worker
func (cw *ConsumerWorker) Start() error {
	cw.Logger.Info("Starting consumer worker",
		cls.Field{Key: "consumer_group", Value: cw.ConsumerOption.ConsumerGroup},
		cls.Field{Key: "consumer_name", Value: cw.ConsumerOption.ConsumerName},
	)

	// 1. create consumer group
	err := cw.createConsumerGroup()
	if err != nil {
		cw.Logger.Error("Failed to create consumer group",
			cls.Field{Key: "error", Value: err.Error()},
		)
		return err
	}

	// 2. start heartbeat Worker
	cw.HeartbeatWorker = NewHeartbeatWorker(cw.ConsumerClient, cw.ConsumerOption)
	cw.HeartbeatWorker.Run(cw.ctx)

	// 3. start main consumption loop
	go cw.run()

	cw.Logger.Info("Consumer worker started successfully")
	return nil
}

// run main consumption loop
func (cw *ConsumerWorker) run() {
	cw.Logger.Info("Starting main consumption loop")

	for !cw.IsShutdown() {
		// check if context is cancelled
		select {
		case <-cw.ctx.Done():
			cw.Logger.Info("Context cancelled, stopping consumer worker")
			cw.Shutdown()
			return
		default:
			// continue
		}
		// 1. get partitions assigned to current consumer
		partitions := cw.HeartbeatWorker.GetHeldPartitions()
		if len(partitions) == 0 {
			cw.Logger.Info("No partitions assigned, waiting...")
			select {
			case <-cw.ctx.Done():
				cw.Logger.Info("Context cancelled while waiting for partitions, stopping consumer worker")
				cw.Shutdown()
				return
			case <-time.After(time.Duration(cw.ConsumerOption.HeartbeatInterval) * time.Second):
				continue
			}
		}

		// 2. create or get worker for each partition. Workers are long-lived; do not wait here.
		for _, partitionInfo := range partitions {
			topicID := partitionInfo.TopicID
			for _, partitionID := range partitionInfo.Partitions {
				worker := cw.getOrCreatePartitionWorker(topicID, partitionID)
				if worker != nil {
					go worker.ConsumeWithContext(cw.ctx)
				}
			}
		}

		// 3. clean up unused partition workers
		cw.cleanupUnusedPartitionWorkers(partitions)

		// 4. intelligent stop: only when offset_end_time is configured. Stop the whole
		// worker once every held partition has caught up to the end and the grace period
		// (consumer_group_timeout + heartbeat_interval) has elapsed (logic.md 126/137).
		if cw.needStop() {
			cw.Logger.Info("All partitions reached end with end time configured, stopping consumer worker")
			cw.Shutdown()
			return
		}

		// 5. wait for next consumption
		select {
		case <-cw.ctx.Done():
			cw.Logger.Info("Context cancelled while waiting for next consumption cycle, stopping consumer worker")
			cw.Shutdown()
			return
		case <-time.After(time.Duration(cw.ConsumerOption.DataFetchInterval) * time.Second):
			// continue loop
		}
	}

	cw.Logger.Info("Main consumption loop stopped")
}

// needStop reports whether the consumer worker should stop on its own.
// It only takes effect when offset_end_time is configured. It returns true once all
// held partitions have caught up to the end (reach_end) and the grace period
// (consumer_group_timeout + heartbeat_interval) has elapsed since they all finished.
// This mirrors the Python reference implementation's _need_stop (logic.md 137).
// Called only from the single run() goroutine, so LastOwnedConsumerFinishTime needs no lock.
func (cw *ConsumerWorker) needStop() bool {
	if cw.ConsumerOption.OffsetEndTime == "" {
		return false
	}

	workers := cw.snapshotPartitionWorkers()
	if len(workers) == 0 {
		// no partitions assigned yet; reset the finish timer and keep running
		cw.LastOwnedConsumerFinishTime = 0
		return false
	}

	for _, worker := range workers {
		if !worker.IsReachEnd() {
			// at least one partition still has data to consume; reset the timer
			cw.LastOwnedConsumerFinishTime = 0
			return false
		}
	}

	// all partitions have reached the end; start / continue the grace countdown
	now := time.Now().Unix()
	if cw.LastOwnedConsumerFinishTime == 0 {
		cw.LastOwnedConsumerFinishTime = now
	}
	grace := int64(cw.ConsumerOption.ConsumerGroupTimeout + cw.ConsumerOption.HeartbeatInterval)
	return now-cw.LastOwnedConsumerFinishTime >= grace
}

// getOrCreatePartitionWorker get or create partition worker
func (cw *ConsumerWorker) getOrCreatePartitionWorker(topicID string, partitionID int) *PartitionConsumerWorker {
	key := fmt.Sprintf("%s:%d", topicID, partitionID)

	cw.PartitionWorkersLock.RLock()
	worker, exists := cw.PartitionWorkers[key]
	cw.PartitionWorkersLock.RUnlock()

	if exists {
		return worker
	}

	// create new partition worker
	cw.PartitionWorkersLock.Lock()
	defer cw.PartitionWorkersLock.Unlock()

	// double check
	if worker, exists = cw.PartitionWorkers[key]; exists {
		return worker
	}

	// create processor instance
	processor := cw.createProcessorInstance()
	processorLock := &cw.ProcessorLock
	if adaptor, ok := processor.(*ConsumerProcessorAdaptor); ok {
		if original, ok := cw.Processor.(*ConsumerProcessorAdaptor); ok && adaptor != original {
			processorLock = &sync.Mutex{}
		}
	}

	worker = NewPartitionConsumerWorker(
		cw.ConsumerClient,
		topicID,
		partitionID,
		cw.ConsumerOption.ConsumerName,
		processor,
		processorLock,
		cw.ConsumerOption.OffsetStartTime,
		cw.ConsumerOption.MaxFetchLogGroupSize,
		cw.ConsumerOption.OffsetEndTime,
		cw.ConsumerOption.StartTime,
		cw.ConsumerOption.EndTime,
		cw.ConsumerOption.Query,
	)

	cw.PartitionWorkers[key] = worker
	cw.Logger.Info("Created partition worker",
		cls.Field{Key: "topic_id", Value: topicID},
		cls.Field{Key: "partition_id", Value: partitionID},
	)

	return worker
}

// cleanupUnusedPartitionWorkers clean up unused partition workers
func (cw *ConsumerWorker) cleanupUnusedPartitionWorkers(activePartitions []cls.PartitionInfo) {
	// build active partition key set
	activeKeys := make(map[string]bool)
	for _, partitionInfo := range activePartitions {
		topicID := partitionInfo.TopicID
		for _, partitionID := range partitionInfo.Partitions {
			key := fmt.Sprintf("%s:%d", topicID, partitionID)
			activeKeys[key] = true
		}
	}

	// clean up unused workers
	cw.PartitionWorkersLock.Lock()
	defer cw.PartitionWorkersLock.Unlock()

	removeKeys := make([]string, 0)
	for key, worker := range cw.PartitionWorkers {
		if !activeKeys[key] {
			cw.Logger.Info("Try to call shut down for unassigned consumer partition",
				cls.Field{Key: "partition_key", Value: key},
			)
			worker.ShutDown()
			cw.Logger.Info("Complete call shut down for unassigned consumer partition",
				cls.Field{Key: "partition_key", Value: key},
			)
		}
		if worker.IsShutdown() {
			cw.Logger.Info("Remove an unassigned consumer partition",
				cls.Field{Key: "partition_key", Value: key},
			)
			// remove from heartbeat partitions (corresponding to Python version remove_heart_partition)
			if cw.HeartbeatWorker != nil {
				// parse topicID and partitionID
				if parts := strings.Split(key, ":"); len(parts) == 2 {
					if partitionID, err := strconv.Atoi(parts[1]); err == nil {
						cw.HeartbeatWorker.RemoveHeartPartition(parts[0], partitionID)
					}
				}
			}
			removeKeys = append(removeKeys, key)
		}
	}

	// batch remove closed workers
	for _, key := range removeKeys {
		delete(cw.PartitionWorkers, key)
	}
}

// createConsumerGroup create consumer group
func (cw *ConsumerWorker) createConsumerGroup() error {
	_, err := cw.ConsumerClient.CreateConsumerGroup(cw.ConsumerOption.ConsumerGroupTimeout)
	return err
}

// createProcessorInstance create processor instance
func (cw *ConsumerWorker) createProcessorInstance() Processor {
	// The default adaptor keeps mutable topic/partition state, so clone it per partition.
	if adaptor, ok := cw.Processor.(*ConsumerProcessorAdaptor); ok {
		return &ConsumerProcessorAdaptor{
			ProcessorBase: NewProcessorBase(),
			Func:          adaptor.Func,
		}
	}
	return cw.Processor
}

// Shutdown gracefully close consumer Worker
func (cw *ConsumerWorker) Shutdown() {
	cw.ShutdownLock.Lock()
	if cw.isShutdown {
		cw.ShutdownLock.Unlock()
		return
	}

	cw.Logger.Info("Shutting down consumer worker")
	cw.isShutdown = true
	cw.ShutdownLock.Unlock()

	// cancel context
	if cw.cancel != nil {
		cw.cancel()
	}

	// close heartbeat Worker
	if cw.HeartbeatWorker != nil {
		cw.HeartbeatWorker.Shutdown()
	}

	// close all partition workers without holding the map lock during user processor shutdown.
	workers := cw.snapshotPartitionWorkers()
	for _, worker := range workers {
		worker.ShutDown()
	}

	cw.Logger.Info("Consumer worker shutdown completed")
}

// IsShutdown check if it has been closed
func (cw *ConsumerWorker) IsShutdown() bool {
	cw.ShutdownLock.Lock()
	defer cw.ShutdownLock.Unlock()
	return cw.isShutdown
}

func (cw *ConsumerWorker) snapshotPartitionWorkers() []*PartitionConsumerWorker {
	cw.PartitionWorkersLock.RLock()
	defer cw.PartitionWorkersLock.RUnlock()
	workers := make([]*PartitionConsumerWorker, 0, len(cw.PartitionWorkers))
	for _, worker := range cw.PartitionWorkers {
		workers = append(workers, worker)
	}
	return workers
}

// GetStats 获取所有分区的消费统计信息（后续要删，仅测试使用），加锁是因为要读取PartitionWorkers map，避免并发读写
func (cw *ConsumerWorker) GetStats() map[string]interface{} {
	workers := cw.snapshotPartitionWorkers()
	stats := make(map[string]interface{})
	var totalLogs int64
	var totalLogGroups int64
	var activePartitions int
	// 收集每个分区的统计信息
	partitionStats := make([]map[string]interface{}, 0, len(workers))
	for _, worker := range workers {
		partitionStat := worker.GetStats()
		partitionStats = append(partitionStats, partitionStat)
		if v, ok := partitionStat["total_logs_consumed"].(int64); ok {
			totalLogs += v
		}
		if v, ok := partitionStat["total_log_groups_consumed"].(int64); ok {
			totalLogGroups += v
		}
		if !worker.IsShutdown() {
			activePartitions++
		}
	}
	// 汇总统计信息
	stats["total_partitions"] = len(workers)
	stats["active_partitions"] = activePartitions
	stats["total_logs_consumed"] = totalLogs
	stats["total_log_groups_consumed"] = totalLogGroups
	stats["partition_details"] = partitionStats
	stats["consumer_group"] = cw.ConsumerOption.ConsumerGroup
	stats["consumer_name"] = cw.ConsumerOption.ConsumerName
	stats["is_shutdown"] = cw.IsShutdown()
	return stats
}

// DeleteConsumerGroup delete consumer group
func (cw *ConsumerWorker) DeleteConsumerGroup() error {
	cw.Logger.Info("Deleting consumer group",
		cls.Field{Key: "consumer_group", Value: cw.ConsumerOption.ConsumerGroup},
	)
	// ensure all partition workers are stopped
	cw.ShutdownLock.Lock()
	if !cw.isShutdown {
		cw.Logger.Warn("Consumer worker is not shutdown, forcing shutdown before deleting consumer group")
		cw.isShutdown = true
	}
	cw.ShutdownLock.Unlock()
	if cw.cancel != nil {
		cw.cancel()
	}
	// stop heartbeat thread
	if cw.HeartbeatWorker != nil {
		cw.Logger.Info("Stopping heartbeat worker before deleting consumer group")
		cw.HeartbeatWorker.Shutdown()
	}
	// ensure all partition consumers are stopped
	cw.Logger.Info("Ensuring all partition workers are stopped")
	for _, worker := range cw.snapshotPartitionWorkers() {
		if !worker.IsShutdown() {
			cw.Logger.Info("Forcing shutdown of partition worker",
				cls.Field{Key: "topic_id", Value: worker.TopicID},
				cls.Field{Key: "partition_id", Value: worker.PartitionID},
			)
			worker.ShutDown()
		}
	}
	// wait for a longer time to ensure all consumers are completely stopped
	cw.Logger.Info("Waiting for all consumers to stop completely")
	time.Sleep(15 * time.Second)
	// try to delete consumer group, up to 3 retries
	var lastErr error
	for i := 0; i < 3; i++ {
		err := cw.ConsumerClient.DeleteConsumerGroup()
		if err == nil {
			cw.Logger.Info("Successfully deleted consumer group",
				cls.Field{Key: "consumer_group", Value: cw.ConsumerOption.ConsumerGroup},
			)
			return nil
		}
		lastErr = err
		cw.Logger.Warn("Failed to delete consumer group, retrying",
			cls.Field{Key: "consumer_group", Value: cw.ConsumerOption.ConsumerGroup},
			cls.Field{Key: "attempt", Value: i + 1},
			cls.Field{Key: "error", Value: err.Error()},
		)
		// wait for a longer time before retrying
		time.Sleep(time.Duration(i+1) * 10 * time.Second)
	}
	cw.Logger.Error("Failed to delete consumer group after retries",
		cls.Field{Key: "consumer_group", Value: cw.ConsumerOption.ConsumerGroup},
		cls.Field{Key: "error", Value: lastErr.Error()},
	)
	return fmt.Errorf("failed to delete consumer group %s after retries: %v", cw.ConsumerOption.ConsumerGroup, lastErr)
}
