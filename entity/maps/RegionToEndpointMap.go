package maps

var regionToEndpointMap = map[string]string{
	"北京":      "ap-beijing",
	"广州":      "ap-guangzhou",
	"上海":      "ap-shanghai",
	"成都":      "ap-chengdu",
	"南京":      "ap-nanjing",
	"重庆":      "ap-chongqing",
	"中国香港":    "ap-hongkong",
	"硅谷":      "na-siliconvalley",
	"弗吉尼亚":    "na-ashburn",
	"新加坡":     "ap-singapore",
	"曼谷":      "ap-bangkok",
	"法兰克福":    "eu-frankfurt",
	"东京":      "ap-tokyo",
	"首尔":      "ap-seoul",
	"雅加达":     "ap-jakarta",
	"圣保罗":     "sa-saopaulo",
	"深圳金融":    "ap-shenzhen-fsi",
	"上海金融":    "ap-shanghai-fsi",
	"北京金融":    "ap-beijing-fsi",
	"上海自动驾驶云": "ap-shanghai-adc",
}

// GetEndpointPrefixByRegion 根据地域获取域名前缀
func GetEndpointPrefixByRegion(region string) string {
	if prefix, ok := regionToEndpointMap[region]; ok {
		return prefix
	}
	return ""
}
