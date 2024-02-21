package tencentcloud_cls_sdk_go

import (
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
)

type ProducerBatch struct {
	totalDataSize        int64
	lock                 sync.RWMutex
	logGroup             *LogGroup
	logGroupSize         int
	logGroupCount        int
	attemptCount         int
	baseRetryBackoffMs   int64
	nextRetryMs          int64
	maxRetryIntervalInMs int64
	callBackList         []CallBack
	createTimeMs         int64
	maxRetryTimes        int
	topicID              string
	result               *Result
	packageID            string
	maxReservedAttempts  int
}

// NewProducerBatch 初始化Producer batch
func NewProducerBatch(topicID string, config *AsyncProducerClientConfig, callBackFunc CallBack, logData interface{}, packageID string) *ProducerBatch {
	var logs = make([]*Log, 0)
	if log, ok := logData.(*Log); ok {
		logs = append(logs, log)
	} else if logList, ok := logData.([]*Log); ok {
		logs = append(logs, logList...)
	}

	logGroup := &LogGroup{
		Logs:        logs,
		Source:      proto.String(config.Source),
		Hostname:    proto.String(config.HostName),
		ContextFlow: proto.String(packageID),
	}
	currentTimeMs := GetTimeMs(time.Now().UnixNano())

	producerBatch := &ProducerBatch{
		logGroup:             logGroup,
		attemptCount:         0,
		maxRetryIntervalInMs: config.MaxRetryBackoffMs,
		callBackList:         []CallBack{},
		createTimeMs:         currentTimeMs,
		maxRetryTimes:        config.Retries,
		baseRetryBackoffMs:   config.BaseRetryBackoffMs,
		topicID:              topicID,
		result:               NewResult(),
		maxReservedAttempts:  config.MaxReservedAttempts,
		packageID:            packageID,
	}
	producerBatch.totalDataSize = int64(producerBatch.logGroup.Size())
	if callBackFunc != nil {
		producerBatch.callBackList = append(producerBatch.callBackList, callBackFunc)
	}
	return producerBatch
}

func (producerBatch *ProducerBatch) getTopicID() string {
	defer producerBatch.lock.RUnlock()
	producerBatch.lock.RLock()
	return producerBatch.topicID
}

func (producerBatch *ProducerBatch) getLogGroupCount() int {
	defer producerBatch.lock.RUnlock()
	producerBatch.lock.RLock()
	return len(producerBatch.logGroup.GetLogs())
}

func (producerBatch *ProducerBatch) addLogToLogGroup(log interface{}) {
	defer producerBatch.lock.Unlock()
	producerBatch.lock.Lock()
	if item, ok := log.(*Log); ok {
		producerBatch.logGroup.Logs = append(producerBatch.logGroup.Logs, item)
	} else if logList, ok := log.([]*Log); ok {
		producerBatch.logGroup.Logs = append(producerBatch.logGroup.Logs, logList...)
	}
}

func (producerBatch *ProducerBatch) addProducerBatchCallBack(callBack CallBack) {
	defer producerBatch.lock.Unlock()
	producerBatch.lock.Lock()
	producerBatch.callBackList = append(producerBatch.callBackList, callBack)
}
