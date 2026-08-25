package tencentcloud_cls_sdk_go

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/pierrec/lz4"
)

const (
	timeoutDefault  = 10000 // 默认上报请求超时时间
	idleConnDefault = 50    // 默认空闲连接数
	logUri          = "/structuredlog"
)

// 弱鉴权（免密）相关请求头，字面量与服务端 consts.ClsAuthMode / consts.ClsUIN 保持一致
const (
	headerAuthMode = "x-cls-auth-mode"
	headerUin      = "X-CLS-Uin"
	authModeWeak   = "weak"
)

type Options struct {
	Host         string
	Timeout      int
	IdleConn     int
	CompressType string
	Credentials  Credentials
}

type Credentials struct {
	SecretID    string
	SecretKEY   string
	SecretToken string
	// Uin 弱鉴权（免密）账号 ID，与 SecretID/SecretKEY 二选一填写。
	// 两者同时填写时以 SecretID/SecretKEY 为准（走强鉴权），Uin 被忽略。
	Uin string
}

// isWeakAuth 判断当前是否走弱鉴权（免密）模式。
// 仅当 SecretID/SecretKEY 不齐全时才为弱鉴权，即 AK/SK 优先。
func (options *Options) isWeakAuth() bool {
	return options.Credentials.SecretID == "" || options.Credentials.SecretKEY == ""
}

func (options *Options) withTimeoutDefault() {
	if options.Timeout <= 0 {
		options.Timeout = timeoutDefault
	}
}

func (options *Options) withIdleConnDefault() {
	if options.IdleConn <= 0 {
		options.IdleConn = idleConnDefault
	}
}

func (options *Options) validateOptions() *CLSError {
	if options.Host == "" {
		return NewError(-1, "", MISSING_HOST, errors.New("host cannot be empty"))
	}

	// AK/SK 齐全走强鉴权；否则要求填写合法的 Uin 走弱鉴权（免密）
	if options.isWeakAuth() {
		if options.Credentials.Uin == "" {
			return NewError(-1, "", MISS_ACCESS_KEY_ID,
				errors.New("SecretID or SecretKEY cannot be empty, or set Uin to use weak authorization"))
		}
		if _, err := strconv.ParseUint(options.Credentials.Uin, 10, 64); err != nil {
			return NewError(-1, "", INVALID_UIN, errors.New("uin must be a digits-only string"))
		}
	}

	if options.CompressType == "" {
		options.CompressType = "lz4"
	}

	return nil
}

func (client *CLSClient) ResetSecretToken(secretID string, secretKEY string, secretToken string) *CLSError {
	// 弱鉴权模式下不使用密钥，忽略本次调用以避免静默切换鉴权模式
	if client.options.isWeakAuth() {
		GetZapLoggerAdapter().Warn("ResetSecretToken ignored in weak auth mode",
			Field{Key: "uin", Value: client.options.Credentials.Uin})
		return nil
	}
	if secretID == "" {
		return NewError(-1, "", MISS_ACCESS_KEY_ID, errors.New("secretID cannot be empty"))
	}
	if secretKEY == "" {
		return NewError(-1, "", MISS_ACCESS_SECRET, errors.New("secretKEY cannot be empty"))
	}
	if secretToken == "" {
		return NewError(-1, "", MISS_ACCESS_TOKEN, errors.New("secretToken cannot be empty"))
	}
	client.options.Credentials = Credentials{
		SecretID:    secretID,
		SecretKEY:   secretKEY,
		SecretToken: secretToken,
	}
	return nil
}

type CLSClient struct {
	options *Options
	client  *http.Client
}

func NewCLSClient(options *Options) (*CLSClient, *CLSError) {
	client := new(CLSClient)
	if err := options.validateOptions(); err != nil {
		return nil, err
	}
	client.options = options
	client.client = &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   time.Duration(options.Timeout) * time.Millisecond,
				KeepAlive: 300 * time.Second,
			}).DialContext,
			MaxIdleConns:        options.IdleConn,
			MaxIdleConnsPerHost: options.IdleConn,
			MaxConnsPerHost:     options.IdleConn,
			IdleConnTimeout:     time.Duration(300) * time.Second,
		},
		Timeout: time.Duration(options.Timeout) * time.Millisecond,
	}
	return client, nil
}

type ErrorMessage struct {
	Code    string `json:"errorcode"`
	Message string `json:"errormessage"`
}

func (client *CLSClient) lz4Compress(body []byte, params url.Values, urlReport string) (*http.Request, *CLSError) {
	out := make([]byte, lz4.CompressBlockBound(len(body)))
	var hashTable [1 << 16]int
	n, err := lz4.CompressBlock(body, out, hashTable[:])
	if err != nil {
		return nil, NewError(-1, "", BAD_REQUEST, err)
	}
	// copy incompressible data as lz4 format
	if n == 0 {
		n, _ = copyIncompressible(body, out)
	}
	req, err := http.NewRequest(http.MethodPost, urlReport, bytes.NewBuffer(out[:n]))
	if err != nil {
		return nil, NewError(-1, "", BAD_REQUEST, err)
	}
	req.URL.RawQuery = params.Encode()
	req.Header.Add("x-cls-compress-type", "lz4")
	return req, nil
}

