package tencentcloud_cls_sdk_go

import (
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
)

// SyncProducerClientConfig sync producer config
type SyncProducerClientConfig struct {
	Endpoint     string
	Timeout      int
	IdleConn     int
	CompressType string
	NeedSource   bool
	HostName     string
	Credential   common.CredentialIface
}

// GetDefaultSyncProducerClientConfig get default sync producer config
func GetDefaultSyncProducerClientConfig() *SyncProducerClientConfig {
	return &SyncProducerClientConfig{
		Timeout:    10000,
		IdleConn:   50,
		NeedSource: true,
	}
}
