package tencentcloud_cls_sdk_go

import (
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
)

// AsyncProducerClientConfig Producer Config
type AsyncProducerClientConfig struct {
	TotalSizeLnBytes    int64
	MaxSendWorkerCount  int64
	MaxBlockSec         int
	MaxBatchSize        int64
	MaxBatchCount       int
	LingerMs            int64
	Retries             int
	MaxReservedAttempts int
	BaseRetryBackoffMs  int64
	MaxRetryBackoffMs   int64
	Endpoint            string
	Source              string
	Timeout             int
	IdleConn            int
	CompressType        string
	HostName            string
	Credential          common.CredentialIface
}

// GetDefaultAsyncProducerClientConfig 获取默认的Producer Config
func GetDefaultAsyncProducerClientConfig() *AsyncProducerClientConfig {
	return &AsyncProducerClientConfig{
		TotalSizeLnBytes:    100 * 1024 * 1024,
		MaxSendWorkerCount:  50,
		MaxBlockSec:         60,
		MaxBatchSize:        512 * 1024,
		LingerMs:            2000,
		Retries:             10,
		MaxReservedAttempts: 11,
		BaseRetryBackoffMs:  100,
		MaxRetryBackoffMs:   50 * 1000,
		MaxBatchCount:       4096,
		Timeout:             10000,
		IdleConn:            50,
	}
}
