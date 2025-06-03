package tencentcloud_cls_sdk_go

import "github.com/tencentcloud/tencentcloud-cls-sdk-go/entity/consts"

// SyncProducerClientConfig sync producer config
type SyncProducerClientConfig struct {
	Endpoint        string
	Region          consts.Region
	NetworkType     consts.NetworkType
	AccessKeyID     string
	AccessKeySecret string
	AccessToken     string
	Timeout         int
	IdleConn        int
	CompressType    string
	NeedSource      bool
	HostName        string
}

// GetDefaultSyncProducerClientConfig get default sync producer config
func GetDefaultSyncProducerClientConfig() *SyncProducerClientConfig {
	return &SyncProducerClientConfig{
		Timeout:    10000,
		IdleConn:   50,
		NeedSource: true,
	}
}
