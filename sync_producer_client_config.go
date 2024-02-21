package tencentcloud_cls_sdk_go

// SyncProducerClientConfig sync producer config
type SyncProducerClientConfig struct {
	Endpoint        string
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
