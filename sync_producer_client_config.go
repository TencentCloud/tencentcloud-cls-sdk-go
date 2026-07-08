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
	// Resolver 可选：设置后每次发送前会动态解析 Host（如通过北极星服务发现）。
	// 若 Resolver 不为 nil，Endpoint 可以为空，或作为兜底使用。
	Resolver EndpointResolver
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
