package tencentcloud_cls_sdk_go

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// AsyncProducerClient async producer client
type AsyncProducerClient struct {
	asyncProducerClientConfig *AsyncProducerClientConfig
	logAccumulator            *Accumulator
	threadPool                *SendThreadPool
	timerTask                 *TimerSendLogTask
	workerWaitGroup           *sync.WaitGroup
	timerTaskWaitGroup        *sync.WaitGroup
	sendThreadPoolWaitGroup   *sync.WaitGroup
	producerLogGroupSize      int64
	Client                    *CLSClient
	producerHash              string
}

// NewAsyncProducerClient 初始化Async Producer Client
func NewAsyncProducerClient(asyncProducerClientConfig *AsyncProducerClientConfig) (*AsyncProducerClient, error) {
	asyncProducerClient := new(AsyncProducerClient)
	asyncProducerClient.asyncProducerClientConfig = validateProducerConfig(asyncProducerClientConfig)
	client, err := NewCLSClient(&Options{
		Host:         asyncProducerClientConfig.Endpoint,
		Timeout:      asyncProducerClientConfig.Timeout,
		IdleConn:     asyncProducerClientConfig.IdleConn,
		CompressType: asyncProducerClientConfig.CompressType,
		Credentials: Credentials{
			SecretID:    asyncProducerClientConfig.AccessKeyID,
			SecretKEY:   asyncProducerClientConfig.AccessKeySecret,
			SecretToken: asyncProducerClientConfig.AccessToken,
		},
	})
	if err != nil {
		return nil, errors.New(err.Message)
	}
	asyncProducerClient.Client = client
	ip, _ := GetLocalIP()
	instanceID := fmt.Sprintf("%s-%d", ip, time.Now().UnixNano())
	asyncProducerClient.producerHash = GenerateProducerHash(instanceID)
	retryQueue := NewRetryQueue()
	worker := NewWorker(client, retryQueue, asyncProducerClient.asyncProducerClientConfig.MaxSendWorkerCount, asyncProducerClient)
	asyncProducerClient.threadPool = NewSendThreadPool(worker)
	asyncProducerClient.logAccumulator = NewAccumulator(asyncProducerClient.asyncProducerClientConfig, worker, asyncProducerClient.threadPool, asyncProducerClient)
	asyncProducerClient.timerTask = NewTimerSendLogTask(asyncProducerClient.logAccumulator, retryQueue, worker, asyncProducerClient.threadPool)
	asyncProducerClient.workerWaitGroup = &sync.WaitGroup{}
	asyncProducerClient.timerTaskWaitGroup = &sync.WaitGroup{}
	asyncProducerClient.sendThreadPoolWaitGroup = &sync.WaitGroup{}
	return asyncProducerClient, nil
}

// validateProducerConfig 校验config配置是否超过最大值
func validateProducerConfig(producerConfig *AsyncProducerClientConfig) *AsyncProducerClientConfig {
	if producerConfig.MaxReservedAttempts <= 0 {
		producerConfig.MaxReservedAttempts = 11
	}
	if producerConfig.MaxBatchCount > 40960 || producerConfig.MaxBatchCount <= 0 {
		producerConfig.MaxBatchCount = 40960
	}
	if producerConfig.MaxBatchSize > 1024*1024*5 || producerConfig.MaxBatchSize <= 0 {
		producerConfig.MaxBatchSize = 1024 * 1024 * 5
	}
	if producerConfig.MaxSendWorkerCount <= 0 {
		producerConfig.MaxSendWorkerCount = 50
	}
	if producerConfig.BaseRetryBackoffMs <= 0 {
		producerConfig.BaseRetryBackoffMs = 100
	}
	if producerConfig.TotalSizeLnBytes <= 0 {
		producerConfig.TotalSizeLnBytes = 100 * 1024 * 1024
	}
	if producerConfig.LingerMs < 100 {
		producerConfig.LingerMs = 2000
	}
	if producerConfig.IdleConn <= 0 {
		producerConfig.IdleConn = 50
	}
	if producerConfig.Timeout <= 0 {
		producerConfig.Timeout = 10000
	}
	if producerConfig.MaxRetryBackoffMs <= 0 {
		producerConfig.MaxRetryBackoffMs = 50 * 1000
	}
	if producerConfig.Source == "" {
		producerConfig.Source, _ = GetLocalIP()
	}
	return producerConfig
}

func (producer *AsyncProducerClient) SendLog(topicId string, log *Log, callback CallBack) error {
	err := producer.waitTime()
	if err != nil {
		return err
	}
	return producer.logAccumulator.addLogToProducerBatch(topicId, log, callback)
}

func (producer *AsyncProducerClient) SendLogList(topicId string, logList []*Log, callback CallBack) (err error) {
	err = producer.waitTime()
	if err != nil {
		return err
	}
	return producer.logAccumulator.addLogToProducerBatch(topicId, logList, callback)
}

func (producer *AsyncProducerClient) waitTime() error {
	if producer.asyncProducerClientConfig.MaxBlockSec > 0 {
		for i := 0; i < producer.asyncProducerClientConfig.MaxBlockSec; i++ {
			if atomic.LoadInt64(&producer.producerLogGroupSize) > producer.asyncProducerClientConfig.TotalSizeLnBytes {
				time.Sleep(time.Second)
			} else {
				return nil
			}
		}
		return errors.New("over producer set maximum blocking time")
	} else if producer.asyncProducerClientConfig.MaxBlockSec == 0 {
		if atomic.LoadInt64(&producer.producerLogGroupSize) > producer.asyncProducerClientConfig.TotalSizeLnBytes {
			return errors.New("over producer set maximum blocking time")
		}
	} else if producer.asyncProducerClientConfig.MaxBlockSec < 0 {
		for {
			if atomic.LoadInt64(&producer.producerLogGroupSize) > producer.asyncProducerClientConfig.TotalSizeLnBytes {
				time.Sleep(time.Second)
			} else {
				return nil
			}
		}
	}
	return nil
}

func (producer *AsyncProducerClient) Start() {
	producer.timerTaskWaitGroup.Add(1)
	go producer.timerTask.run(producer.timerTaskWaitGroup, producer.asyncProducerClientConfig)
	producer.sendThreadPoolWaitGroup.Add(1)
	go producer.threadPool.start(producer.workerWaitGroup, producer.sendThreadPoolWaitGroup)
}

func (producer *AsyncProducerClient) Close(timeoutMs int64) error {
	startCloseTime := time.Now()
	producer.sendCloseProducerSignal()
	producer.timerTaskWaitGroup.Wait()
	producer.threadPool.shutDownFlag.Store(true)
	for {
		if atomic.LoadInt64(&producer.timerTask.worker.taskCount) == 0 && !producer.threadPool.hasTask() {
			return nil
		}
		if time.Since(startCloseTime) > time.Duration(timeoutMs)*time.Millisecond {
			return errors.New("the producer timeout closes, and some of the cached data may not be sent properly")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (producer *AsyncProducerClient) sendCloseProducerSignal() {
	producer.timerTask.shutDownFlag.Store(true)
	producer.logAccumulator.shutDownFlag.Store(true)
	producer.timerTask.worker.retryQueueShutDownFlag.Store(true)
}
