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
	// Uin 弱鉴权（免密）账号 ID，与 AccessKeyID/AccessKeySecret 二选一填写。
	// 两者同时填写时以 AccessKeyID/AccessKeySecret 为准（走强鉴权），Uin 被忽略。
	Uin          string
	Timeout      int
	IdleConn     int
	CompressType string
	NeedSource   bool
	HostName     string
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
