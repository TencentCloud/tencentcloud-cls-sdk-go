package consumer

import (
	"fmt"
	"go.uber.org/zap"
	tencentcloud_cls_sdk_go "github.com/tencentcloud/tencentcloud-cls-sdk-go"
	"time"
)

// ProcessorBase 对应 Python ConsumerProcessorBase
// 提供 offset 自动提交、初始化、关闭、日志记录等基础行为

type ProcessorBase struct {
	TopicID       string
	PartitionID   int
	LastCheckTime int64
	OffsetTimeout int64
}

// NewProcessorBase 构造函数
func NewProcessorBase() *ProcessorBase {
	return &ProcessorBase{
		TopicID:       "",
		PartitionID:   -1,
		LastCheckTime: time.Now().Unix(),
		OffsetTimeout: 3,
	}
}

// SaveOffset 定时/强制提交 offset
func (p *ProcessorBase) SaveOffset(tracker *OffsetTracker, force bool) {
	zapLogger, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	defer zapLogger.Sync()
	logger := tencentcloud_cls_sdk_go.NewZapLogger(zapLogger)
	currentTime := time.Now().Unix()
	if force || currentTime-p.LastCheckTime > p.OffsetTimeout {
		err := tracker.SaveOffset(true)
		if err != nil {
			logger.Error("Fail to store offset for partition",
				tencentcloud_cls_sdk_go.Field{Key: "topic_id", Value: p.TopicID},
				tencentcloud_cls_sdk_go.Field{Key: "partition_id", Value: p.PartitionID},
				tencentcloud_cls_sdk_go.Field{Key: "error", Value: err.Error()},
			)
		}
		p.LastCheckTime = currentTime
	} else {
		err := tracker.SaveOffset(false)
		if err != nil {
			logger.Error("Fail to store offset for partition",
				tencentcloud_cls_sdk_go.Field{Key: "topic_id", Value: p.TopicID},
				tencentcloud_cls_sdk_go.Field{Key: "partition_id", Value: p.PartitionID},
				tencentcloud_cls_sdk_go.Field{Key: "error", Value: err.Error()},
			)
		}
	}
}

// Initialize 初始化 topic_id
func (p *ProcessorBase) Initialize(topicID string) {
	p.TopicID = topicID
}

// Shutdown 关闭时强制提交 offset，并记录日志
func (p *ProcessorBase) Shutdown(tracker *OffsetTracker) error {
	zapLogger, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	defer zapLogger.Sync()
	logger := tencentcloud_cls_sdk_go.NewZapLogger(zapLogger)
	consumerClient := tracker.consumerGroupClient
	id := fmt.Sprintf("%s/%s/%s/%s/%d",
		consumerClient.LogsetID, p.TopicID,
		consumerClient.ConsumerGroup, consumerClient.Consumer,
		p.PartitionID)
	logger.Info("ConsumerProcessor is shutdown",
		tencentcloud_cls_sdk_go.Field{Key: "consumer_id", Value: id},
		tencentcloud_cls_sdk_go.Field{Key: "partition_id", Value: p.PartitionID},
	)
	p.SaveOffset(tracker, true)
	return nil
}

// Processor 接口
type Processor interface {
	Process(logs []*tencentcloud_cls_sdk_go.Log, tracker *OffsetTracker) (interface{}, error)
	Initialize(topicID string)
	Shutdown(tracker *OffsetTracker) error
	SaveOffset(tracker *OffsetTracker, force bool)
}

type ConsumerProcessorAdaptor struct {
	*ProcessorBase
	Func func(topicID string, partitionID int, logs []*tencentcloud_cls_sdk_go.Log) interface{}
}

func (a *ConsumerProcessorAdaptor) Process(logs []*tencentcloud_cls_sdk_go.Log, tracker *OffsetTracker) (interface{}, error) {
	ret := a.Func(a.TopicID, a.PartitionID, logs)
	if b, ok := ret.(bool); ok && !b {
		// do not save offset when getting False
		return NewTaskResult(nil), nil
	}
	a.SaveOffset(tracker, false)
	return ret, nil
}

// 支持异常传递

type TaskResult struct {
	TaskException error
}

func NewTaskResult(taskException error) *TaskResult {
	return &TaskResult{TaskException: taskException}
}
