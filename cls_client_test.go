package tencentcloud_cls_sdk_go

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestValidateOptionsAuthMode 覆盖强/弱鉴权的凭证校验判定表
func TestValidateOptionsAuthMode(t *testing.T) {
	cases := []struct {
		name         string
		credentials  Credentials
		host         string
		wantErrCode  string
		wantWeakAuth bool
	}{
		{
			name:        "host 为空",
			host:        "",
			credentials: Credentials{SecretID: "id", SecretKEY: "key"},
			wantErrCode: MISSING_HOST,
		},
		{
			name:         "AK/SK 齐全走强鉴权",
			host:         "ap-guangzhou.cls.tencentcs.com",
			credentials:  Credentials{SecretID: "id", SecretKEY: "key"},
			wantWeakAuth: false,
		},
		{
			name:         "仅填 Uin 走弱鉴权",
			host:         "ap-guangzhou.cls.tencentcs.com",
			credentials:  Credentials{Uin: "100012345678"},
			wantWeakAuth: true,
		},
		{
			name:         "AK/SK 与 Uin 并存时以 AK/SK 为准",
			host:         "ap-guangzhou.cls.tencentcs.com",
			credentials:  Credentials{SecretID: "id", SecretKEY: "key", Uin: "100012345678"},
			wantWeakAuth: false,
		},
		{
			name:        "AK/SK 与 Uin 均为空",
			host:        "ap-guangzhou.cls.tencentcs.com",
			credentials: Credentials{},
			wantErrCode: MISS_ACCESS_KEY_ID,
		},
		{
			name:        "仅填 SecretID 且无 Uin",
			host:        "ap-guangzhou.cls.tencentcs.com",
			credentials: Credentials{SecretID: "id"},
			wantErrCode: MISS_ACCESS_KEY_ID,
		},
		{
			name:        "Uin 含非数字字符",
			host:        "ap-guangzhou.cls.tencentcs.com",
			credentials: Credentials{Uin: "10001234567a"},
			wantErrCode: INVALID_UIN,
		},
		{
			name:        "Uin 为负数",
			host:        "ap-guangzhou.cls.tencentcs.com",
			credentials: Credentials{Uin: "-100012345678"},
			wantErrCode: INVALID_UIN,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			options := &Options{Host: c.host, Credentials: c.credentials}
			err := options.validateOptions()
			if c.wantErrCode != "" {
				if err == nil {
					t.Fatalf("期望错误码 %s，实际通过校验", c.wantErrCode)
				}
				if err.Code != c.wantErrCode {
					t.Fatalf("期望错误码 %s，实际 %s", c.wantErrCode, err.Code)
				}
				return
			}
			if err != nil {
				t.Fatalf("期望校验通过，实际返回错误 %s", err.Code)
			}
			if got := options.isWeakAuth(); got != c.wantWeakAuth {
				t.Fatalf("期望 isWeakAuth=%v，实际 %v", c.wantWeakAuth, got)
			}
			if options.CompressType != "lz4" {
				t.Fatalf("期望默认压缩类型 lz4，实际 %s", options.CompressType)
			}
		})
	}
}

// TestSendWeakAuthHeaders 校验弱鉴权与强鉴权两种模式下请求头的组装差异
func TestSendWeakAuthHeaders(t *testing.T) {
	cases := []struct {
		name        string
		credentials Credentials
		wantHeaders map[string]string
		wantAbsent  []string
	}{
		{
			name:        "弱鉴权带明文身份头且不带签名",
			credentials: Credentials{Uin: "100012345678"},
			wantHeaders: map[string]string{
				headerAuthMode: authModeWeak,
				headerUin:      "100012345678",
			},
			wantAbsent: []string{"Authorization", "X-Cls-Token"},
		},
		{
			name:        "强鉴权带签名且不带弱鉴权头",
			credentials: Credentials{SecretID: "id", SecretKEY: "key", SecretToken: "token"},
			wantHeaders: map[string]string{
				"X-Cls-Token": "token",
			},
			wantAbsent: []string{headerAuthMode, headerUin},
		},
		{
			name:        "AK/SK 与 Uin 并存时按强鉴权发送",
			credentials: Credentials{SecretID: "id", SecretKEY: "key", Uin: "100012345678"},
			wantAbsent:  []string{headerAuthMode, headerUin, "X-Cls-Token"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got http.Header
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Clone()
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := NewCLSClient(&Options{
				Host:        strings.TrimPrefix(server.URL, "http://"),
				Credentials: c.credentials,
			})
			if err != nil {
				t.Fatalf("创建 client 失败：%s", err.Code)
			}

			if sendErr := client.Send(context.Background(), "topic-id", &LogGroup{}); sendErr != nil {
				t.Fatalf("发送失败：%s", sendErr.Message)
			}

			for key, want := range c.wantHeaders {
				if got.Get(key) != want {
					t.Errorf("请求头 %s 期望 %q，实际 %q", key, want, got.Get(key))
				}
			}
			for _, key := range c.wantAbsent {
				if got.Get(key) != "" {
					t.Errorf("请求头 %s 不应存在，实际 %q", key, got.Get(key))
				}
			}
			// 强鉴权必须携带签名，弱鉴权必须不携带
			isWeak := c.credentials.SecretID == "" || c.credentials.SecretKEY == ""
			if !isWeak && got.Get("Authorization") == "" {
				t.Error("强鉴权请求缺少 Authorization 头")
			}
		})
	}
}

