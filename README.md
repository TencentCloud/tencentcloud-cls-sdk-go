# Tencent CLS Log SDK for Go

[![Go Reference](https://pkg.go.dev/badge/github.com/tencentcloud/tencentcloud-cls-sdk-go.svg)](https://pkg.go.dev/github.com/tencentcloud/tencentcloud-cls-sdk-go)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](./LICENSE)
[![Go Version](https://img.shields.io/badge/go-%3E%3D1.17-00ADD8.svg)](https://go.dev/)

**English** | [简体中文](./README_CN.md)

The official Go SDK for [Tencent Cloud Log Service (CLS)](https://cloud.tencent.com/product/cls), providing **log upload** and **log consumption** capabilities out of the box.

---

## Table of Contents

- [Features](#features)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Endpoints & Regions](#endpoints--regions)
- [Authentication](#authentication)
  - [Strong Auth (API Key)](#strong-auth-api-key)
  - [Weak Auth (Anonymous / Uin only)](#weak-auth-anonymous--uin-only)
  - [Refreshing Temporary Credentials](#refreshing-temporary-credentials)
- [Producer](#producer)
  - [Async Producer](#async-producer)
  - [Sync Producer](#sync-producer)
  - [Async Producer Config Reference](#async-producer-config-reference)
- [Consumer](#consumer)
- [Compression](#compression)
- [Regenerate Protobuf](#regenerate-protobuf)
- [License](#license)

---

## Features

- **Log Upload**: async / sync producer, batching & aggregation, retry, callbacks, multiple compression codecs (`lz4` / `zstd` / `deflate`).
- **Log Consumption**: consumer group based multi-partition concurrent consumption, automatic offset persistence and resume-from-checkpoint.
- **Auth**: supports both permanent credentials (`AccessKeyID` + `AccessKeySecret`) and temporary credentials (`AccessToken`); also supports **anonymous (Uin only)** upload.
- **Endpoint**: automatic endpoint composition by `Region` + `NetworkType`.
- **Resilience**: exponential backoff retry on retryable errors (429 / 5xx); fast-fail on 4xx client errors.
- **High Throughput**: fully leverages Go's concurrency; a single instance can sustain high ingestion QPS.

## Installation

Requires Go **1.17+**.

```bash
go get github.com/tencentcloud/tencentcloud-cls-sdk-go
```

## Quick Start

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

## Endpoints & Regions

You may either fill `Endpoint` directly or use `SetEndpointByRegionAndNetworkType(region, networkType)` to compose it automatically.

```go
cfg := cls.GetDefaultSyncProducerClientConfig()
cfg.SetEndpointByRegionAndNetworkType(cls.Shanghai, cls.Intranet)
// or
cfg.Endpoint = "ap-guangzhou.cls.tencentcs.com"
```

**Network types**

| Constant | Value | Description |
| --- | --- | --- |
| `cls.Intranet` | `cls.tencentyun.com` | Private network (VPC) |
| `cls.Extranet` | `cls.tencentcs.com` | Public Internet |

**Supported regions** (see [Available Regions](https://cloud.tencent.com/document/product/614/18940#.E5.9F.9F.E5.90.8D) for the latest list):

`Beijing`, `Guangzhou`, `Shanghai`, `Chengdu`, `Nanjing`, `Chongqing`, `Hongkong`, `Siliconvalley`, `Ashburn`, `Singapore`, `Bangkok`, `Frankfurt`, `Tokyo`, `Seoul`, `Jakarta`, `Saopaulo`, `ShenzhenFSI`, `ShanghaiFSI`, `BeijingFSI`, `ShanghaiADC`.

The `Region` / `NetworkType` types are `string` aliases — you can freely define your own values when needed.

## Authentication

### Strong Auth (API Key)

Get your `AccessKeyID` / `AccessKeySecret` from the [CAM console](https://console.cloud.tencent.com/cam/capi). Make sure the credential owner has the [permission to upload logs via API/SDK](https://cloud.tencent.com/document/product/614/68374#.E4.BD.BF.E7.94.A8-api-.E4.B8.8A.E4.BC.A0.E6.95.B0.E6.8D.AE).

```go
cfg.AccessKeyID     = "<YourSecretID>"
cfg.AccessKeySecret = "<YourSecretKey>"
// Optional: use a temporary security token (STS)
cfg.AccessToken     = "<YourSecurityToken>"
```

### Weak Auth (Anonymous / Uin only)

Besides API keys, the SDK also supports **anonymous upload** using only the account `Uin` — no `AccessKeyID` / `AccessKeySecret` is required.

**Prerequisite**: the target topic MUST have **Anonymous Upload → API/SDK log upload** enabled, otherwise the server responds `401 Unauthorized`.

`Uin` and `AccessKeyID + AccessKeySecret` are **mutually exclusive but one is required**; creating a client with neither returns an error.

```go
// Async producer
cfg := cls.GetDefaultAsyncProducerClientConfig()
cfg.Endpoint = "ap-guangzhou.cls.tencentcs.com"
cfg.Uin      = "100012345678" // account Uin

// Sync producer
scfg := cls.GetDefaultSyncProducerClientConfig()
scfg.Endpoint = "ap-guangzhou.cls.tencentcs.com"
scfg.Uin      = "100012345678"
```

**Auth mode is inferred from credentials:**

| AccessKeyID + AccessKeySecret | Uin | Auth Mode |
| --- | --- | --- |
| ✅ | ❌ | Strong (Cloud API signature) |
| ❌ | ✅ | **Weak (anonymous)** |
| ✅ | ✅ | Strong; `Uin` is ignored |
| ❌ | ❌ | `MissAccessKeyId` error at client creation |

Additional rules:

- `Uin` must be a **digits-only** string, otherwise `InvalidUin` is returned.
- **AK/SK take precedence** — if both are set, strong auth is used so the security level is never silently downgraded.
- `ResetSecretToken()` is a **no-op** in weak-auth mode (a warn log is printed).

⚠️ **Security notice**

- Weak auth is for **log upload only**; log consumption (`consumer`) still requires AK/SK.
- Weak auth only guarantees transport tamper-resistance; **it does NOT authenticate the caller identity**. The security level is equivalent to anonymous write. Use it only in trusted network environments.

### Refreshing Temporary Credentials

When using STS temporary credentials, call `ResetSecretToken` before the token expires:

```go
// Async producer
err := producer.Client.ResetSecretToken(newSecretID, newSecretKey, newSecurityToken)

// Sync producer
err := syncClient.ResetSecretToken(newSecretID, newSecretKey, newSecurityToken)
```

## Producer

### Async Producer

The async producer buffers logs in memory, aggregates them into batches and dispatches them via a worker pool. It returns immediately from `SendLog` / `SendLogList` and delivers success/failure via a user-defined `CallBack`.

**Why use the async producer?**

- **Non-blocking send** with per-log callbacks.
- **Graceful shutdown**: `Close(timeoutMs)` flushes buffered data before returning.
- **Per-log observability**: implement your own `CallBack.Success` / `CallBack.Fail`.
- **Simple config**: one struct configures batching, retry, timeouts and more.
- **Auto retry**: retries on 429 / 5xx errors with exponential backoff.
- **High throughput**: engineered on top of goroutines.

Basic lifecycle:

```go
producer, err := cls.NewAsyncProducerClient(cfg)
if err != nil { /* ... */ }
producer.Start()                 // start background workers
_ = producer.SendLog(topicID, log, callback)
_ = producer.SendLogList(topicID, logs, callback)
_ = producer.Close(60_000)       // wait up to 60s for graceful drain
```

### Sync Producer

The sync producer sends logs immediately in the calling goroutine. Choose it when you need back-pressure or strict per-call semantics.

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

Server-side constraints enforced by the sync producer: **a single request must not exceed 5 MB and 10 000 log entries**.

### Async Producer Config Reference

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `Endpoint` | `string` | — | CLS endpoint host, e.g. `ap-guangzhou.cls.tencentcs.com`. |
| `AccessKeyID` / `AccessKeySecret` | `string` | — | Permanent API credentials. |
| `AccessToken` | `string` | — | Optional STS security token. |
| `Uin` | `string` | — | Weak-auth (anonymous) account ID. See [Weak Auth](#weak-auth-anonymous--uin-only). |
| `Source` | `string` | local IP | Log `source` field written into `LogGroup.Source`. |
| `HostName` | `string` | — | Optional hostname written into `LogGroup.Hostname`. |
| `CompressType` | `string` | `lz4` | `lz4` or `zstd`. |
| `Timeout` | `int` (ms) | `10000` | HTTP request timeout. |
| `IdleConn` | `int` | `50` | Max idle connections per host. |
| `TotalSizeLnBytes` | `int64` | `100 MB` | Max in-memory buffered bytes. |
| `MaxSendWorkerCount` | `int64` | `50` | Max concurrent send goroutines. |
| `MaxBlockSec` | `int` | `60` | Max seconds `SendLog` blocks when the buffer is full. `0` = fail immediately; negative = block forever. |
| `MaxBatchSize` | `int64` | `512 KB` | Batch flush threshold (bytes). Max `5 MB`. |
| `MaxBatchCount` | `int` | `4096` | Batch flush threshold (entries). Max `40960`. |
| `LingerMs` | `int64` | `2000` | Max time a batch may linger before being flushed (min `100`). |
| `Retries` | `int` | `10` | Retry attempts on retryable errors. `<=0` disables retry. |
| `MaxReservedAttempts` | `int` | `11` | Max attempt records returned to the callback. |
| `BaseRetryBackoffMs` | `int64` | `100` | Base backoff for the first retry (exponential: `base * 2^(N-1)`). |
| `MaxRetryBackoffMs` | `int64` | `50000` | Retry backoff upper bound. |

The sync producer only uses the subset relevant to per-call sending: `Endpoint`, `AccessKeyID/Secret/Token`, `Uin`, `Timeout`, `IdleConn`, `CompressType`, `NeedSource`, `HostName`.

## Consumer

The `consumer` sub-package provides a full-featured consumer-group implementation:

- **Consumer group management**: auto-create if missing, reuse otherwise.
- **Heartbeat & partition assignment**: dynamic rebalance via `PartitionStrategy=2`; scale out horizontally.
- **Multi-partition concurrency**: each partition has its own goroutine that fetches logs and serially invokes your `Processor`.
- **Offset persistence**: advanced on fetch, flushed every 60 s, flushed after 30 s idle, force-flushed on exit — resume-from-checkpoint is transparent.
- **Server-side DSL pre-filter**: set `ConsumerOption.Query` to a [DSL expression](https://cloud.tencent.com/document/product/614/37908) so only matching logs are delivered, saving bandwidth and CPU. Example: `log_keep(op_and(op_gt(v("status"), 400), str_exist(v("message"), "access failed")))`.
- **Bounded consumption**: with `OffsetEndTime` set, the worker auto-exits when all partitions catch up.
- **Graceful degradation**: `InvalidOffset` auto-recovers to the latest offset; heartbeat timeout triggers reassignment; a panicking `Process` does not stall the pipeline.

Minimal usage: implement the `Processor` interface → build a `ConsumerOption` → `consumer.NewConsumerWorker(option, processor).Run(ctx)`.

For detailed configuration, concurrency model, error handling, FAQ and a runnable example (`consumer/demo/consumer_demo.go`), see [`consumer/README.md`](./consumer/README.md).

## Compression

| CompressType | Header `x-cls-compress-type` | Notes |
| --- | --- | --- |
| `lz4` (default) | `lz4` | Default when `CompressType` is empty. |
| `zstd` | `zstd` | Better compression ratio, slightly more CPU. |

Set it via `cfg.CompressType = "zstd"`.

## Regenerate Protobuf

If you modified `cls.proto`, regenerate the Go stub:

```bash
protoc --gofast_out=. cls.proto
```

## License

Distributed under the [Apache License 2.0](./LICENSE).