package tencentcloud_cls_sdk_go

import "fmt"

// SyncProducerClientConfig sync producer config
type SyncProducerClientConfig struct {
	Endpoint        string
	region          Region      // 地域
	networkType     NetworkType // 网络类型
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

// SetEndpointByRegionAndNetworkType 根据地域和网络类型设置域名
func (config *SyncProducerClientConfig) SetEndpointByRegionAndNetworkType(region Region, networkType NetworkType) {
	config.region = region
	config.networkType = networkType
	config.Endpoint = fmt.Sprintf("%s.%s", config.region, config.networkType)
}
