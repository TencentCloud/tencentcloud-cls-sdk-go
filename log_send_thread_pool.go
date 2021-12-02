package tencentcloud_cls_sdk_go

import (
	"container/list"
	"log"
	"sync"
	"time"

	"go.uber.org/atomic"
)

type SendThreadPool struct {
	shutDownFlag *atomic.Bool
	queue        *list.List
	lock         sync.RWMutex
	worker       *Worker
	logger       log.Logger
}

func NewSendThreadPool(worker *Worker) *SendThreadPool {
	return &SendThreadPool{
		shutDownFlag: atomic.NewBool(false),
		queue:        list.New(),
		worker:       worker,
	}
}

func (threadPool *SendThreadPool) addTask(batch *ProducerBatch) {
	defer threadPool.lock.Unlock()
	threadPool.lock.Lock()
	threadPool.queue.PushBack(batch)
}

func (threadPool *SendThreadPool) popTask() *ProducerBatch {
	defer threadPool.lock.Unlock()
	threadPool.lock.Lock()
	if threadPool.queue.Len() <= 0 {
		return nil
	}
	ele := threadPool.queue.Front()
	threadPool.queue.Remove(ele)
	return ele.Value.(*ProducerBatch)
}

func (threadPool *SendThreadPool) hasTask() bool {
	defer threadPool.lock.RUnlock()
	threadPool.lock.RLock()
	return threadPool.queue.Len() > 0
}

func (threadPool *SendThreadPool) start(ioWorkerWaitGroup *sync.WaitGroup, ioThreadPoolwait *sync.WaitGroup) {
	defer ioThreadPoolwait.Done()
	for {
		if task := threadPool.popTask(); task != nil {
			threadPool.worker.startSendTask(ioWorkerWaitGroup)
			go func(producerBatch *ProducerBatch) {
				defer threadPool.worker.closeSendTask(ioWorkerWaitGroup)
				threadPool.worker.sendToServer(producerBatch)
			}(task)
		} else {
			if !threadPool.shutDownFlag.Load() {
				time.Sleep(100 * time.Millisecond)
			} else {
				break
			}
		}
	}

}
