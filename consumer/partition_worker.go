package consumer

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	cls "github.com/tencentcloud/tencentcloud-cls-sdk-go"
)

// PartitionConsumerWorker simplified partition consumer Worker
// serial design, focusing on main logic
type PartitionConsumerWorker struct {
	// identification information
	TopicID              string
	PartitionID          int
	ConsumerName         string
	LogClient            *ConsumerClient
	Processor            Processor
	OffsetTracker        *OffsetTracker
	Logger               cls.Logger
	OffsetStartTime      string
	OffsetEndTime        string
	StartTime            int64
	EndTime              int64
	// Query is the optional DSL pre-filter expression forwarded to PullLogs.
	// Empty string disables server-side filtering.
	Query                string
	MaxFetchLogGroupSize int
	NextFetchOffset      int64
	shutdown             int32 // 0=running, 1=shutdown; atomic to avoid data race
	running              int32 // 0=idle, 1=running; prevents duplicate goroutine launch
	reachEnd             int32 // 0=not reached, 1=reached end (caught up to latest/end offset); atomic
	Initialized          bool
	LastFetchTime        int64
	LastFetchCount       int
	LastEmptyFlushTime   int64 // unix seconds; used for the "flush offset every 30s while idle" protection
	LogList              []*cls.Log
	processorLock        *sync.Mutex
	closeOnce            sync.Once
	statsLock            sync.RWMutex
	// 统计字段
	TotalLogsConsumed      int64 // 该分区总共消费的日志数
	TotalLogGroupsConsumed int64 // 该分区总共消费的日志组数，一个日志组包含多个日志
}

// NewPartitionConsumerWorker constructor
func NewPartitionConsumerWorker(
	logClient *ConsumerClient,
	topicID string,
	partitionID int,
	consumerName string,
	processor Processor,
	processorLock *sync.Mutex,
	offsetStartTime string,
	maxFetchLogGroupSize int,
	offsetEndTime string,
	startTime int64,
	endTime int64,
	query string,
) *PartitionConsumerWorker {
	if processorLock == nil {
		processorLock = &sync.Mutex{}
	}
	return &PartitionConsumerWorker{
		TopicID:                topicID,
		LogClient:              logClient,
		PartitionID:            partitionID,
		ConsumerName:           consumerName,
		OffsetStartTime:        offsetStartTime,
		OffsetEndTime:          offsetEndTime,
		StartTime:              startTime,
		EndTime:                endTime,
		Query:                  query,
		Processor:              processor,
		OffsetTracker:          NewOffsetTracker(logClient, consumerName, topicID, partitionID),
		MaxFetchLogGroupSize:   maxFetchLogGroupSize,
		NextFetchOffset:        -1,
		Initialized:            false,
		LastFetchTime:          0,
		LastFetchCount:         0,
		LogList:                make([]*cls.Log, 0),
		processorLock:          processorLock,
		TotalLogsConsumed:      0,
		TotalLogGroupsConsumed: 0,
		Logger:                 cls.GetZapLoggerAdapter(),
	}
}

