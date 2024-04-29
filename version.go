package tencentcloud_cls_sdk_go

import (
	"fmt"
)

var userAgent = "cls-go-sdk"
var version = "1.0.7"

// GetUserAgent ...
func getUserAgent() string {
	return fmt.Sprintf("%s-%s", userAgent, version)
}

// GetVersion ...
func getVersion() string {
	return version
}

// SetUserAgent ...
func SetUserAgent(agent string) {
	userAgent = agent
}

// SetVersion ...
func SetVersion(ver string) {
	version = ver
}
