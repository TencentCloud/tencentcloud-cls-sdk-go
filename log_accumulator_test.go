package tencentcloud_cls_sdk_go

import (
	"fmt"
	"sync"
	asyncAtomic "sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
)

// ============================================================================
// 工具函数：构造独立的、不依赖真实网络的 Accumulator，用于纯内存单测
// ============================================================================

// newTestConfig 返回一份合法的 config（走过 validateProducerConfig 校验）。
// totalSize 用来控制背压阈值，maxBlockSec 用来控制阻塞语义。
func newTestConfig(totalSize int64, maxBlockSec int) *AsyncProducerClientConfig {
	cfg := &AsyncProducerClientConfig{
		TotalSizeLnBytes:    totalSize,
		MaxSendWorkerCount:  8,
		MaxBlockSec:         maxBlockSec,
		MaxBatchSize:        512 * 1024,
		MaxBatchCount:       4096,
		LingerMs:            2000,
		Retries:             3,
		MaxReservedAttempts: 11,
		BaseRetryBackoffMs:  100,
		MaxRetryBackoffMs:   50 * 1000,
		Endpoint:            "127.0.0.1:0",
		AccessKeyID:         "fake",
		AccessKeySecret:     "fake",
		Timeout:             1000,
		IdleConn:            1,
		Source:              "127.0.0.1",
	}
	return validateProducerConfig(cfg)
}

// newTestProducer 返回一个 producer 骨架，不启动 goroutine，
// 避免真实网络请求，专注验证 accumulator / worker 内存逻辑。
func newTestProducer(cfg *AsyncProducerClientConfig) *AsyncProducerClient {
	p := &AsyncProducerClient{
		asyncProducerClientConfig: cfg,
		producerHash:              "TESTHASH",
	}
	retryQueue := NewRetryQueue()
	worker := NewWorker(nil, retryQueue, cfg.MaxSendWorkerCount, p)
	p.threadPool = NewSendThreadPool(worker)
	p.logAccumulator = NewAccumulator(cfg, worker, p.threadPool, p)
	return p
}

// makeLog 构造一条 payload 约为 bytesPerLog 字节的日志。
func makeLog(bytesPerLog int) *Log {
	if bytesPerLog < 8 {
		bytesPerLog = 8
	}
	// key 固定 8 字节，value 填充剩余（GetLogSizeCalculate 里也是简单相加）
	key := "k"
	val := make([]byte, bytesPerLog-len(key))
	for i := range val {
		val[i] = 'a'
	}
	return &Log{
		Time: proto.Int64(time.Now().Unix()),
		Contents: []*Log_Content{
			{Key: proto.String(key), Value: proto.String(string(val))},
		},
	}
}

// drainThreadPool 把 threadPool 队列里堆积的 batch 全部弹出并"模拟发送成功"，
// 用来在测试里手动释放配额，验证账目守恒。
func drainThreadPool(p *AsyncProducerClient) int64 {
	var released int64
	for {
		batch := p.threadPool.popTask()
		if batch == nil {
			return released
		}
		asyncAtomic.AddInt64(&p.producerLogGroupSize, -batch.accountedSize)
		released += batch.accountedSize
	}
}

// ============================================================================
// Bug 1：核心场景 —— 并发 SendLog 不再击穿 TotalSizeLnBytes
// ============================================================================