// ConsumeWithContext main consumption method using context.
// Uses running flag (CAS) to prevent duplicate concurrent goroutine execution on the same worker.
func (pw *PartitionConsumerWorker) ConsumeWithContext(ctx context.Context) {
	if pw.IsShutdown() {
		return
	}

	// Prevent duplicate goroutine: only one ConsumeWithContext runs at a time per worker.
	if !atomic.CompareAndSwapInt32(&pw.running, 0, 1) {
		return
	}
	defer atomic.StoreInt32(&pw.running, 0)

	// 1. initialization phase
	if !pw.Initialized {
		pw.initialize()
		return
	}

	// 2. main consumption loop - simplified version of intelligent stop
	for !pw.IsShutdown() {
		// check if context is cancelled
		select {
		case <-ctx.Done():
			pw.Logger.Info("Context cancelled, stopping consumption",
				cls.Field{Key: "topicID", Value: pw.TopicID},
				cls.Field{Key: "partitionID", Value: pw.PartitionID},
			)
			pw.close()
			return
		default:
			// continue consumption
		}

		// fetch data
		logGroups := pw.fetchData()

		// process data
		if len(logGroups) > 0 {
			pw.processData(logGroups)
		}

		// periodic flush_check fallback: ensure the 60s offset commit still fires even
		// when the partition is idle (no data). processData no longer triggers it on its
		// own, so this single call covers both the busy and idle paths (logic.md 244/301).
		pw.OffsetTracker.FlushCheck()

		// rate limiting simplified version
		select {
		case <-ctx.Done():
			pw.Logger.Info("Context cancelled during sleep, stopping consumption",
				cls.Field{Key: "topicID", Value: pw.TopicID},
				cls.Field{Key: "partitionID", Value: pw.PartitionID},
			)
			pw.close()
			return
		case <-time.After(100 * time.Millisecond):
			// continue loop
		}
	}

	// consumption end, force submit offset
	pw.close()
	pw.Logger.Info("Consumer finished, offset flushed",
		cls.Field{Key: "topicID", Value: pw.TopicID},
		cls.Field{Key: "partitionID", Value: pw.PartitionID},
	)
}

// Consume main consumption method
func (pw *PartitionConsumerWorker) Consume() {
	pw.ConsumeWithContext(context.Background())
}

// initialize initialization
func (pw *PartitionConsumerWorker) initialize() {
	pw.Logger.Info("Initializing partition worker",
		cls.Field{Key: "topicID", Value: pw.TopicID},
		cls.Field{Key: "partitionID", Value: pw.PartitionID},
	)

	// initialize processor under lock, because the default adaptor keeps mutable topic/partition state.
	pw.processorLock.Lock()
	if adaptor, ok := pw.Processor.(*ConsumerProcessorAdaptor); ok && adaptor.ProcessorBase != nil {
		adaptor.PartitionID = pw.PartitionID
	}
	pw.Processor.Initialize(pw.TopicID)
	pw.processorLock.Unlock()

	// get start offset
	offset, err := pw.LogClient.GetPartitionOffsets(pw.TopicID, pw.PartitionID, pw.OffsetStartTime)
	if err == nil && offset >= 0 {
		// offset 0 is valid and should not fall back to begin.
		pw.setNextFetchOffset(offset)
		pw.OffsetTracker.SetMemoryOffset(offset)
		pw.OffsetTracker.SetPersistentOffset(offset)
		pw.Logger.Info("Initialized with persistent offset",
			cls.Field{Key: "offset", Value: offset},
		)
	} else {
		// if consumer group does not exist or has no offset record, start from begin position
		// try to get offset at begin position
		beginOffset, beginErr := pw.LogClient.GetPartitionOffsets(pw.TopicID, pw.PartitionID, "begin")
		if beginErr == nil && beginOffset >= 0 {
			pw.setNextFetchOffset(beginOffset)
			pw.Logger.Info("Initialized with begin offset",
				cls.Field{Key: "offset", Value: beginOffset},
			)
		} else {
			// if failed to get begin offset, use 0
			pw.setNextFetchOffset(0)
			pw.Logger.Info("Failed to get offset, using default 0",
				cls.Field{Key: "offset", Value: int64(0)},
			)
		}
	}

	pw.Initialized = true
}

