package tencentcloud_cls_sdk_go

import (
	"errors"
	"sync"
	asyncAtomic "sync/atomic"
	"time"

	"go.uber.org/atomic"
)

// errOverMaxBlock 与原 waitTime 返回的错误信息保持一致，
// 便于外部调用方按字符串判断行为不发生变化。
var errOverMaxBlock = errors.New("over producer set maximum blocking time")

type Accumulator struct {
	lock           sync.RWMutex
	logTopicData   map[string]*ProducerBatch
	producerConfig *AsyncProducerClientConfig
	worker         *Worker
	shutDownFlag   *atomic.Bool
	threadPool     *SendThreadPool
	producer       *AsyncProducerClient
	batchID        *atomic.Int64
	producerHash   string
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
		batchID:        atomic.NewInt64(0),
		producerHash:   producer.producerHash,
	}
}

func (accumulator *Accumulator) addOrSendProducerBatch(topicId string, producerBatch *ProducerBatch, log interface{}, callback CallBack, logSize int64) {
	totalDataCount := producerBatch.getLogGroupCount() + 1
	if producerBatch.totalDataSize > accumulator.producerConfig.MaxBatchSize &&
		producerBatch.totalDataSize < 5242880 &&
		totalDataCount <= accumulator.producerConfig.MaxBatchCount {
		producerBatch.addLogToLogGroup(log)
		asyncAtomic.AddInt64(&producerBatch.accountedSize, logSize)
		if callback != nil {
			producerBatch.addProducerBatchCallBack(callback)
		}
		accumulator.innerSendToServer(topicId, producerBatch)
	} else if producerBatch.totalDataSize <= accumulator.producerConfig.MaxBatchSize &&
		totalDataCount <= accumulator.producerConfig.MaxBatchCount {
		producerBatch.addLogToLogGroup(log)
		asyncAtomic.AddInt64(&producerBatch.accountedSize, logSize)
		if callback != nil {
			producerBatch.addProducerBatchCallBack(callback)
		}
	} else {
		// 老 batch 立即冲刷，为新 log 新建 batch；新 batch 内部会用同样的口径
		// 自行初始化 accountedSize，配额收支保持一致。
		accumulator.innerSendToServer(topicId, producerBatch)
		accumulator.createNewProducerBatch(log, callback, topicId)
	}
}

func (accumulator *Accumulator) createNewProducerBatch(logType interface{}, callback CallBack, topicId string) {
	if item, ok := logType.(*Log); ok {
		newProducerBatch := NewProducerBatch(topicId, accumulator.producerConfig, callback, item, generatePackageId(accumulator.producerHash, accumulator.batchID))
		accumulator.logTopicData[topicId] = newProducerBatch
	} else if logList, ok := logType.([]*Log); ok {
		newProducerBatch := NewProducerBatch(topicId, accumulator.producerConfig, callback, logList, generatePackageId(accumulator.producerHash, accumulator.batchID))
		accumulator.logTopicData[topicId] = newProducerBatch
	}
}

func (accumulator *Accumulator) innerSendToServer(topicId string, producerBatch *ProducerBatch) {
	accumulator.threadPool.addTask(producerBatch)
	delete(accumulator.logTopicData, topicId)
}

