package tencentcloud_cls_sdk_go

import (
	"container/heap"
	"sync"
	"time"
)

// LogSendRetryQueue RetryQueue cache ProducerBatch and retry latter
type LogSendRetryQueue struct {
	batch []*ProducerBatch
	mutex sync.Mutex
}

// NewRetryQueue ...
func NewRetryQueue() *LogSendRetryQueue {
	retryQueue := LogSendRetryQueue{}
	heap.Init(&retryQueue)
	return &retryQueue
}

func (retryQueue *LogSendRetryQueue) sendToRetryQueue(producerBatch *ProducerBatch) {
	retryQueue.mutex.Lock()
	defer retryQueue.mutex.Unlock()
	if producerBatch != nil {
		heap.Push(retryQueue, producerBatch)
	}
}

func (retryQueue *LogSendRetryQueue) getRetryBatch(moverShutDownFlag bool) (producerBatchList []*ProducerBatch) {
	retryQueue.mutex.Lock()
	defer retryQueue.mutex.Unlock()
	if !moverShutDownFlag {
		for retryQueue.Len() > 0 {
			producerBatch := heap.Pop(retryQueue)
			if producerBatch.(*ProducerBatch).nextRetryMs < GetTimeMs(time.Now().UnixNano()) {
				producerBatchList = append(producerBatchList, producerBatch.(*ProducerBatch))
			} else {
				heap.Push(retryQueue, producerBatch.(*ProducerBatch))
				break
			}
		}
	} else {
		for retryQueue.Len() > 0 {
			producerBatch := heap.Pop(retryQueue)
			producerBatchList = append(producerBatchList, producerBatch.(*ProducerBatch))
		}
	}
	return producerBatchList
}

func (retryQueue *LogSendRetryQueue) Len() int {
	return len(retryQueue.batch)
}

func (retryQueue *LogSendRetryQueue) Less(i, j int) bool {
	return retryQueue.batch[i].nextRetryMs < retryQueue.batch[j].nextRetryMs
}

func (retryQueue *LogSendRetryQueue) Swap(i, j int) {
	retryQueue.batch[i], retryQueue.batch[j] = retryQueue.batch[j], retryQueue.batch[i]
}

func (retryQueue *LogSendRetryQueue) Push(x interface{}) {
	item := x.(*ProducerBatch)
	retryQueue.batch = append(retryQueue.batch, item)
}

func (retryQueue *LogSendRetryQueue) Pop() interface{} {
	old := retryQueue.batch
	n := len(old)
	item := old[n-1]
	retryQueue.batch = old[0 : n-1]
	return item
}