// fetchData fetch data - serial implementation
func (pw *PartitionConsumerWorker) fetchData() []*cls.LogGroup {
	// check if it has been closed
	if pw.IsShutdown() {
		return nil
	}

	// intelligent rate limiting - dynamic adjustment based on data volume
	now := time.Now().UnixNano() / int64(time.Millisecond) // convert to milliseconds
	var minIntervalMs int64
	lastFetchTime, lastFetchCount := pw.getFetchStats()

	// dynamic adjustment of interval based on the amount of data fetched last time
	if lastFetchCount < 100 {
		minIntervalMs = 500 // 0.5 seconds = 500 milliseconds
	} else if lastFetchCount < 500 {
		minIntervalMs = 200 // 0.2 seconds = 200 milliseconds
	} else if lastFetchCount < 1000 {
		minIntervalMs = 50 // 0.05 seconds = 50 milliseconds
	} else {
		minIntervalMs = 0 // no limit when data volume is large
	}
	if minIntervalMs > 0 && now-lastFetchTime < minIntervalMs {
		return nil
	}

	nextFetchOffset := pw.getNextFetchOffset()
	pw.Logger.Info("Fetching data",
		cls.Field{Key: "topicID", Value: pw.TopicID},
		cls.Field{Key: "partitionID", Value: pw.PartitionID},
		cls.Field{Key: "offset", Value: nextFetchOffset},
	)

	pw.setLastFetchTime(now)

	// fix time filtering logic
	var startTimePtr *int64
	var endTimePtr *int64

	// only perform time filtering when startTime and endTime are not 0
	if pw.StartTime > 0 || pw.EndTime > 0 {
		startTime := int64(pw.StartTime)
		endTime := int64(pw.EndTime)
		if startTime > endTime && endTime > 0 {
			pw.Logger.Info("StartTime is greater than EndTime, skipping fetch")
			return nil
		}
		startTimePtr = &startTime
		endTimePtr = &endTime
	}

	// add retry mechanism
	var resp *cls.PullLogResponse
	var err error

	for retryTimes := 0; retryTimes < 3; retryTimes++ {
		// check if it has been closed before each retry
		if pw.IsShutdown() {
			return nil
		}
		resp, err = pw.LogClient.PullLogs(pw.TopicID, pw.PartitionID, pw.MaxFetchLogGroupSize, startTimePtr, nextFetchOffset, endTimePtr, pw.Query)
		if err == nil {
			break
		}

		// check if it is an invalid offset error, if so, try to get offset again
		if retryTimes == 0 && strings.Contains(strings.ToLower(err.Error()), "invalidoffset") {
			pw.Logger.Info("Invalid offset detected, trying to get end offset",
				cls.Field{Key: "topic_id", Value: pw.TopicID},
				cls.Field{Key: "partition_id", Value: pw.PartitionID},
			)

			// try to get end offset
			offsets, getErr := pw.LogClient.GetOffsets(pw.TopicID, pw.PartitionID, "end")
			if getErr == nil && len(offsets) > 0 && len(offsets[0].PartitionOffsets) > 0 {
				nextFetchOffset = offsets[0].PartitionOffsets[0].Offset
				pw.setNextFetchOffset(nextFetchOffset)
				pw.Logger.Info("Updated to end offset",
					cls.Field{Key: "new_offset", Value: nextFetchOffset},
				)
				continue
			}
		}

		pw.Logger.Error("Error fetching data (retry)",
			cls.Field{Key: "retry", Value: retryTimes},
			cls.Field{Key: "error", Value: err.Error()},
		)

		// last retry failed, return nil
		if retryTimes == 2 {
			return nil
		}

		// wait for a while before retrying
		time.Sleep(time.Duration(retryTimes+1) * time.Second)
	}

	if resp == nil {
		return nil
	}

	logGroups := resp.GetLogGroups()
	nextOffset := resp.GetNextOffset()

	pw.Logger.Info("Fetched log groups",
		cls.Field{Key: "topic_id", Value: pw.TopicID},
		cls.Field{Key: "partition_id", Value: pw.PartitionID},
		cls.Field{Key: "log_groups", Value: len(logGroups)},
		cls.Field{Key: "next_offset", Value: nextOffset},
		cls.Field{Key: "offset", Value: pw.OffsetTracker.GetOffset()},
	)

	// update next fetch offset
	currentNextFetchOffset := pw.getNextFetchOffset()
	pw.Logger.Info("Checking offset update",
		cls.Field{Key: "topic_id", Value: pw.TopicID},
		cls.Field{Key: "partition_id", Value: pw.PartitionID},
		cls.Field{Key: "nextOffset", Value: nextOffset},
		cls.Field{Key: "nextOffset > 0", Value: nextOffset > 0},
		cls.Field{Key: "current_next_fetch_offset", Value: currentNextFetchOffset},
	)

	if nextOffset > 0 {
		oldOffset := currentNextFetchOffset
		pw.setNextFetchOffset(nextOffset)
		// immediately update OffsetTracker
		pw.OffsetTracker.SetOffset(nextOffset)
		// still progressing, not at the end
		pw.setReachEnd(false)
		pw.Logger.Info("Updated next fetch offset",
			cls.Field{Key: "topic_id", Value: pw.TopicID},
			cls.Field{Key: "partition_id", Value: pw.PartitionID},
			cls.Field{Key: "old_offset", Value: oldOffset},
			cls.Field{Key: "new_offset", Value: nextOffset},
			cls.Field{Key: "current_next_fetch_offset", Value: nextOffset},
		)
	} else {
		// nextOffset <= 0 (-1 / PULL_NO_LOG) means there is no more data to pull:
		// we have caught up to the latest / configured end offset (logic.md 199/230).
		pw.setReachEnd(true)
		pw.Logger.Info("No offset update needed, reached end",
			cls.Field{Key: "topic_id", Value: pw.TopicID},
			cls.Field{Key: "partition_id", Value: pw.PartitionID},
			cls.Field{Key: "nextOffset", Value: nextOffset},
		)
	}

	// empty-data protection: when no log groups are fetched, proactively flush the
	// offset every 30s so that progress is still persisted while the partition is
	// idle / caught up (logic.md 200/301). Reset the timer once data flows again.
	if len(logGroups) == 0 {
		now := time.Now().Unix()
		last := pw.getLastEmptyFlushTime()
		if last == 0 {
			pw.setLastEmptyFlushTime(now)
		} else if now-last >= 30 {
			if err := pw.OffsetTracker.FlushOffset(false); err != nil {
				pw.Logger.Error("Failed to flush offset on idle",
					cls.Field{Key: "topic_id", Value: pw.TopicID},
					cls.Field{Key: "partition_id", Value: pw.PartitionID},
					cls.Field{Key: "error", Value: err.Error()},
				)
			}
			pw.setLastEmptyFlushTime(now)
		}
	} else {
		pw.setLastEmptyFlushTime(0)
	}

	pw.setLastFetchCount(len(logGroups))
	return logGroups
}

