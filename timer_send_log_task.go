package tencentcloud_cls_sdk_go

import (
	"sync"
	"time"

	"go.uber.org/atomic"
)

type TimerSendLogTask struct {
	shutDownFlag   *atomic.Bool
	retryQueue     *LogSendRetryQueue
	worker         *Worker
	logAccumulator *Accumulator
	threadPool     *SendThreadPool
}

func NewTimerSendLogTask(logAccumulator *Accumulator, retryQueue *LogSendRetryQueue, ioWorker *Worker, threadPool *SendThreadPool) *TimerSendLogTask {
	timerTask := &TimerSendLogTask{
		shutDownFlag:   atomic.NewBool(false),
		retryQueue:     retryQueue,
		worker:         ioWorker,
		logAccumulator: logAccumulator,
		threadPool:     threadPool,
	}
	return timerTask
}

func (timerTask *TimerSendLogTask) sendToServer(key string, batch *ProducerBatch, config *AsyncProducerClientConfig) {
	if value, ok := timerTask.logAccumulator.logTopicData[key]; !ok {
		return
	} else if GetTimeMs(time.Now().UnixNano())-value.createTimeMs < config.LingerMs {
		return
	}
	timerTask.threadPool.addTask(batch)
	delete(timerTask.logAccumulator.logTopicData, key)
}

func (timerTask *TimerSendLogTask) run(moverWaitGroup *sync.WaitGroup, config *AsyncProducerClientConfig) {
	defer moverWaitGroup.Done()
	for !timerTask.shutDownFlag.Load() {
		sleepMs := config.LingerMs
		nowTimeMs := GetTimeMs(time.Now().UnixNano())
		timerTask.logAccumulator.lock.Lock()
		mapCount := len(timerTask.logAccumulator.logTopicData)
		for key, batch := range timerTask.logAccumulator.logTopicData {
			timeInterval := batch.createTimeMs + config.LingerMs - nowTimeMs
			if timeInterval <= 0 {
				timerTask.sendToServer(key, batch, config)
			} else {
				if sleepMs > timeInterval {
					sleepMs = timeInterval
				}
			}
		}
		timerTask.logAccumulator.lock.Unlock()

		if mapCount == 0 {
			sleepMs = config.LingerMs
		}

		retryProducerBatchList := timerTask.retryQueue.getRetryBatch(timerTask.shutDownFlag.Load())
		if retryProducerBatchList == nil {
			time.Sleep(time.Duration(sleepMs) * time.Millisecond)
		} else {
			count := len(retryProducerBatchList)
			for i := 0; i < count; i++ {
				timerTask.threadPool.addTask(retryProducerBatchList[i])
			}
		}

	}
	timerTask.logAccumulator.lock.Lock()
	for _, batch := range timerTask.logAccumulator.logTopicData {
		timerTask.threadPool.addTask(batch)
	}
	timerTask.logAccumulator.logTopicData = make(map[string]*ProducerBatch)
	timerTask.logAccumulator.lock.Unlock()

	producerBatchList := timerTask.retryQueue.getRetryBatch(timerTask.shutDownFlag.Load())
	count := len(producerBatchList)
	for i := 0; i < count; i++ {
		timerTask.threadPool.addTask(producerBatchList[i])
	}
}
