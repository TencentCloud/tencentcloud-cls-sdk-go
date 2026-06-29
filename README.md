Tencent CLS Log SDK
---

## 一、SDK 功能说明

`tencentcloud-cls-sdk-go` 是腾讯云日志服务（CLS）官方 Go SDK，提供**日志上传**与**日志消费**两大能力：

- **日志上传**：异步 / 同步生产者，支持攒批聚合、失败重试、回调通知、多种压缩（lz4 / zstd / deflate），开箱即用。
- **日志消费**：基于消费组（Consumer Group）的多分区并发消费、自动 offset 持久化与断点续传，业务侧只需实现一个 `Processor` 接口即可。

通用特性：
- 支持 `AccessKeyID + AccessKeySecret` 永久密钥与 `AccessToken` 临时密钥，并提供运行时刷新。
- 支持按地域 + 网络类型（公网 / 内网 / VPC 内网）自动拼接 endpoint。
- 失败重试：429 / 5xx 等可恢复错误自动指数退避；4xx 类客户端错误直接失败。
- 高性能：充分利用 Go 协程并发能力，单实例可支撑高吞吐上报。

```bash
go get github.com/tencentcloud/tencentcloud-cls-sdk-go
```

---

## 二、上传 SDK 说明

### USAGE

```
go get github.com/tencentcloud/tencentcloud-cls-sdk-go
```

### 为什么要使用CLS Log SDK

- 异步发送：发送日志立即返回，无须等待，支持传入callback function。
- 优雅关闭：通过调用close方法，producer会将所有其缓存的数据进行发送，防止日志丢失。
- 感知每一条日志的成功状态： 用户可以自定义CallBack方法的实现，监控每一条日志的状态
- 使用简单： 通过简单配置，就可以实现复杂的日志上传聚合、失败重试等逻辑
- 失败重试： 429、500 等服务端错误，都会进行重试
- 高性能： 得益于go语言的高并发能力


### CLS Host

