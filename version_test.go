package tencentcloud_cls_sdk_go

import (
	"regexp"
	"strings"
	"testing"
)

// TestVersionFormat 校验版本号为语义化版本格式，防止误填非法值
func TestVersionFormat(t *testing.T) {
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(version) {
		t.Errorf("版本号应为 x.y.z 格式，实际 %q", version)
	}
	if getVersion() != version {
		t.Errorf("getVersion() 期望 %q，实际 %q", version, getVersion())
	}
}

// TestGetUserAgent 校验 User-Agent 的组装格式，服务端依赖该格式统计客户端版本分布
func TestGetUserAgent(t *testing.T) {
	got := getUserAgent()
	want := userAgent + "-" + version
	if got != want {
		t.Errorf("User-Agent 期望 %q，实际 %q", want, got)
	}
	if !strings.HasPrefix(got, "cls-go-sdk-") {
		t.Errorf("User-Agent 应以 cls-go-sdk- 开头，实际 %q", got)
	}
}

// TestSetUserAgentAndVersion 校验自定义 UA / 版本号后能正确反映到 User-Agent
func TestSetUserAgentAndVersion(t *testing.T) {
	originAgent, originVersion := userAgent, version
	defer func() {
		userAgent, version = originAgent, originVersion
	}()

	SetUserAgent("custom-agent")
	SetVersion("9.9.9")
	if got := getUserAgent(); got != "custom-agent-9.9.9" {
		t.Errorf("User-Agent 期望 custom-agent-9.9.9，实际 %q", got)
	}
}
