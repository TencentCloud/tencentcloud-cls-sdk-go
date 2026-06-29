package consumer

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	cls "github.com/tencentcloud/tencentcloud-cls-sdk-go"
)

// HeartbeatWorker heartbeat worker
// Responsible for maintaining heartbeat connection with CLS backend and obtaining partition allocation
type HeartbeatWorker struct {
	ConsumerClient           *ConsumerClient
	ConsumerOption           *ConsumerOption
	HeldPartitions           []cls.PartitionInfo
	HeartPartitions          []cls.PartitionInfo
	isShutdown               int32 // 0=running, 1=shutdown
	ShutdownLock             sync.Mutex
	LastHeartbeatSuccessTime int64
	Logger                   cls.Logger
	lock                     sync.RWMutex  // add read-write lock to protect partition data
	stopChan                 chan struct{} // add stop signal channel
	stoppedChan              chan struct{} // add stopped signal channel
	ctx                      context.Context
	cancel                   context.CancelFunc
}

// NewHeartbeatWorker create heartbeat worker
func NewHeartbeatWorker(consumerClient *ConsumerClient, consumerOption *ConsumerOption) *HeartbeatWorker {
	// initialize empty partition list
	heldPartitions := make([]cls.PartitionInfo, 0)
	heartPartitions := make([]cls.PartitionInfo, 0)

	// create empty partition list for each topic
	for _, topicID := range consumerOption.TopicIDs {
		heldPartitions = append(heldPartitions, cls.PartitionInfo{
			TopicID:    topicID,
			Partitions: []int{},
		})
		heartPartitions = append(heartPartitions, cls.PartitionInfo{
			TopicID:    topicID,
			Partitions: []int{},
		})
	}

	return &HeartbeatWorker{
		ConsumerClient:           consumerClient,
		ConsumerOption:           consumerOption,
		HeldPartitions:           heldPartitions,
		HeartPartitions:          heartPartitions,
		isShutdown:               0,
		LastHeartbeatSuccessTime: time.Now().Unix(),
		Logger:                   cls.GetZapLoggerAdapter(),
		stopChan:                 make(chan struct{}),
		stoppedChan:              make(chan struct{}),
	}
}

// Run start heartbeat worker with context
func (hw *HeartbeatWorker) Run(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	hw.ctx = ctx
	hw.cancel = func() {} // empty function, because context is managed by external
	hw.Logger.Info("Starting heartbeat worker with context")
	go hw.run()
}

// Start start heartbeat worker
func (hw *HeartbeatWorker) Start() {
	hw.Logger.Info("Starting heartbeat worker")
	hw.Run(context.Background())
}

// run heartbeat main loop
func (hw *HeartbeatWorker) run() {
	hw.Logger.Info("Starting heartbeat loop")
	defer func() {
		hw.Logger.Info("Heartbeat loop stopped")
		close(hw.stoppedChan)
	}()

	for !hw.IsShutdown() {
		// check if context is cancelled
		if hw.ctx != nil {
			select {
			case <-hw.ctx.Done():
				hw.Logger.Info("Context cancelled, stopping heartbeat")
				return
			default:
				// continue execution
			}
		}
		// send heartbeat
		success := hw.sendHeartbeat()

		if success {
			hw.LastHeartbeatSuccessTime = time.Now().Unix()

			// count total partitions from a locked snapshot
			heldPartitions := hw.GetHeldPartitions()
			totalPartitions := 0
			for _, partitionInfo := range heldPartitions {
				totalPartitions += len(partitionInfo.Partitions)
			}

			hw.Logger.Info("Heartbeat successful",
				cls.Field{Key: "total_topics", Value: len(heldPartitions)},
				cls.Field{Key: "total_partitions", Value: totalPartitions},
			)
		} else {
			hw.Logger.Info("Heartbeat failed, retrying...")
		}

		// wait for next heartbeat or stop signal
		select {
		case <-hw.stopChan:
			hw.Logger.Info("Received stop signal, exiting heartbeat loop")
			return
		case <-hw.ctx.Done():
			hw.Logger.Info("Context cancelled, exiting heartbeat loop")
			return
		case <-time.After(time.Duration(hw.ConsumerOption.HeartbeatInterval) * time.Second):
			// continue heartbeat
		}
	}
}

