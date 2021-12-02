package tencentcloud_cls_sdk_go

import (
	"encoding/json"
)

// Error defines sls error
type CLSError struct {
	HTTPCode  int32  `json:"httpCode"`
	Code      string `json:"errorCode"`
	Message   string `json:"errorMessage"`
	RequestID string `json:"requestID"`
}

// NewClientError new client error
func NewError(httpCode int32, requestID, errorCode string, err error) *CLSError {
	if err == nil {
		return nil
	}
	e := new(CLSError)
	e.HTTPCode = httpCode
	e.Code = errorCode
	e.Message = err.Error()
	e.RequestID = requestID
	return e
}

func (e CLSError) String() string {
	b, err := json.MarshalIndent(e, "", "    ")
	if err != nil {
		return ""
	}
	return string(b)
}

func (e CLSError) Error() string {
	return e.String()
}