func (client *CLSClient) zstdCompress(body []byte, params url.Values, urlReport string) (*http.Request, *CLSError) {
	data, err := ZSTDCompress(ZstdEncoderParams{CompressionLevelDefault}, nil, body)
	if err != nil {
		return nil, NewError(-1, "", BAD_REQUEST, err)
	}

	req, err := http.NewRequest(http.MethodPost, urlReport, bytes.NewBuffer(data))
	if err != nil {
		return nil, NewError(-1, "", BAD_REQUEST, err)
	}
	req.URL.RawQuery = params.Encode()
	req.Header.Add("x-cls-compress-type", "zstd")
	return req, nil
}

// Send cls实际发送接口
func (client *CLSClient) Send(ctx context.Context, topicId string, group ...*LogGroup) *CLSError {
	params := url.Values{"topic_id": []string{topicId}}
	headers := url.Values{"Host": {client.options.Host}, "Content-Type": {"application/x-protobuf"}}

	urlReport := fmt.Sprintf("http://%s/structuredlog", client.options.Host)

	var logGroupList LogGroupList
	for _, item := range group {
		logGroupList.LogGroupList = append(logGroupList.LogGroupList, item)
	}
	body, _ := logGroupList.Marshal()

	var req *http.Request
	var clsErr *CLSError

	if client.options.CompressType == "zstd" {
		if req, clsErr = client.zstdCompress(body, params, urlReport); clsErr != nil {
			return clsErr
		}
	} else {
		if req, clsErr = client.lz4Compress(body, params, urlReport); clsErr != nil {
			return clsErr
		}
	}

	req.Header.Add("Host", client.options.Host)
	req.Header.Add("Content-Type", "application/x-protobuf")
	req.Header.Add("User-Agent", getUserAgent())

	if client.options.isWeakAuth() {
		// 弱鉴权（免密）：只带明文身份头，不计算签名、不带 Authorization/Token
		req.Header.Add(headerAuthMode, authModeWeak)
		req.Header.Add(headerUin, client.options.Credentials.Uin)
	} else {
		authorization := signature(client.options.Credentials.SecretID, client.options.Credentials.SecretKEY,
			http.MethodPost, logUri, params, headers, 300)
		req.Header.Add("Authorization", authorization)
		if client.options.Credentials.SecretToken != "" {
			req.Header.Add("X-Cls-Token", client.options.Credentials.SecretToken)
		}
	}

	req = req.WithContext(ctx)
	resp, err := client.client.Do(req)
	if err != nil {
		return NewError(-1, "--No RequestId--", BAD_REQUEST, err)
	}
	defer resp.Body.Close()

	// 401, 403, 404, 413 直接返回错误
	if resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 404 || resp.StatusCode == 413 {
		v, err := io.ReadAll(resp.Body)
		if err != nil {
			return NewError(int32(resp.StatusCode), resp.Header.Get("X-Cls-Requestid"), BAD_REQUEST, errors.New("bad request"))
		}
		var message ErrorMessage
		if err := json.Unmarshal(v, &message); err != nil {
			return NewError(int32(resp.StatusCode), resp.Header.Get("X-Cls-Requestid"), BAD_REQUEST, errors.New("bad request"))
		}
		return NewError(int32(resp.StatusCode), resp.Header.Get("X-Cls-Requestid"), message.Code, errors.New(message.Message))
	}
	// 200 直接返回
	if resp.StatusCode == 200 {
		return nil
	}

	// 如果被服务端写入限速
	if resp.StatusCode == 429 {
		return NewError(int32(resp.StatusCode), resp.Header.Get("X-Cls-Requestid"), WRITE_QUOTA_EXCEED, errors.New("write quota exceed"))
	}
	// 如果是服务端错误
	if resp.StatusCode >= 500 {
		return NewError(int32(resp.StatusCode), resp.Header.Get("X-Cls-Requestid"), INTERNAL_SERVER_ERROR, errors.New("server internal error"))
	}
	return NewError(int32(resp.StatusCode), resp.Header.Get("X-Cls-Requestid"), UNKNOWN_ERROR, errors.New("unknown error"))
}

func copyIncompressible(src, dst []byte) (int, error) {
	lLen, dn := len(src), len(dst)
	di := 0
	if lLen < 0xF {
		dst[di] = byte(lLen << 4)
	} else {
		dst[di] = 0xF0
		if di++; di == dn {
			return di, nil
		}
		lLen -= 0xF
		for ; lLen >= 0xFF; lLen -= 0xFF {
			dst[di] = 0xFF
			if di++; di == dn {
				return di, nil
			}
		}
		dst[di] = byte(lLen)
	}
	if di++; di+len(src) > dn {
		return di, nil
	}
	di += copy(dst[di:], src)
	return di, nil
}
