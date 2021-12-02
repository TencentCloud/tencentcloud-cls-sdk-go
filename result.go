package tencentcloud_cls_sdk_go

type Result struct {
	attemptList []*Attempt
	successful  bool
}

// NewResult init result
func NewResult() *Result {
	return &Result{
		attemptList: []*Attempt{},
		successful:  false,
	}
}

func (result *Result) IsSuccessful() bool {
	return result.successful
}

func (result *Result) GetReservedAttempts() []*Attempt {
	return result.attemptList
}
