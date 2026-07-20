package consumer

import (
	"fmt"
	"sync"
	"time"

	cls "github.com/tencentcloud/tencentcloud-cls-sdk-go"
)

type OffsetTracker struct {
	consumerGroupClient        *ConsumerClient
	consumerName               string
	topicID                    string
	partitionID                int
	lastCheckTime              int64
	offset                     int64
	tempOffset                 int64
	lastPersistentOffset       int64
	defaultFlushOffsetInterval int64
	lock                       sync.Mutex
}

func NewOffsetTracker(client *ConsumerClient, consumerName, topicID string, partitionID int) *OffsetTracker {
	return &OffsetTracker{
		consumerGroupClient:        client,
		consumerName:               consumerName,
		topicID:                    topicID,
		partitionID:                partitionID,
		offset:                     -1,
		tempOffset:                 -1,
		lastPersistentOffset:       -1,
		defaultFlushOffsetInterval: 60,
	}
}

func (t *OffsetTracker) SetOffset(offset int64) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.offset = offset
}

func (t *OffsetTracker) GetOffset() int64 {
	t.lock.Lock()
	defer t.lock.Unlock()
	return t.offset
}

func (t *OffsetTracker) SaveOffset(persistent bool, offset ...int64) error {
	t.lock.Lock()
	if len(offset) > 0 {
		t.tempOffset = offset[0]
	} else {
		t.tempOffset = t.offset
	}
	t.lock.Unlock()
	if persistent {
		return t.FlushOffset(false)
	}
	return nil
}

func (t *OffsetTracker) SetMemoryOffset(offset int64) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.tempOffset = offset
}

func (t *OffsetTracker) SetPersistentOffset(offset int64) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.lastPersistentOffset = offset
}

func (t *OffsetTracker) FlushOffset(force bool) error {
	t.lock.Lock() // add lock, avoid concurrent read and write
	defer t.lock.Unlock()
	flushOffset := t.tempOffset
	if flushOffset == -1 {
		flushOffset = t.offset
	}
	if flushOffset == -1 {
		return nil
	}
	if flushOffset != t.lastPersistentOffset || force {
		offsets := t.GenerateOffsets(flushOffset)
		err := t.consumerGroupClient.UpdateOffsets(offsets)
		if err != nil {
			return fmt.Errorf("Failed to persistent the offset to outside system, %s, %d, %d: %v", t.consumerName, t.partitionID, flushOffset, err)
		}
		t.tempOffset = flushOffset
		t.lastPersistentOffset = flushOffset
	}
	return nil
}

func (t *OffsetTracker) FlushCheck() {
	currentTime := time.Now().Unix()
	t.lock.Lock()
	lastCheck := t.lastCheckTime
	t.lock.Unlock()
	if currentTime > lastCheck+t.defaultFlushOffsetInterval {
		err := t.FlushOffset(false)
		if err != nil {
			fmt.Println(err)
		}
		t.lock.Lock()
		t.lastCheckTime = currentTime
		t.lock.Unlock()
	}
}

func (t *OffsetTracker) GenerateOffsets(offset int64) []cls.TopicPartitionOffsetsInfo {
	return []cls.TopicPartitionOffsetsInfo{
		{
			TopicID: t.topicID,
			PartitionOffsets: []cls.PartitionOffset{
				{
					PartitionID: t.partitionID,
					Offset:      offset,
				},
			},
		},
	}
}

func (t *OffsetTracker) GetTempOffset() int64 {
	t.lock.Lock()
	defer t.lock.Unlock()
	return t.tempOffset
}

func (t *OffsetTracker) GetLastPersistentOffset() int64 {
	t.lock.Lock()
	defer t.lock.Unlock()
	return t.lastPersistentOffset
}

func (t *OffsetTracker) GetLastCheckTime() int64 {
	t.lock.Lock()
	defer t.lock.Unlock()
	return t.lastCheckTime
}
