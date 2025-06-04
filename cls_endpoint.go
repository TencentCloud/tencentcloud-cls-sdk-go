package tencentcloud_cls_sdk_go

// NetworkType 网络类型,可以将string赋值给该类型，实现自定义网络类型
type NetworkType string

// Region 地域类型,可以将string赋值给该类型，实现自定义地域
type Region string

const (
	Intranet NetworkType = "cls.tencentyun.com" //内网
	Extranet NetworkType = "cls.tencentcs.com"  //外网
)

const (
	Beijing       Region = "ap-beijing"
	Guangzhou     Region = "ap-guangzhou"
	Shanghai      Region = "ap-shanghai"
	Chengdu       Region = "ap-chengdu"
	Nanjing       Region = "ap-nanjing"
	Chongqing     Region = "ap-chongqing"
	Hongkong      Region = "ap-hongkong"
	Siliconvalley Region = "na-siliconvalley"
	Ashburn       Region = "na-ashburn"
	Singapore     Region = "ap-singapore"
	Bangkok       Region = "ap-bangkok"
	Frankfurt     Region = "eu-frankfurt"
	Tokyo         Region = "ap-tokyo"
	Seoul         Region = "ap-seoul"
	Jakarta       Region = "ap-jakarta"
	Saopaulo      Region = "sa-saopaulo"
	ShenzhenFSI   Region = "ap-shenzhen-fsi"
	ShanghaiFSI   Region = "ap-shanghai-fsi"
	BeijingFSI    Region = "ap-beijing-fsi"
	ShanghaiADC   Region = "ap-shanghai-adc"
)
