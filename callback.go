package tencentcloud_cls_sdk_go

type CallBack interface {
	Success(result *Result)
	Fail(result *Result)
}