// tryReserveAndAppend 在 accumulator.lock 内完成 3 步：
//  1. 校验 shutDownFlag；
//  2. 判断 producerLogGroupSize+logSize 是否超过 TotalSizeLnBytes；
//  3. 若通过，累加 producerLogGroupSize 并把日志 append 到 batch。
//
// 三步在同一临界区里执行，彻底避免了原来 waitTime（无锁读）与 append（加锁写）
// 之间的 check-then-act race，从根本上保证 producerLogGroupSize 不会被并发写击穿。
//
// 返回值：
//   - (true, nil)  成功入队
//   - (false, nil) 配额不足，调用方需要根据 MaxBlockSec 决定 sleep 或 return
//   - (false, err) 明确错误（例如 producer 已 shutdown）
func (accumulator *Accumulator) tryReserveAndAppend(topicId string, logData interface{}, callback CallBack, logSize int64) (bool, error) {
	accumulator.lock.Lock()
	defer accumulator.lock.Unlock()

	if accumulator.shutDownFlag.Load() {
		return false, errors.New("producer has shutdown and cannot write to new logs")
	}

	// 锁内读一次最新值：worker 侧的 -= 是无锁 atomic，
	// 但只会让当前读到的值更小（更宽松），不会误判为超限。
	cur := asyncAtomic.LoadInt64(&accumulator.producer.producerLogGroupSize)
	if cur+logSize > accumulator.producerConfig.TotalSizeLnBytes {
		return false, nil
	}

	// 通过配额检查 —— 一次性把额度扣掉，再 append。
	asyncAtomic.AddInt64(&accumulator.producer.producerLogGroupSize, logSize)

	if mlog, ok := logData.(*Log); ok {
		if producerBatch, ok := accumulator.logTopicData[topicId]; ok {
			asyncAtomic.AddInt64(&producerBatch.totalDataSize, logSize)
			accumulator.addOrSendProducerBatch(topicId, producerBatch, mlog, callback, logSize)
		} else {
			accumulator.createNewProducerBatch(mlog, callback, topicId)
		}
	} else if logList, ok := logData.([]*Log); ok {
		if producerBatch, ok := accumulator.logTopicData[topicId]; ok {
			asyncAtomic.AddInt64(&producerBatch.totalDataSize, logSize)
			accumulator.addOrSendProducerBatch(topicId, producerBatch, logList, callback, logSize)
		} else {
			accumulator.createNewProducerBatch(logList, callback, topicId)
		}
	} else {
		// 回滚：类型不支持，把已扣的额度还回去。
		asyncAtomic.AddInt64(&accumulator.producer.producerLogGroupSize, -logSize)
		return false, errors.New("invalid logType")
	}
	return true, nil
}

// addLogToProducerBatch 是对外入口。锁外算 logSize，锁内做 tryReserveAndAppend，
// 未通过时按 MaxBlockSec 语义 sleep-and-retry 或立即返回 error，
// 与原 waitTime 的三种分支（>0 / ==0 / <0）行为对齐。
func (accumulator *Accumulator) addLogToProducerBatch(topicId string, logData interface{}, callback CallBack) error {
	if accumulator.shutDownFlag.Load() {
		return errors.New("producer has shutdown and cannot write to new logs")
	}

	// 锁外算好本次待写入的字节数，避免在临界区内做 O(N) 遍历。
	var logSize int64
	switch v := logData.(type) {
	case *Log:
		sz, err := GetLogSizeCalculate(v)
		if err != nil {
			return err
		}
		logSize = int64(sz)
	case []*Log:
		sz, err := GetLogListSize(v)
		if err != nil {
			return err
		}
		logSize = int64(sz)
	default:
		return errors.New("invalid logType")
	}

	maxBlockSec := accumulator.producerConfig.MaxBlockSec

	// 立即失败模式：一次判额，不满足即 return。
	if maxBlockSec == 0 {
		ok, err := accumulator.tryReserveAndAppend(topicId, logData, callback, logSize)
		if err != nil {
			return err
		}
		if !ok {
			return errOverMaxBlock
		}
		return nil
	}

	// 无限阻塞模式：只要配额不足就 sleep，直到成功或 shutdown。
	if maxBlockSec < 0 {
		for {
			ok, err := accumulator.tryReserveAndAppend(topicId, logData, callback, logSize)
			if err != nil {
				return err
			}
			if ok {
				return nil
			}
			time.Sleep(time.Second)
		}
	}

	// 有限阻塞模式：最多 maxBlockSec 次每次 sleep 1s，
	// 语义与原 waitTime 循环形态严格一致（尝试 N 次，最终失败）。
	for i := 0; i < maxBlockSec; i++ {
		ok, err := accumulator.tryReserveAndAppend(topicId, logData, callback, logSize)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		time.Sleep(time.Second)
	}
	return errOverMaxBlock
}
