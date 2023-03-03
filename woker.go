package tencentcloud_cls_sdk_go

import (
	"context"
	"math"
	"sync"
	asyncAtomic "sync/atomic"
	"time"

	"go.uber.org/atomic"
)

type Worker struct {
	taskCount              int64
	retryQueue             *LogSendRetryQueue
	retryQueueShutDownFlag *atomic.Bool
	maxSendWorker          chan int64
	producer               *AsyncProducerClient
	clsClient              *CLSClient
}

// NewWorker ...
func NewWorker(clsClient *CLSClient, retryQueue *LogSendRetryQueue, maxSendWorkerCount int64, producer *AsyncProducerClient) *Worker {
	return &Worker{
		clsClient:              clsClient,
		retryQueue:             retryQueue,
		taskCount:              0,
		retryQueueShutDownFlag: atomic.NewBool(false),
		maxSendWorker:          make(chan int64, maxSendWorkerCount),
		producer:               producer,
	}
}

func (worker *Worker) sendToServer(producerBatch *ProducerBatch) {
	var err *CLSError
	err = worker.clsClient.Send(context.Background(), producerBatch.topicID, producerBatch.logGroup)
	if err == nil {
		if producerBatch.attemptCount < producerBatch.maxReservedAttempts {
			attempt := NewAttempt(true, "", "", "", GetTimeMs(time.Now().UnixNano()))
			producerBatch.result.attemptList = append(producerBatch.result.attemptList, attempt)
		}
		producerBatch.result.successful = true
		asyncAtomic.AddInt64(&worker.producer.producerLogGroupSize, -producerBatch.totalDataSize)
		if len(producerBatch.callBackList) > 0 {
			for _, callBack := range producerBatch.callBackList {
				callBack.Success(producerBatch.result)
			}
		}
	} else {
		if worker.retryQueueShutDownFlag.Load() {
			if len(producerBatch.callBackList) > 0 {
				for _, callBack := range producerBatch.callBackList {
					worker.addErrorMessageToBatchAttempt(producerBatch, err)
					callBack.Fail(producerBatch.result)
				}
			}
			return
		}

		// 413 -- 404 -- 401 不再重新上传
		if err.HTTPCode == 413 || err.HTTPCode == 404 || err.HTTPCode == 401 {
			worker.addErrorMessageToBatchAttempt(producerBatch, err)
			worker.executeFailedCallback(producerBatch)
			return
		}
		if producerBatch.attemptCount < producerBatch.maxRetryTimes {
			worker.addErrorMessageToBatchAttempt(producerBatch, err)
			retryWaitTime := producerBatch.baseRetryBackoffMs * int64(math.Pow(2, float64(producerBatch.attemptCount)-1))
			if retryWaitTime < producerBatch.maxRetryIntervalInMs {
				producerBatch.nextRetryMs = GetTimeMs(time.Now().UnixNano()) + retryWaitTime
			} else {
				producerBatch.nextRetryMs = GetTimeMs(time.Now().UnixNano()) + producerBatch.maxRetryIntervalInMs
			}
			worker.retryQueue.sendToRetryQueue(producerBatch)
		} else {
			worker.executeFailedCallback(producerBatch)
		}
	}
}

func (worker *Worker) addErrorMessageToBatchAttempt(producerBatch *ProducerBatch, err *CLSError) {
	if producerBatch.attemptCount < producerBatch.maxReservedAttempts {
		attempt := NewAttempt(false, err.RequestID, err.Code, err.Message, GetTimeMs(time.Now().UnixNano()))
		producerBatch.result.attemptList = append(producerBatch.result.attemptList, attempt)
	}
	producerBatch.result.successful = false
	producerBatch.attemptCount += 1
}

func (worker *Worker) closeSendTask(ioWorkerWaitGroup *sync.WaitGroup) {
	<-worker.maxSendWorker
	asyncAtomic.AddInt64(&worker.taskCount, -1)
	ioWorkerWaitGroup.Done()
}

func (worker *Worker) startSendTask(ioWorkerWaitGroup *sync.WaitGroup) {
	asyncAtomic.AddInt64(&worker.taskCount, 1)
	worker.maxSendWorker <- 1
	ioWorkerWaitGroup.Add(1)
}

func (worker *Worker) executeFailedCallback(producerBatch *ProducerBatch) {
	asyncAtomic.AddInt64(&worker.producer.producerLogGroupSize, -producerBatch.totalDataSize)
	if len(producerBatch.callBackList) > 0 {
		for _, callBack := range producerBatch.callBackList {
			callBack.Fail(producerBatch.result)
		}
	}
}
