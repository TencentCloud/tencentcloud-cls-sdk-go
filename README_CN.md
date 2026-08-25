# 腾讯云 CLS 日志服务 Go SDK

[![Go Reference](https://pkg.go.dev/badge/github.com/tencentcloud/tencentcloud-cls-sdk-go.svg)](https://pkg.go.dev/github.com/tencentcloud/tencentcloud-cls-sdk-go)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](./LICENSE)
[![Go Version](https://img.shields.io/badge/go-%3E%3D1.17-00ADD8.svg)](https://go.dev/)

[English](./README.md) | **简体中文**

`tencentcloud-cls-sdk-go` 是[腾讯云日志服务（CLS）](https://cloud.tencent.com/product/cls)的官方 Go SDK，同时提供**日志上传**与**日志消费**两大能力，开箱即用。

---

## 目录

- [功能特性](#功能特性)
- [安装](#安装)
- [快速开始](#快速开始)
- [Endpoint 与地域](#endpoint-与地域)
- [鉴权方式](#鉴权方式)
  - [强鉴权（云 API 密钥）](#强鉴权云-api-密钥)
  - [弱鉴权（免密 / 仅 Uin）](#弱鉴权免密--仅-uin)
  - [临时密钥刷新](#临时密钥刷新)
- [生产者（Producer）](#生产者producer)
  - [异步生产者](#异步生产者)
  - [同步生产者](#同步生产者)
  - [异步生产者配置参数](#异步生产者配置参数)
- [消费者（Consumer）](#消费者consumer)
- [压缩算法](#压缩算法)
- [重新生成 Protobuf](#重新生成-protobuf)
- [License](#license)

---

## 功能特性

- **日志上传**：异步 / 同步生产者；支持攒批聚合、失败重试、回调通知、多种压缩算法（`lz4` / `zstd` / `deflate`）。
- **日志消费**：基于消费组（Consumer Group）的多分区并发消费，自动 offset 持久化与断点续传。
- **鉴权模式**：支持 `AccessKeyID + AccessKeySecret` 永久密钥、`AccessToken` 临时密钥，以及**免密（Uin）**上报。
- **Endpoint 自动拼接**：按 `Region` + `NetworkType` 组合生成域名。
- **健壮性**：`429` / `5xx` 等可恢复错误指数退避自动重试；`4xx` 客户端错误直接失败。
- **高性能**：充分利用 Go 协程并发能力，单实例即可支撑高吞吐上报。

## 安装

要求 Go **1.17+**。

```bash
go get github.com/tencentcloud/tencentcloud-cls-sdk-go
```

## 快速开始

```go
package main

import (
    "fmt"
    "sync"
    "time"

    cls "github.com/tencentcloud/tencentcloud-cls-sdk-go"
)

func main() {
    cfg := cls.GetDefaultAsyncProducerClientConfig()
    cfg.Endpoint = "ap-guangzhou.cls.tencentcs.com"
    cfg.AccessKeyID = "<YourSecretID>"
    cfg.AccessKeySecret = "<YourSecretKey>"

    topicID := "<YourTopicID>"

    producer, err := cls.NewAsyncProducerClient(cfg)
    if err != nil {
        panic(err)
    }
    // 异步 producer 必须先启动
    producer.Start()

    var wg sync.WaitGroup
    cb := &Callback{}
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for j := 0; j < 1000; j++ {
                log := cls.NewCLSLog(time.Now().Unix(), map[string]string{
                    "content":  "hello world",
                    "content2": fmt.Sprintf("%d", j),
                })
                if err := producer.SendLog(topicID, log, cb); err != nil {
                    fmt.Println(err)
                }
            }
        }()
    }
    wg.Wait()
    // 关闭时会 flush 缓冲中的日志，避免丢失
    _ = producer.Close(60000)
}

type Callback struct{}

func (c *Callback) Success(r *cls.Result) {
    for _, a := range r.GetReservedAttempts() {
        fmt.Printf("%+v\n", a)
    }
}

func (c *Callback) Fail(r *cls.Result) {
    fmt.Println(r.IsSuccessful(), r.GetErrorCode(), r.GetErrorMessage(), r.GetRequestId())
}
```

## Endpoint 与地域

有两种方式配置 Endpoint：直接填写字符串，或通过 `SetEndpointByRegionAndNetworkType(region, networkType)` 自动拼接。

```go
cfg := cls.GetDefaultSyncProducerClientConfig()
cfg.SetEndpointByRegionAndNetworkType(cls.Shanghai, cls.Intranet)
// 或者
cfg.Endpoint = "ap-guangzhou.cls.tencentcs.com"
```

Endpoint 填写规则请参考[可用地域](https://cloud.tencent.com/document/product/614/18940#.E5.9F.9F.E5.90.8D) 中 **API 上传日志** Tab 下的域名。

**网络类型**

| 常量 | 值 | 描述 |
| --- | --- | --- |
| `cls.Intranet` | `cls.tencentyun.com` | 内网（VPC） |
| `cls.Extranet` | `cls.tencentcs.com` | 外网（公网） |

**内置地域常量**

`Beijing`、`Guangzhou`、`Shanghai`、`Chengdu`、`Nanjing`、`Chongqing`、`Hongkong`、`Siliconvalley`、`Ashburn`、`Singapore`、`Bangkok`、`Frankfurt`、`Tokyo`、`Seoul`、`Jakarta`、`Saopaulo`、`ShenzhenFSI`、`ShanghaiFSI`、`BeijingFSI`、`ShanghaiADC`。

`Region` / `NetworkType` 底层都是 `string` 类型别名，你可以将任意字符串赋值给它们以支持自定义地域。

## 鉴权方式

### 强鉴权（云 API 密钥）

`AccessKeyID` 与 `AccessKeySecret` 为云 API 密钥，请前往[密钥管理](https://console.cloud.tencent.com/cam/capi)获取。请确保密钥所属账号具备 [SDK 上传日志权限](https://cloud.tencent.com/document/product/614/68374#.E4.BD.BF.E7.94.A8-api-.E4.B8.8A.E4.BC.A0.E6.95.B0.E6.8D.AE)。

```go
cfg.AccessKeyID     = "<YourSecretID>"
cfg.AccessKeySecret = "<YourSecretKey>"
// 可选：使用临时密钥（STS Token）
cfg.AccessToken     = "<YourSecurityToken>"
```

### 弱鉴权（免密 / 仅 Uin）

除云 API 密钥之外，SDK 还支持**弱鉴权（免密）**上报：无需 `AccessKeyID` / `AccessKeySecret`，只需填写账号 `Uin` 即可上报日志。

**前置条件**：目标主题必须开启「**匿名上传 → API/SDK 上传日志**」，否则服务端一律返回 `401 Unauthorized`。

`Uin` 与 `AccessKeyID + AccessKeySecret` **二选一必填**，两者都不填会在创建 client 时直接报错。

```go
// 异步 producer
cfg := cls.GetDefaultAsyncProducerClientConfig()
cfg.Endpoint = "ap-guangzhou.cls.tencentcs.com"
cfg.Uin      = "100012345678" // 账号 Uin

// 同步 producer
scfg := cls.GetDefaultSyncProducerClientConfig()
scfg.Endpoint = "ap-guangzhou.cls.tencentcs.com"
scfg.Uin      = "100012345678"
```

**鉴权模式判定规则**（无需额外开关，SDK 依据凭证自动推断）：

| AccessKeyID + AccessKeySecret | Uin | 鉴权模式 |
| --- | --- | --- |
| ✅ | ❌ | 强鉴权（云 API 签名） |
| ❌ | ✅ | **弱鉴权（免密）** |
| ✅ | ✅ | 强鉴权，`Uin` 被忽略 |
| ❌ | ❌ | 创建 client 时报错 `MissAccessKeyId` |

其他约束：

- `Uin` 必须为**纯数字字符串**，否则报错 `InvalidUin`。
- **AK/SK 优先**：两者同时填写时走强鉴权，避免安全等级被静默降低。
- 弱鉴权模式下调用 `ResetSecretToken()` **不会生效**（会打印一条 warn 日志）。

⚠️ **安全提示**

- 弱鉴权仅用于**日志上传**，日志消费（`consumer`）仍必须使用 AK/SK。
- 弱鉴权只保证传输防篡改，**不提供身份真实性校验**，安全等级等同匿名写入。请仅在可信网络环境中使用，敏感业务请使用云 API 密钥。

### 临时密钥刷新

使用 STS 临时密钥时，需在 Token 过期前调用 `ResetSecretToken` 更新：

```go
// 异步 producer
err := producer.Client.ResetSecretToken(newSecretID, newSecretKey, newSecurityToken)

// 同步 producer
err := syncClient.ResetSecretToken(newSecretID, newSecretKey, newSecurityToken)
```

## 生产者（Producer）

### 异步生产者

异步生产者将日志缓存在内存中攒批发送，`SendLog` / `SendLogList` 立即返回，成功/失败通过用户实现的 `CallBack` 通知。

**为什么要用异步生产者？**

- **异步发送**：调用立即返回，无须等待，支持传入 callback function。
- **优雅关闭**：调用 `Close(timeoutMs)` 时会将缓存中的数据发送完毕，防止日志丢失。
- **感知每一条日志状态**：自定义实现 `CallBack.Success` / `CallBack.Fail` 即可监控每条日志。
- **简单易用**：一份配置搞定攒批、重试、超时等复杂逻辑。
- **失败重试**：`429` / `5xx` 等服务端错误自动重试。
- **高性能**：得益于 Go 语言的高并发能力。

生命周期：

```go
producer, err := cls.NewAsyncProducerClient(cfg)
if err != nil { /* ... */ }
producer.Start()                 // 启动后台协程
_ = producer.SendLog(topicID, log, callback)
_ = producer.SendLogList(topicID, logs, callback)
_ = producer.Close(60_000)       // 最多等待 60 秒完成 flush
```

### 同步生产者

同步生产者在调用协程内直接发送日志，适合需要背压控制或严格顺序语义的场景。

```go
cfg := cls.GetDefaultSyncProducerClientConfig()
cfg.SetEndpointByRegionAndNetworkType(cls.Guangzhou, cls.Extranet)
cfg.AccessKeyID     = "<YourSecretID>"
cfg.AccessKeySecret = "<YourSecretKey>"
cfg.CompressType    = "zstd"

client, err := cls.NewSyncProducerClient(cfg)
if err != nil { /* ... */ }

logs := make([]*cls.Log, 0, 100)
for i := 0; i < 100; i++ {
    logs = append(logs, cls.NewCLSLog(time.Now().Unix(),
        map[string]string{"number": fmt.Sprint(i)}))
}

ctx, cancel := context.WithTimeout(context.Background(), time.Second)
defer cancel()

if err := client.SendLogList(ctx, topicID, logs); err != nil {
    log.Fatal(err)
}
```

服务端限制：**单次请求日志总大小不得超过 5 MB，条数不得超过 10 000**（同步生产者会在客户端做前置校验）。

### 异步生产者配置参数

| 字段 | 类型 | 默认值 | 描述 |
| --- | --- | --- | --- |
| `Endpoint` | `string` | — | CLS 上传域名，如 `ap-guangzhou.cls.tencentcs.com`。 |
| `AccessKeyID` / `AccessKeySecret` | `string` | — | 永久密钥。 |
| `AccessToken` | `string` | — | 可选的 STS 临时安全令牌。 |
| `Uin` | `string` | — | 弱鉴权账号 ID，详见[弱鉴权](#弱鉴权免密--仅-uin)。 |
| `Source` | `string` | 本机 IP | 写入 `LogGroup.Source` 的日志来源字段。 |
| `HostName` | `string` | — | 可选，写入 `LogGroup.Hostname`。 |
| `CompressType` | `string` | `lz4` | 压缩算法，可选 `lz4` / `zstd`。 |
| `Timeout` | `int` (ms) | `10000` | HTTP 请求超时时间。 |
| `IdleConn` | `int` | `50` | 每个 host 的最大空闲连接数。 |
| `TotalSizeLnBytes` | `int64` | `100 MB` | 实例能缓存的日志大小上限。 |
| `MaxSendWorkerCount` | `int64` | `50` | 并发的最大 goroutine 数量。 |
| `MaxBlockSec` | `int` | `60` | 缓存不足时 `SendLog` 的最大阻塞秒数。`0` = 立即失败；负值 = 一直阻塞直到有空间。 |
| `MaxBatchSize` | `int64` | `512 KB` | Batch 触发发送的字节阈值，最大 `5 MB`。 |
| `MaxBatchCount` | `int` | `4096` | Batch 触发发送的条数阈值，最大 `40960`。 |
| `LingerMs` | `int64` | `2000` | Batch 从创建到可发送的最大逗留时间，最小 `100` 毫秒。 |
| `Retries` | `int` | `10` | 单个 Batch 首次失败后可重试次数，`<=0` 表示不重试。 |
| `MaxReservedAttempts` | `int` | `11` | 保留并回传给回调的 attempt 记录数上限。 |
| `BaseRetryBackoffMs` | `int64` | `100` | 首次重试退避基准；第 N 次重试等待 `base * 2^(N-1)`。 |
| `MaxRetryBackoffMs` | `int64` | `50000` | 单次重试的最大退避时间。 |

同步生产者只使用与单次发送相关的子集参数：`Endpoint`、`AccessKeyID/Secret/Token`、`Uin`、`Timeout`、`IdleConn`、`CompressType`、`NeedSource`、`HostName`。

## 消费者（Consumer）

`consumer` 子包提供了功能完整的消费组实现，能力概览：

- **消费组管理**：不存在自动创建，已存在自动复用。
- **心跳与分区分配**：按 `PartitionStrategy=2` 做动态 rebalance，多实例横向扩容自动均衡分区。
- **多分区并发消费**：每个分区独立 goroutine 拉取日志并串行调用业务 `Processor`。
- **Offset 自动持久化**：拉取自动推进 + 60s 周期兜底 + 空闲 30s flush + 退出强制 flush，断点续传无需关心。
- **服务端 DSL 预过滤**：通过 `ConsumerOption.Query` 传入 DSL 表达式，命中日志才下发，节省带宽与客户端 CPU。示例：`log_keep(op_and(op_gt(v("status"), 400), str_exist(v("message"), "access failed")))`，只消费 `status>400` 且 `message` 含 `access failed` 的日志。详见 [日志消费 DSL 过滤语法](https://cloud.tencent.com/document/product/614/37908)。
- **智能停止**：配置 `OffsetEndTime` 后所有分区追上末尾即自动退出整个 worker。
- **优雅退出与失败自愈**：`InvalidOffset` 自动取最新 offset、心跳超时自动重新分配分区、`Process` panic 不阻塞消费。

最简使用方式：实现 `Processor` 接口 → 构造 `ConsumerOption` → `consumer.NewConsumerWorker(option, processor).Run(ctx)`。

详细使用说明、配置参数、并发模型、错误处理、FAQ 以及可运行示例（`consumer/demo/consumer_demo.go`）请参见 [`consumer/README.md`](./consumer/README.md)。

## 压缩算法

| CompressType | 请求头 `x-cls-compress-type` | 备注 |
| --- | --- | --- |
| `lz4`（默认） | `lz4` | `CompressType` 未设置时的默认值。 |
| `zstd` | `zstd` | 压缩率更高，CPU 消耗略高。 |

设置方式：`cfg.CompressType = "zstd"`。

## 重新生成 Protobuf

修改了 `cls.proto` 后，可用如下命令重新生成 Go 代码：

```bash
protoc --gofast_out=. cls.proto
```

## License

本项目基于 [Apache License 2.0](./LICENSE) 开源。