// processData process data - serial implementation
func (pw *PartitionConsumerWorker) processData(logGroups []*cls.LogGroup) {
	// check if it has been closed
	if pw.IsShutdown() {
		return
	}

	if len(logGroups) == 0 {
		return
	}

	pw.Logger.Info("Processing log groups",
		cls.Field{Key: "log_groups", Value: len(logGroups)},
	)

	// add panic recovery
	defer func() {
		if r := recover(); r != nil {
			pw.Logger.Error("Panic in processData",
				cls.Field{Key: "error", Value: r},
			)
		}
	}()

	// allocate a fresh backing array each batch instead of reusing pw.LogList[:0].
	// This prevents data corruption if the user's Processor keeps a reference to the
	// slice after Process returns: the next batch would otherwise overwrite the
	// underlying array. (logic.md 195: deep copy before submitting to process)
	var totalLogsInThisBatch int64
	for _, logGroup := range logGroups {
		totalLogsInThisBatch += int64(len(logGroup.Logs))
	}
	pw.LogList = make([]*cls.Log, 0, totalLogsInThisBatch)
	// 更新统计信息
	pw.addConsumedStats(totalLogsInThisBatch, int64(len(logGroups)))
	// call processor to process data
	for _, logGroup := range logGroups {
		for _, logItem := range logGroup.Logs {
			// add all logs fetched in one round to the list
			pw.LogList = append(pw.LogList, logItem)
		}
	}
	var err error
	func() {
		pw.processorLock.Lock()
		defer pw.processorLock.Unlock()
		_, err = pw.Processor.Process(pw.LogList, pw.OffsetTracker)
	}()
	if err != nil {
		pw.Logger.Error("Error processing data",
			cls.Field{Key: "error", Value: err.Error()},
		)
		return
	}

	// offset has been updated in fetchData; the periodic flush_check fallback is now
	// driven once per loop iteration in ConsumeWithContext (covers idle partitions too).

	pw.Logger.Info("Successfully processed log groups",
		cls.Field{Key: "log_groups", Value: len(logGroups)},
		cls.Field{Key: "current_offset", Value: pw.OffsetTracker.GetOffset()},
	)
}