endpoint填写请参考[可用地域](https://cloud.tencent.com/document/product/614/18940#.E5.9F.9F.E5.90.8D)中 **API上传日志** Tab中的域名（也可以选择地域与网络环境类型自动生成。如：Guangzhou，Extranet）![image-20230403191435319](https://github.com/TencentCloud/tencentcloud-cls-sdk-js/blob/main/demo.png)

### 密钥信息

AccessKeyID和AccessKeySecret为云API密钥，密钥信息获取请前往[密钥获取](https://console.cloud.tencent.com/cam/capi)。并请确保密钥关联的账号具有相应的[SDK上传日志权限](https://cloud.tencent.com/document/product/614/68374#.E4.BD.BF.E7.94.A8-api-.E4.B8.8A.E4.BC.A0.E6.95.B0.E6.8D.AE)

### Demo

```
package main

import (
	"fmt"
	"github.com/tencentcloud/tencentcloud-cls-sdk-go"
	"sync"
	"time"
)

func main() {
	producerConfig := tencentcloud_cls_sdk_go.GetDefaultAsyncProducerClientConfig()
	producerConfig.Endpoint = "ap-guangzhou.cls.tencentcs.com"
	producerConfig.AccessKeyID = ""
	producerConfig.AccessKeySecret = ""
	topicId := ""
	producerInstance, err := tencentcloud_cls_sdk_go.NewAsyncProducerClient(producerConfig)
	if err != nil {
		fmt.Println(err)
		return
	}

        // 异步发送程序，需要启动
	producerInstance.Start()
	
	var m sync.WaitGroup
	callBack := &Callback{}
	for i := 0; i < 10; i++ {
		m.Add(1)
		go func() {
			defer m.Done()
			for i := 0; i < 1000; i++ {
				log := tencentcloud_cls_sdk_go.NewCLSLog(time.Now().Unix(), map[string]string{"content": "hello world| I'm from Beijing", "content2": fmt.Sprintf("%v", i)})
				err = producerInstance.SendLog(topicId, log, callBack)
				if err != nil {
					fmt.Println(err)
					continue
				}
			}
		}()
	}
	m.Wait()
	producerInstance.Close(60000)
}

type Callback struct {
}

func (callback *Callback) Success(result *tencentcloud_cls_sdk_go.Result) {
	attemptList := result.GetReservedAttempts()
	for _, attempt := range attemptList {
		fmt.Printf("%+v \n", attempt)
	}
}

func (callback *Callback) Fail(result *tencentcloud_cls_sdk_go.Result) {
	fmt.Println(result.IsSuccessful())
	fmt.Println(result.GetErrorCode())
	fmt.Println(result.GetErrorMessage())
	fmt.Println(result.GetReservedAttempts())
	fmt.Println(result.GetRequestId())
	fmt.Println(result.GetTimeStampMs())
}
```

### 配置参数详解

| 参数                | 类型   | 描述                                                         |
| ------------------- | ------ | ------------------------------------------------------------ |
| TotalSizeLnBytes    | Int64  | 实例能缓存的日志大小上限，默认为 100MB。       |
| MaxSendWorkerCount    | Int64  | client能并发的最多"goroutine"的数量，默认为50 |
| MaxBlockSec         | Int    | 如果client可用空间不足，调用者在 send 方法上的最大阻塞时间，默认为 60 秒。<br/>如果超过这个时间后所需空间仍无法得到满足，send 方法会抛出TimeoutException。如果将该值设为0，当所需空间无法得到满足时，send 方法会立即抛出 TimeoutException。如果您希望 send 方法一直阻塞直到所需空间得到满足，可将该值设为负数。 |
| MaxBatchSize        | Int64  | 当一个Batch中缓存的日志大小大于等于 batchSizeThresholdInBytes 时，该 batch 将被发送，默认为 512 KB，最大可设置成 5MB。 |
| MaxBatchCount       | Int    | 当一个Batch中缓存的日志条数大于等于 batchCountThreshold 时，该 batch 将被发送，默认为 4096，最大可设置成 40960。 |
| LingerMs            | Int64  | Batch从创建到可发送的逗留时间，默认为 2 秒，最小可设置成 100 毫秒。 |
| Retries             | Int    | 如果某个Batch首次发送失败，能够对其重试的次数，默认为 10 次。<br/>如果 retries 小于等于 0，该 ProducerBatch 首次发送失败后将直接进入失败队列。 |
| MaxReservedAttempts | Int    | 每个Batch每次被尝试发送都对应着一个Attemp，此参数用来控制返回给用户的 attempt 个数，默认只保留最近的 11 次 attempt 信息。<br/>该参数越大能让您追溯更多的信息，但同时也会消耗更多的内存。 |
| BaseRetryBackoffMs  | Int64  | 首次重试的退避时间，默认为 100 毫秒。 client采样指数退避算法，第 N 次重试的计划等待时间为 baseRetryBackoffMs * 2^(N-1)。 |
| MaxRetryBackoffMs   | Int64  | 重试的最大退避时间，默认为 50 秒。                           |


### generate cls log

```
protoc --gofast_out=. cls.proto
```

---

## 三、消费 SDK 说明

本 SDK 内置了基于「消费组（Consumer Group）」的高阶日志消费模块 `consumer`，主要能力：

- **消费组管理**：不存在自动创建、已存在自动复用。
- **心跳与分区分配**：服务端按 `PartitionStrategy=2` 做动态 rebalance，多实例横向扩容自动均衡分区。
- **多分区并发消费**：每个分区独立 goroutine 拉取日志并串行调用业务 `Processor`。
- **Offset 自动持久化**：拉取自动推进 + 60s 周期兜底 + 空闲 30s flush + 退出强制 flush，断点续传无需关心。
- **智能停止**：配置 `OffsetEndTime` 后所有分区追上末尾即自动退出整个 worker。
- **VPC / 公网切换**：`Internal=true` 时云 API 走 `cls.internal.tencentcloudapi.com`。
- **优雅退出与失败自愈**：`InvalidOffset` 自动取最新 offset、心跳超时自动重新分配分区、`Process` panic 不阻塞消费。

最简使用方式：实现 `Processor` 接口 → 构造 `ConsumerOption` → `consumer.NewConsumerWorker(option, processor).Run(ctx)` 即可。

> 详细使用说明、配置参数详解、并发模型、错误处理、FAQ 以及可运行示例（`consumer/demo/consumer_demo.go`）请参见 [`consumer/README.md`](./consumer/README.md)。