// TestSendWeakAuthUnauthorized 校验服务端弱鉴权失败返回的 401 能正确映射为 CLSError
func TestSendWeakAuthUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Cls-Requestid", "req-123")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errorcode":"Unauthorized","errormessage":"weak auth failed"}`))
	}))
	defer server.Close()

	client, err := NewCLSClient(&Options{
		Host:        strings.TrimPrefix(server.URL, "http://"),
		Credentials: Credentials{Uin: "100012345678"},
	})
	if err != nil {
		t.Fatalf("创建 client 失败：%s", err.Code)
	}

	sendErr := client.Send(context.Background(), "topic-id", &LogGroup{})
	if sendErr == nil {
		t.Fatal("期望返回 401 错误，实际成功")
	}
	if sendErr.HTTPCode != http.StatusUnauthorized {
		t.Errorf("期望 HTTPCode 401，实际 %d", sendErr.HTTPCode)
	}
	if sendErr.Code != "Unauthorized" {
		t.Errorf("期望错误码 Unauthorized，实际 %s", sendErr.Code)
	}
	if sendErr.RequestID != "req-123" {
		t.Errorf("期望 RequestID req-123，实际 %s", sendErr.RequestID)
	}
}

// TestResetSecretTokenWeakAuth 弱鉴权模式下 ResetSecretToken 应被忽略且不改动凭证
func TestResetSecretTokenWeakAuth(t *testing.T) {
	client, err := NewCLSClient(&Options{
		Host:        "ap-guangzhou.cls.tencentcs.com",
		Credentials: Credentials{Uin: "100012345678"},
	})
	if err != nil {
		t.Fatalf("创建 client 失败：%s", err.Code)
	}

	if resetErr := client.ResetSecretToken("id", "key", "token"); resetErr != nil {
		t.Fatalf("期望返回 nil，实际 %s", resetErr.Code)
	}
	if !client.options.isWeakAuth() {
		t.Error("弱鉴权模式不应被 ResetSecretToken 切换为强鉴权")
	}
	if client.options.Credentials.SecretID != "" || client.options.Credentials.SecretKEY != "" ||
		client.options.Credentials.SecretToken != "" {
		t.Error("弱鉴权模式下凭证不应被修改")
	}
	if client.options.Credentials.Uin != "100012345678" {
		t.Errorf("Uin 不应被修改，实际 %s", client.options.Credentials.Uin)
	}
}

// TestResetSecretTokenStrongAuth 强鉴权模式下 ResetSecretToken 行为保持不变
func TestResetSecretTokenStrongAuth(t *testing.T) {
	client, err := NewCLSClient(&Options{
		Host:        "ap-guangzhou.cls.tencentcs.com",
		Credentials: Credentials{SecretID: "old-id", SecretKEY: "old-key"},
	})
	if err != nil {
		t.Fatalf("创建 client 失败：%s", err.Code)
	}

	if resetErr := client.ResetSecretToken("", "key", "token"); resetErr == nil {
		t.Error("secretID 为空应返回错误")
	}
	if resetErr := client.ResetSecretToken("id", "", "token"); resetErr == nil {
		t.Error("secretKEY 为空应返回错误")
	}
	if resetErr := client.ResetSecretToken("id", "key", ""); resetErr == nil {
		t.Error("secretToken 为空应返回错误")
	}
	if resetErr := client.ResetSecretToken("new-id", "new-key", "new-token"); resetErr != nil {
		t.Fatalf("期望重置成功，实际 %s", resetErr.Code)
	}
	if client.options.Credentials.SecretID != "new-id" {
		t.Errorf("期望 SecretID 被更新为 new-id，实际 %s", client.options.Credentials.SecretID)
	}
}

// TestProducerConfigPassesUin 校验两个 producer config 的 Uin 能透传到 client
func TestProducerConfigPassesUin(t *testing.T) {
	asyncConfig := GetDefaultAsyncProducerClientConfig()
	asyncConfig.Endpoint = "ap-guangzhou.cls.tencentcs.com"
	asyncConfig.Uin = "100012345678"
	asyncClient, err := NewAsyncProducerClient(asyncConfig)
	if err != nil {
		t.Fatalf("创建 async producer 失败：%v", err)
	}
	if asyncClient.Client.options.Credentials.Uin != "100012345678" {
		t.Errorf("async producer 未透传 Uin，实际 %q", asyncClient.Client.options.Credentials.Uin)
	}
	if !asyncClient.Client.options.isWeakAuth() {
		t.Error("async producer 仅填 Uin 时应为弱鉴权")
	}

	syncConfig := GetDefaultSyncProducerClientConfig()
	syncConfig.Endpoint = "ap-guangzhou.cls.tencentcs.com"
	syncConfig.Uin = "100012345678"
	syncClient, err := NewSyncProducerClient(syncConfig)
	if err != nil {
		t.Fatalf("创建 sync producer 失败：%v", err)
	}
	if syncClient.client.options.Credentials.Uin != "100012345678" {
		t.Errorf("sync producer 未透传 Uin，实际 %q", syncClient.client.options.Credentials.Uin)
	}
	if !syncClient.client.options.isWeakAuth() {
		t.Error("sync producer 仅填 Uin 时应为弱鉴权")
	}
}
