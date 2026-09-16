package tencentcloud_cls_sdk_go

import (
	"errors"
	"testing"
)

func TestNewYunApiLogClientFromConfigMissingCredentials(t *testing.T) {
	cases := []struct {
		name        string
		config      YunApiLogClientConfig
		wantErrCode string
		wantErrMsg  string
	}{
		{
			name:        "AccessKeyId 与 AccessKey 均为空",
			config:      YunApiLogClientConfig{},
			wantErrCode: MISS_ACCESS_KEY_ID,
			wantErrMsg:  "accessKeyId cannot be empty",
		},
		{
			name:        "仅 AccessKeyId 为空",
			config:      YunApiLogClientConfig{AccessKey: "sk"},
			wantErrCode: MISS_ACCESS_KEY_ID,
			wantErrMsg:  "accessKeyId cannot be empty",
		},
		{
			name:        "仅 AccessKey 为空",
			config:      YunApiLogClientConfig{AccessKeyId: "ak"},
			wantErrCode: MISS_ACCESS_SECRET,
			wantErrMsg:  "accessKey cannot be empty",
		},
		{
			name:        "仅填 Region 不足以构造客户端",
			config:      YunApiLogClientConfig{Region: "ap-guangzhou"},
			wantErrCode: MISS_ACCESS_KEY_ID,
			wantErrMsg:  "accessKeyId cannot be empty",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewYunApiLogClientFromConfig(tc.config)
			if err == nil {
				t.Fatal("NewYunApiLogClientFromConfig should return error")
			}
			if client != nil {
				t.Fatalf("client should be nil when config is invalid, got %#v", client)
			}

			var clsErr *CLSError
			if !errors.As(err, &clsErr) {
				t.Fatalf("error should be *CLSError, got %T", err)
			}
			if clsErr.Code != tc.wantErrCode {
				t.Fatalf("error code = %q, want %q", clsErr.Code, tc.wantErrCode)
			}
			if clsErr.Message != tc.wantErrMsg {
				t.Fatalf("error message = %q, want %q", clsErr.Message, tc.wantErrMsg)
			}
		})
	}
}

func TestNewYunApiLogClientFromConfigEndpoint(t *testing.T) {
	cases := []struct {
		name         string
		config       YunApiLogClientConfig
		wantEndpoint string
	}{
		{
			name:         "未指定 Region 使用默认公网域名",
			config:       YunApiLogClientConfig{AccessKeyId: "ak", AccessKey: "sk"},
			wantEndpoint: "cls.tencentcloudapi.com",
		},
		{
			name:         "指定 Region 使用地域公网域名",
			config:       YunApiLogClientConfig{AccessKeyId: "ak", AccessKey: "sk", Region: "ap-guangzhou"},
			wantEndpoint: "cls.ap-guangzhou.tencentcloudapi.com",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewYunApiLogClientFromConfig(tc.config)
			if err != nil {
				t.Fatalf("NewYunApiLogClientFromConfig returned error: %v", err)
			}
			if client == nil {
				t.Fatal("client should not be nil")
			}
			if client.CLSClient == nil {
				t.Fatal("embedded CLSClient should not be nil")
			}
			if client.CLSClient.options.Host != tc.wantEndpoint {
				t.Fatalf("host = %q, want %q", client.CLSClient.options.Host, tc.wantEndpoint)
			}
			if client.region != tc.config.Region {
				t.Fatalf("region = %q, want %q", client.region, tc.config.Region)
			}
		})
	}
}

func TestNewYunApiLogClientFromConfigPassesCredentials(t *testing.T) {
	config := YunApiLogClientConfig{
		AccessKeyId:   "test-ak",
		AccessKey:     "test-sk",
		SecurityToken: "test-token",
		Source:        "test-source",
		Region:        "ap-beijing",
	}

	client, err := NewYunApiLogClientFromConfig(config)
	if err != nil {
		t.Fatalf("NewYunApiLogClientFromConfig returned error: %v", err)
	}
	if client.secretId != config.AccessKeyId {
		t.Fatalf("secretId = %q, want %q", client.secretId, config.AccessKeyId)
	}
	if client.secretKey != config.AccessKey {
		t.Fatalf("secretKey = %q, want %q", client.secretKey, config.AccessKey)
	}
	if client.source != config.Source {
		t.Fatalf("source = %q, want %q", client.source, config.Source)
	}

	credentials := client.CLSClient.options.Credentials
	if credentials.SecretID != config.AccessKeyId || credentials.SecretKEY != config.AccessKey {
		t.Fatalf("credentials = %#v, want AK/SK from config", credentials)
	}
	if credentials.SecretToken != config.SecurityToken {
		t.Fatalf("SecretToken = %q, want %q", credentials.SecretToken, config.SecurityToken)
	}
	if client.CLSClient.options.isWeakAuth() {
		t.Fatal("cloud api client must use strong (TC3) auth")
	}
}

func TestDeprecatedYunApiLogClientConstructorsInvalid(t *testing.T) {
	if client := NewYunApiLogClient("", "", false, "", "", "ap-guangzhou"); client != nil {
		t.Fatalf("NewYunApiLogClient should return nil, got %#v", client)
	}
	if client := NewYunApiLogClientWithConfig(YunApiLogClientConfig{AccessKeyId: "ak"}); client != nil {
		t.Fatalf("NewYunApiLogClientWithConfig should return nil, got %#v", client)
	}
	if client := NewYunApiLogClientSimple("", "sk"); client != nil {
		t.Fatalf("NewYunApiLogClientSimple should return nil, got %#v", client)
	}
}

func TestDeprecatedYunApiLogClientConstructorsDelegate(t *testing.T) {
	client := NewYunApiLogClient("ak", "sk", false, "token", "source", "ap-shanghai")
	if client == nil || client.CLSClient == nil {
		t.Fatal("NewYunApiLogClient should return a usable client")
	}
	if client.CLSClient.options.Host != "cls.ap-shanghai.tencentcloudapi.com" {
		t.Fatalf("host = %q", client.CLSClient.options.Host)
	}
	if client.secretId != "ak" || client.secretKey != "sk" || client.source != "source" {
		t.Fatalf("unexpected client fields: %#v", client)
	}
	if client.CLSClient.options.Credentials.SecretToken != "token" {
		t.Fatalf("SecretToken = %q", client.CLSClient.options.Credentials.SecretToken)
	}

	withConfig := NewYunApiLogClientWithConfig(YunApiLogClientConfig{
		AccessKeyId: "ak",
		AccessKey:   "sk",
		Region:      "ap-shanghai",
	})
	if withConfig == nil || withConfig.CLSClient == nil {
		t.Fatal("NewYunApiLogClientWithConfig should return a usable client")
	}
	if withConfig.CLSClient.options.Host != "cls.ap-shanghai.tencentcloudapi.com" {
		t.Fatalf("host = %q", withConfig.CLSClient.options.Host)
	}

	simple := NewYunApiLogClientSimple("ak", "sk")
	if simple == nil || simple.CLSClient == nil {
		t.Fatal("NewYunApiLogClientSimple should return a usable client")
	}
	if simple.CLSClient.options.Host != "cls.tencentcloudapi.com" {
		t.Fatalf("host = %q", simple.CLSClient.options.Host)
	}
	if simple.region != "" || simple.source != "" {
		t.Fatalf("unexpected defaults: %#v", simple)
	}
}
