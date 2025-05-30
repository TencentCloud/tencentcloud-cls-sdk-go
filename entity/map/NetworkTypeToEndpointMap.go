package _map

var networkTypeToEndpointMap = map[string]string{
	"内网": "cls.tencentyun.comg",
	"外网": "cls.tencentcs.com",
}

func GetEndpointSuffixByNetworkType(networkType string) string {
	return networkTypeToEndpointMap[networkType]
}