// ShutDown close worker
func (pw *PartitionConsumerWorker) ShutDown() {
	pw.close()

	pw.Logger.Info("Shutting down partition worker",
		cls.Field{Key: "topicID", Value: pw.TopicID},
		cls.Field{Key: "partitionID", Value: pw.PartitionID},
	)
}

func (pw *PartitionConsumerWorker) close() {
	atomic.StoreInt32(&pw.shutdown, 1)
	pw.closeOnce.Do(func() {
		pw.processorLock.Lock()
		defer pw.processorLock.Unlock()
		pw.OffsetTracker.FlushOffset(true)
		pw.Processor.Shutdown(pw.OffsetTracker)
	})
}

// IsShutdown check if it has been closed
func (pw *PartitionConsumerWorker) IsShutdown() bool {
	return atomic.LoadInt32(&pw.shutdown) == 1
}

// IsReachEnd reports whether this partition has caught up to the latest / configured
// end offset (no more data to pull). Used by the worker-level intelligent stop.
func (pw *PartitionConsumerWorker) IsReachEnd() bool {
	return atomic.LoadInt32(&pw.reachEnd) == 1
}

func (pw *PartitionConsumerWorker) setReachEnd(reached bool) {
	if reached {
		atomic.StoreInt32(&pw.reachEnd, 1)
	} else {
		atomic.StoreInt32(&pw.reachEnd, 0)
	}
}

// GetStats 获取该分区的消费统计信息
func (pw *PartitionConsumerWorker) GetStats() map[string]interface{} {
	pw.statsLock.RLock()
	defer pw.statsLock.RUnlock()
	return map[string]interface{}{
		"topic_id":                  pw.TopicID,
		"partition_id":              pw.PartitionID,
		"total_logs_consumed":       pw.TotalLogsConsumed,
		"total_log_groups_consumed": pw.TotalLogGroupsConsumed,
		"last_fetch_count":          pw.LastFetchCount,
		"next_fetch_offset":         pw.NextFetchOffset,
		"is_shutdown":               pw.IsShutdown(),
	}
}

func (pw *PartitionConsumerWorker) getNextFetchOffset() int64 {
	pw.statsLock.RLock()
	defer pw.statsLock.RUnlock()
	return pw.NextFetchOffset
}

func (pw *PartitionConsumerWorker) setNextFetchOffset(offset int64) {
	pw.statsLock.Lock()
	defer pw.statsLock.Unlock()
	pw.NextFetchOffset = offset
}

func (pw *PartitionConsumerWorker) getFetchStats() (int64, int) {
	pw.statsLock.RLock()
	defer pw.statsLock.RUnlock()
	return pw.LastFetchTime, pw.LastFetchCount
}

func (pw *PartitionConsumerWorker) setLastFetchTime(timestamp int64) {
	pw.statsLock.Lock()
	defer pw.statsLock.Unlock()
	pw.LastFetchTime = timestamp
}

func (pw *PartitionConsumerWorker) setLastFetchCount(count int) {
	pw.statsLock.Lock()
	defer pw.statsLock.Unlock()
	pw.LastFetchCount = count
}

func (pw *PartitionConsumerWorker) getLastEmptyFlushTime() int64 {
	pw.statsLock.RLock()
	defer pw.statsLock.RUnlock()
	return pw.LastEmptyFlushTime
}

func (pw *PartitionConsumerWorker) setLastEmptyFlushTime(timestamp int64) {
	pw.statsLock.Lock()
	defer pw.statsLock.Unlock()
	pw.LastEmptyFlushTime = timestamp
}

func (pw *PartitionConsumerWorker) addConsumedStats(logs, logGroups int64) {
	pw.statsLock.Lock()
	defer pw.statsLock.Unlock()
	pw.TotalLogsConsumed += logs
	pw.TotalLogGroupsConsumed += logGroups
}