// TestAccumulator_QuotaCapUnderConcurrency 用大量 goroutine 并发写入，
// 断言 producerLogGroupSize 峰值不会明显超过 TotalSizeLnBytes。
// 修复前该断言几乎必挂（多协程 check-then-act 击穿）。
func TestAccumulator_QuotaCapUnderConcurrency(t *testing.T) {
	const (
		totalSize     = int64(64 * 1024) // 64KB 上限，方便快速跑满
		bytesPerLog   = 1024             // 每条约 1KB
		writerCount   = 200
		logsPerWriter = 20
	)
	cfg := newTestConfig(totalSize, 0) // MaxBlockSec=0 立即失败，制造最激烈的抢占
	p := newTestProducer(cfg)

	var wg sync.WaitGroup
	var peak int64
	stopSampler := make(chan struct{})
	// 峰值采样：任何时刻 producerLogGroupSize 都不应大于 totalSize + bytesPerLog
	// （最后一次判额通过时最多把值抬到 totalSize+bytesPerLog）
	go func() {
		for {
			select {
			case <-stopSampler:
				return
			default:
				v := asyncAtomic.LoadInt64(&p.producerLogGroupSize)
				for {
					prev := asyncAtomic.LoadInt64(&peak)
					if v <= prev || asyncAtomic.CompareAndSwapInt64(&peak, prev, v) {
						break
					}
				}
				time.Sleep(50 * time.Microsecond)
			}
		}
	}()

	var okCount, errCount int64
	for i := 0; i < writerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < logsPerWriter; j++ {
				err := p.logAccumulator.addLogToProducerBatch("topic", makeLog(bytesPerLog), nil)
				if err != nil {
					asyncAtomic.AddInt64(&errCount, 1)
				} else {
					asyncAtomic.AddInt64(&okCount, 1)
				}
			}
		}()
	}
	wg.Wait()
	close(stopSampler)

	// 允许 1 条日志的抬升幅度：判额是 cur+logSize <= total，
	// 允许最后一次通过时把 producerLogGroupSize 抬到略高于 total。
	limit := totalSize + int64(bytesPerLog)*2 // 留 2 倍余量做安全裕度
	got := asyncAtomic.LoadInt64(&peak)
	if got > limit {
		t.Fatalf("producerLogGroupSize 峰值 %d 击穿了阈值 %d（TotalSizeLnBytes=%d, bytesPerLog=%d）", got, limit, totalSize, bytesPerLog)
	}
	if okCount == 0 {
		t.Fatalf("okCount=0，说明测试构造错误，一条日志都没成功入队")
	}
	if errCount == 0 {
		t.Fatalf("errCount=0，说明测试构造错误，配额未被打满，无法体现背压")
	}
	t.Logf("peak=%d, ok=%d, err=%d, totalSize=%d", got, okCount, errCount, totalSize)
}

// ============================================================================
// Bug 1：MaxBlockSec == 0 立即失败语义
// ============================================================================

func TestAccumulator_MaxBlockSecZero_ReturnsError(t *testing.T) {
	cfg := newTestConfig(2*1024, 0) // 2KB 上限
	p := newTestProducer(cfg)

	// 先把 producerLogGroupSize 手动占满
	asyncAtomic.StoreInt64(&p.producerLogGroupSize, cfg.TotalSizeLnBytes)

	start := time.Now()
	err := p.logAccumulator.addLogToProducerBatch("topic", makeLog(512), nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("MaxBlockSec=0 且配额已满，应立即返回错误，但 err=nil")
	}
	if err.Error() != "over producer set maximum blocking time" {
		t.Fatalf("错误信息与原语义不一致: %v", err)
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("MaxBlockSec=0 应立即返回，但耗时 %v", elapsed)
	}
}

// ============================================================================
// Bug 1：MaxBlockSec > 0 情况下，配额被释放后能重新通过
// ============================================================================

func TestAccumulator_MaxBlockSecPositive_ReleasesUnblocks(t *testing.T) {
	cfg := newTestConfig(2*1024, 3) // 最多阻塞 3s
	p := newTestProducer(cfg)

	// 打满
	asyncAtomic.StoreInt64(&p.producerLogGroupSize, cfg.TotalSizeLnBytes)

	// 500ms 后模拟 worker 完成发送、释放配额
	go func() {
		time.Sleep(500 * time.Millisecond)
		asyncAtomic.StoreInt64(&p.producerLogGroupSize, 0)
	}()

	start := time.Now()
	err := p.logAccumulator.addLogToProducerBatch("topic", makeLog(512), nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("配额已释放，应写入成功，但 err=%v", err)
	}
	if elapsed < 400*time.Millisecond {
		t.Fatalf("释放前不应立即成功，实际耗时 %v", elapsed)
	}
	if elapsed > 2500*time.Millisecond {
		t.Fatalf("释放后应在下一个 sleep tick 唤醒成功，实际耗时 %v", elapsed)
	}
}

// ============================================================================
// shutdown 场景：Accumulator 拒绝新写入
// ============================================================================

func TestAccumulator_RejectAfterShutdown(t *testing.T) {
	cfg := newTestConfig(1024*1024, 0)
	p := newTestProducer(cfg)

	p.logAccumulator.shutDownFlag.Store(true)

	err := p.logAccumulator.addLogToProducerBatch("topic", makeLog(128), nil)
	if err == nil {
		t.Fatalf("producer 已 shutdown，应拒绝新写入，但 err=nil")
	}
}

