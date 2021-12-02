package tencentcloud_cls_sdk_go

import (
	"errors"
	"sync"

	asyncAtomic "sync/atomic"

	"go.uber.org/atomic"
)

type Accumulator struct {
	lock           sync.RWMutex
	logTopicData   map[string]*ProducerBatch
	producerConfig *AsyncProducerClientConfig
	worker         *Worker
	shutDownFlag   *atomic.Bool
	threadPool     *SendThreadPool
	producer       *AsyncProducerClient
}

// NewAccumulator ...
func NewAccumulator(config *AsyncProducerClientConfig, worker *Worker, threadPool *SendThreadPool, producer *AsyncProducerClient) *Accumulator {
	return &Accumulator{
		logTopicData:   make(map[string]*ProducerBatch),
		producerConfig: config,
		worker:         worker,
		shutDownFlag:   atomic.NewBool(false),
		threadPool:     threadPool,
		producer:       producer,
	}
}

func (accumulator *Accumulator) addOrSendProducerBatch(topicId string, producerBatch *ProducerBatch, log interface{}, callback CallBack) {
	totalDataCount := producerBatch.getLogGroupCount() + 1
	if producerBatch.totalDataSize > accumulator.producerConfig.MaxBatchSize &&
		producerBatch.totalDataSize < 5242880 &&
		totalDataCount <= accumulator.producerConfig.MaxBatchCount {
		producerBatch.addLogToLogGroup(log)
		if callback != nil {
			producerBatch.addProducerBatchCallBack(callback)
		}
		accumulator.innerSendToServer(topicId, producerBatch)
	} else if producerBatch.totalDataSize <= accumulator.producerConfig.MaxBatchSize &&
		totalDataCount <= accumulator.producerConfig.MaxBatchCount {
		producerBatch.addLogToLogGroup(log)
		if callback != nil {
			producerBatch.addProducerBatchCallBack(callback)
		}
	} else {
		accumulator.innerSendToServer(topicId, producerBatch)
		accumulator.createNewProducerBatch(log, callback, topicId)
	}
}

func (accumulator *Accumulator) createNewProducerBatch(logType interface{}, callback CallBack, topicId string) {
	if item, ok := logType.(*Log); ok {
		newProducerBatch := NewProducerBatch(topicId, accumulator.producerConfig, callback, item)
		accumulator.logTopicData[topicId] = newProducerBatch
	} else if logList, ok := logType.([]*Log); ok {
		newProducerBatch := NewProducerBatch(topicId, accumulator.producerConfig, callback, logList)
		accumulator.logTopicData[topicId] = newProducerBatch
	}
}

func (accumulator *Accumulator) innerSendToServer(topicId string, producerBatch *ProducerBatch) {
	accumulator.threadPool.addTask(producerBatch)
	delete(accumulator.logTopicData, topicId)
}

func (accumulator *Accumulator) addLogToProducerBatch(topicId string, logData interface{}, callback CallBack) error {
	if accumulator.shutDownFlag.Load() {
		return errors.New("producer has shutdown and cannot write to new logs")
	}

	defer accumulator.lock.Unlock()
	accumulator.lock.Lock()
	if mlog, ok := logData.(*Log); ok {
		if producerBatch, ok := accumulator.logTopicData[topicId]; ok == true {
			logSize := int64(GetLogSizeCalculate(mlog))
			asyncAtomic.AddInt64(&producerBatch.totalDataSize, logSize)
			asyncAtomic.AddInt64(&accumulator.producer.producerLogGroupSize, logSize)
			accumulator.addOrSendProducerBatch(topicId, producerBatch, mlog, callback)
		} else {
			accumulator.createNewProducerBatch(mlog, callback, topicId)
		}
	} else if logList, ok := logData.([]*Log); ok {
		if producerBatch, ok := accumulator.logTopicData[topicId]; ok == true {
			logListSize := int64(GetLogListSize(logList))
			asyncAtomic.AddInt64(&producerBatch.totalDataSize, logListSize)
			asyncAtomic.AddInt64(&accumulator.producer.producerLogGroupSize, logListSize)
			accumulator.addOrSendProducerBatch(topicId, producerBatch, logList, callback)
		} else {
			accumulator.createNewProducerBatch(logList, callback, topicId)
		}
	} else {
		return errors.New("invalid logType")
	}
	return nil

}
