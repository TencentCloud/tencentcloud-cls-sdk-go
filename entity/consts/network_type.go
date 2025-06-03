package consts

// NetworkType 网络类型,可以将string赋值给该类型，实现自定义网络类型
type NetworkType string

const (
	Intranet NetworkType = "cls.tencentyun.com" //内网
	Extranet NetworkType = "cls.tencentcs.com"  //外网
)