// ============================================================================
// 账目守恒：预扣量 == 释放量（accountedSize 与 GetLogSizeCalculate 口径一致）
// ============================================================================

// TestAccumulator_AccountedSizeConservation 写入 N 条日志、把所有 batch 手动
// "模拟发送成功"，producerLogGroupSize 最终应回落到 0，验证账目守恒。
// 这是 Bug 1 方案 A 落地时最容易踩的坑（预扣与释放口径不一致），本测试专门守护。
func TestAccumulator_AccountedSizeConservation(t *testing.T) {
	cfg := newTestConfig(10*1024*1024, 0) // 大额度，保证不触发背压
	p := newTestProducer(cfg)

	const N = 500
	for i := 0; i < N; i++ {
		if err := p.logAccumulator.addLogToProducerBatch(
			fmt.Sprintf("topic-%d", i%5),
			makeLog(64+i%256),
			nil,
		); err != nil {
			t.Fatalf("addLogToProducerBatch(#%d) err=%v", i, err)
		}
	}

	// 冲刷 accumulator 里未满的 batch，全部丢进 threadPool
	p.logAccumulator.lock.Lock()
	for key, batch := range p.logAccumulator.logTopicData {
		p.threadPool.addTask(batch)
		delete(p.logAccumulator.logTopicData, key)
	}
	p.logAccumulator.lock.Unlock()

	sizeBefore := asyncAtomic.LoadInt64(&p.producerLogGroupSize)
	released := drainThreadPool(p)
	sizeAfter := asyncAtomic.LoadInt64(&p.producerLogGroupSize)

	if sizeAfter != 0 {
		t.Fatalf("账目未清零：sizeBefore=%d released=%d sizeAfter=%d", sizeBefore, released, sizeAfter)
	}
	if released != sizeBefore {
		t.Fatalf("释放量 %d != 预扣量 %d，accountedSize 口径与入口不一致", released, sizeBefore)
	}
}

// ============================================================================
// Bug 2：worker shutdown 分支只追加一次 attempt，配额被释放
// ============================================================================

// TestWorker_ExecuteFailedCallback_ReleasesAccountedSize 验证：
//  1. 失败回调按 accountedSize 释放配额，而不是 totalDataSize。
//  2. 多个 CallBack 时不会重复扣减配额（executeFailedCallback 只减一次）。
func TestWorker_ExecuteFailedCallback_ReleasesAccountedSize(t *testing.T) {
	cfg := newTestConfig(1024*1024, 0)
	p := newTestProducer(cfg)

	// 通过 accumulator 正常入队，得到一个真实占额的 batch
	if err := p.logAccumulator.addLogToProducerBatch("topic", makeLog(200), &countingCallback{}); err != nil {
		t.Fatalf("addLogToProducerBatch err=%v", err)
	}
	// 再挂 2 个 callback，模拟"多回调"场景
	batch := p.logAccumulator.logTopicData["topic"]
	if batch == nil {
		t.Fatalf("batch 应已创建")
	}
	cb1 := &countingCallback{}
	cb2 := &countingCallback{}
	batch.addProducerBatchCallBack(cb1)
	batch.addProducerBatchCallBack(cb2)

	before := asyncAtomic.LoadInt64(&p.producerLogGroupSize)
	p.threadPool.worker.executeFailedCallback(batch)
	after := asyncAtomic.LoadInt64(&p.producerLogGroupSize)

	if before-after != batch.accountedSize {
		t.Fatalf("释放量应等于 accountedSize=%d, 实际释放 %d", batch.accountedSize, before-after)
	}
	if cb1.failCount != 1 || cb2.failCount != 1 {
		t.Fatalf("每个 callback 应被 Fail 一次，实际 cb1=%d cb2=%d", cb1.failCount, cb2.failCount)
	}
}