// sendHeartbeat send heartbeat
func (hw *HeartbeatWorker) sendHeartbeat() bool {
	// build heartbeat request partition information under a read lock,
	// so the blocking network call below does not hold the lock.
	hw.lock.RLock()
	partitions := hw.buildHeartbeatPartitions()
	hw.lock.RUnlock()

	// send heartbeat WITHOUT holding the lock to avoid blocking readers
	// (e.g. GetHeldPartitions in the main consumption loop) during network IO.
	var responsePartitions []cls.PartitionInfo
	success := hw.ConsumerClient.Heartbeat(partitions, &responsePartitions)

	// update shared partition state under the write lock.
	hw.lock.Lock()
	defer hw.lock.Unlock()

	if success {
		// check partition change
		if !hw.equalPartitions(hw.HeartPartitions, responsePartitions) {
			addSet := hw.subPartitions(responsePartitions, hw.HeartPartitions)
			removeSet := hw.subPartitions(hw.HeartPartitions, responsePartitions)
			if len(addSet) > 0 || len(removeSet) > 0 {
				hw.Logger.Info("Partition reorganize",
					cls.Field{Key: "adding", Value: addSet},
					cls.Field{Key: "removing", Value: removeSet},
				)
			}
		}

		hw.HeartPartitions = hw.addPartitions(hw.HeartPartitions, responsePartitions)
		hw.HeldPartitions = hw.copyPartitions(responsePartitions)
		return true
	} else {
		// handle heartbeat failure
		currentTime := time.Now().Unix()
		if currentTime-hw.LastHeartbeatSuccessTime > int64(hw.ConsumerOption.ConsumerGroupTimeout+hw.ConsumerOption.HeartbeatInterval) {
			// timeout reset partition
			hw.Logger.Info("Heart beat timeout, automatic reset consumer held partitions")
			emptyPartitions := hw.emptyPartitions()
			hw.HeartPartitions = emptyPartitions
			hw.HeldPartitions = hw.copyPartitions(emptyPartitions)
		} else {
			// keep original partition
			hw.Logger.Info("Heart beat failed, Keep the held partitions unchanged")
			// response_partitions = mheld_partitions
			hw.HeartPartitions = hw.addPartitions(hw.HeartPartitions, hw.HeldPartitions)
		}
		// in both cases, mheld_partitions remains unchanged
		return false
	}
}

// buildHeartbeatPartitions build heartbeat request partition information
func (hw *HeartbeatWorker) buildHeartbeatPartitions() []cls.PartitionInfo {
	// if there are held partitions, use held partitions
	if len(hw.HeldPartitions) > 0 {
		return hw.copyPartitions(hw.HeldPartitions)
	}

	// otherwise, send empty partition list for each topic
	// backend will perform rebalance allocation based on load balancing strategy
	partitions := make([]cls.PartitionInfo, 0)

	for _, topicID := range hw.ConsumerOption.TopicIDs {
		partitionInfo := cls.PartitionInfo{
			TopicID:    topicID,
			Partitions: []int{}, // empty partition list, let backend allocate
		}
		partitions = append(partitions, partitionInfo)
	}

	return partitions
}

// equalPartitions compare two partition information
func (hw *HeartbeatWorker) equalPartitions(p1, p2 []cls.PartitionInfo) bool {
	if len(p1) != len(p2) {
		return false
	}

	for i := range p1 {
		if p1[i].TopicID != p2[i].TopicID {
			return false
		}
		if len(p1[i].Partitions) != len(p2[i].Partitions) {
			return false
		}
		for j := range p1[i].Partitions {
			if p1[i].Partitions[j] != p2[i].Partitions[j] {
				return false
			}
		}
	}
	return true
}

// subPartitions calculate partition difference set
func (hw *HeartbeatWorker) subPartitions(p1, p2 []cls.PartitionInfo) []string {
	result := make([]string, 0)
	sameTopics := make(map[string]bool)

	for _, pA := range p1 {
		for _, pB := range p2 {
			if pA.TopicID == pB.TopicID {
				// calculate difference set
				diff := hw.subPartitionIDs(pA.Partitions, pB.Partitions)
				if len(diff) > 0 {
					result = append(result, hw.formatPartitionInfo(pA.TopicID, diff))
				}
				sameTopics[pA.TopicID] = true
				break
			}
		}
	}

	// add topics in p1 that are not in p2
	for _, pA := range p1 {
		if !sameTopics[pA.TopicID] {
			result = append(result, hw.formatPartitionInfo(pA.TopicID, pA.Partitions))
		}
	}

	return result
}

// subPartitionIDs calculate partition ID difference set
func (hw *HeartbeatWorker) subPartitionIDs(ids1, ids2 []int) []int {
	set2 := make(map[int]bool)
	for _, id := range ids2 {
		set2[id] = true
	}

	result := make([]int, 0)
	for _, id := range ids1 {
		if !set2[id] {
			result = append(result, id)
		}
	}
	return result
}

