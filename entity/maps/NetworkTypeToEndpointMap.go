package maps

var networkTypeToEndpointMap = map[string]string{
	"内网": "cls.tencentyun.com",
	"外网": "cls.tencentcs.com",
}

// GetEndpointSuffixByNetworkType 根据网络类型获取域名后缀
func GetEndpointSuffixByNetworkType(networkType string) string {
	return networkTypeToEndpointMap[networkType]
}