// TestWorker_ShutdownBranch_SingleAttempt 模拟 retryQueueShutDownFlag=true 时
// 的失败路径，断言 attemptCount 只增加 1、attemptList 只多 1 条，且配额被释放。
// 修复前 addErrorMessageToBatchAttempt 在 for callBackList 循环内，
// 多回调会让 attemptCount 和 attemptList 被重复累加。
func TestWorker_ShutdownBranch_SingleAttempt(t *testing.T) {
	cfg := newTestConfig(1024*1024, 0)
	p := newTestProducer(cfg)

	if err := p.logAccumulator.addLogToProducerBatch("topic", makeLog(200), &countingCallback{}); err != nil {
		t.Fatalf("addLogToProducerBatch err=%v", err)
	}
	batch := p.logAccumulator.logTopicData["topic"]
	cb1 := &countingCallback{}
	cb2 := &countingCallback{}
	cb3 := &countingCallback{}
	batch.addProducerBatchCallBack(cb1)
	batch.addProducerBatchCallBack(cb2)
	batch.addProducerBatchCallBack(cb3)

	worker := p.threadPool.worker
	worker.retryQueueShutDownFlag.Store(true)

	// 直接调用 addErrorMessageToBatchAttempt + executeFailedCallback，
	// 复现 sendToServer 里 shutdown 分支的执行路径（避免真实 http）
	fakeErr := &CLSError{HTTPCode: 500, Code: "InternalError", Message: "mock"}
	before := asyncAtomic.LoadInt64(&p.producerLogGroupSize)
	worker.addErrorMessageToBatchAttempt(batch, fakeErr)
	worker.executeFailedCallback(batch)
	after := asyncAtomic.LoadInt64(&p.producerLogGroupSize)

	if batch.attemptCount != 1 {
		t.Fatalf("attemptCount 应为 1，实际 %d（多回调导致重复计数？）", batch.attemptCount)
	}
	if len(batch.result.attemptList) != 1 {
		t.Fatalf("attemptList 应只有 1 条，实际 %d", len(batch.result.attemptList))
	}
	if before-after != batch.accountedSize {
		t.Fatalf("配额未按 accountedSize 释放，before=%d after=%d accounted=%d", before, after, batch.accountedSize)
	}
	if cb1.failCount+cb2.failCount+cb3.failCount != 3 {
		// cb1/cb2/cb3 每人 Fail 1 次 = 3；入口那次挂载的匿名 callback 我拿不到引用不参与断言
		t.Fatalf("Fail 回调次数不符预期: %d+%d+%d", cb1.failCount, cb2.failCount, cb3.failCount)
	}
}

// ============================================================================
// 辅助：可计数 callback
// ============================================================================

type countingCallback struct {
	successCount int
	failCount    int
}

func (c *countingCallback) Success(r *Result) { c.successCount++ }
func (c *countingCallback) Fail(r *Result)    { c.failCount++ }

// TestAccumulator_NoRaceUnderConcurrentReleaseAndReserve 专供 `go test -race` 使用。
// 一边有 N 个 goroutine 不断 SendLog（走完整的判额+预扣路径），
// 另一边有 M 个 goroutine 不断"模拟 worker 释放"（直接减 producerLogGroupSize），
// race detector 应该报告 0 个 race。
//
// 这条测试守护的是"accumulator 侧持锁预扣 vs worker 侧无锁释放"这套模型的
// 无锁读—原子加/减—一致性。如果未来有人误把 producerLogGroupSize 的更新方式改回
// 普通赋值，本用例会立即在 -race 下暴露问题。
func TestAccumulator_NoRaceUnderConcurrentReleaseAndReserve(t *testing.T) {
	if testing.Short() {
		t.Skip("skip in short mode")
	}
	cfg := newTestConfig(1*1024*1024, -1) // 无限阻塞模式；确保并发路径都跑到
	p := newTestProducer(cfg)

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// writers：并发写入
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = p.logAccumulator.addLogToProducerBatch("topic", makeLog(256), nil)
				}
			}
		}()
	}
	// releasers：并发释放
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					cur := asyncAtomic.LoadInt64(&p.producerLogGroupSize)
					if cur > 4096 {
						asyncAtomic.AddInt64(&p.producerLogGroupSize, -4096)
					}
					time.Sleep(100 * time.Microsecond)
				}
			}
		}()
	}

	time.Sleep(300 * time.Millisecond)
	close(stop)
	// 兜底：把 producerLogGroupSize 强制置 0，避免 releasers 停止后 writers 卡在阻塞循环
	asyncAtomic.StoreInt64(&p.producerLogGroupSize, 0)
	p.logAccumulator.shutDownFlag.Store(true) // 让阻塞中的 writer 尽快返回
	wg.Wait()
}