// addPartitions merge partition information
func (hw *HeartbeatWorker) addPartitions(p1, p2 []cls.PartitionInfo) []cls.PartitionInfo {
	result := make([]cls.PartitionInfo, 0)
	sameTopics := make(map[string]bool)

	// handle partition merge for same topic
	for _, pA := range p1 {
		for _, pB := range p2 {
			if pA.TopicID == pB.TopicID {
				// merge partition IDs, remove duplicates
				mergedPartitions := hw.mergePartitionIDs(pA.Partitions, pB.Partitions)
				result = append(result, cls.PartitionInfo{
					TopicID:    pA.TopicID,
					Partitions: mergedPartitions,
				})
				sameTopics[pA.TopicID] = true
				break
			}
		}
	}

	// add topics in p1 that are not in p2
	for _, pA := range p1 {
		if !sameTopics[pA.TopicID] {
			result = append(result, pA)
		}
	}

	// add topics in p2 that are not in p1
	for _, pB := range p2 {
		if !sameTopics[pB.TopicID] {
			result = append(result, pB)
		}
	}

	return result
}

// mergePartitionIDs merge partition ID list, remove duplicates
func (hw *HeartbeatWorker) mergePartitionIDs(ids1, ids2 []int) []int {
	merged := make(map[int]bool)

	for _, id := range ids1 {
		merged[id] = true
	}
	for _, id := range ids2 {
		merged[id] = true
	}

	result := make([]int, 0, len(merged))
	for id := range merged {
		result = append(result, id)
	}

	return result
}

// copyPartitions deep copy partition information
func (hw *HeartbeatWorker) copyPartitions(partitions []cls.PartitionInfo) []cls.PartitionInfo {
	result := make([]cls.PartitionInfo, len(partitions))
	for i, partition := range partitions {
		result[i] = cls.PartitionInfo{
			TopicID:    partition.TopicID,
			Partitions: append([]int{}, partition.Partitions...),
		}
	}
	return result
}

func (hw *HeartbeatWorker) emptyPartitions() []cls.PartitionInfo {
	result := make([]cls.PartitionInfo, 0, len(hw.ConsumerOption.TopicIDs))
	for _, topicID := range hw.ConsumerOption.TopicIDs {
		result = append(result, cls.PartitionInfo{
			TopicID:    topicID,
			Partitions: []int{},
		})
	}
	return result
}

// formatPartitionInfo format partition information
func (hw *HeartbeatWorker) formatPartitionInfo(topicID string, partitions []int) string {
	// format: topicID[partition1,partition2,...]
	return fmt.Sprintf("%s[%v]", topicID, partitions)
}

// GetHeldPartitions get current held partitions
func (hw *HeartbeatWorker) GetHeldPartitions() []cls.PartitionInfo {
	hw.lock.RLock()
	defer hw.lock.RUnlock()
	return hw.copyPartitions(hw.HeldPartitions)
}

// RemoveHeartPartition
func (hw *HeartbeatWorker) RemoveHeartPartition(topicID string, partitionID int) {
	hw.lock.Lock()
	defer hw.lock.Unlock()

	hw.Logger.Info("Try to remove partition",
		cls.Field{Key: "topicID", Value: topicID},
		cls.Field{Key: "partitionID", Value: partitionID},
		cls.Field{Key: "current partitions", Value: hw.HeldPartitions},
	)

	// remove from HeldPartitions
	for i, part := range hw.HeldPartitions {
		if part.TopicID == topicID {
			for j, pid := range part.Partitions {
				if pid == partitionID {
					// remove partition ID
					hw.HeldPartitions[i].Partitions = append(part.Partitions[:j], part.Partitions[j+1:]...)
					break
				}
			}
			break
		}
	}

	// remove from HeartPartitions
	for i, part := range hw.HeartPartitions {
		if part.TopicID == topicID {
			for j, pid := range part.Partitions {
				if pid == partitionID {
					// remove partition ID
					hw.HeartPartitions[i].Partitions = append(part.Partitions[:j], part.Partitions[j+1:]...)
					break
				}
			}
			break
		}
	}

	hw.Logger.Info("Removed partition",
		cls.Field{Key: "topicID", Value: topicID},
		cls.Field{Key: "partitionID", Value: partitionID},
	)
}

// Shutdown shutdown heartbeat worker
func (hw *HeartbeatWorker) Shutdown() {
	if !atomic.CompareAndSwapInt32(&hw.isShutdown, 0, 1) {
		return
	}

	hw.Logger.Info("Shutting down heartbeat worker")

	// send stop signal
	close(hw.stopChan)

	// wait for stop completion
	select {
	case <-hw.stoppedChan:
		hw.Logger.Info("Heartbeat worker stopped")
	case <-time.After(5 * time.Second):
		hw.Logger.Warn("Heartbeat worker shutdown timeout")
	}
}

// IsShutdown check if it is shutdown
func (hw *HeartbeatWorker) IsShutdown() bool {
	return atomic.LoadInt32(&hw.isShutdown) == 1
}
