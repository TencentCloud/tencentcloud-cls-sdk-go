package tencentcloud_cls_sdk_go

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pierrec/lz4"
)

const (
	timeoutDefault  = 10000 // 默认上报请求超时时间
	idleConnDefault = 50    // 默认空闲连接数
	logUri          = "/structuredlog"
)

type Options struct {
	Host         string
	Scheme       string
	Timeout      int
	IdleConn     int
	CompressType string
	Credentials  Credentials
	// Resolver 可选。若不为 nil，则每次发送请求前会调用其 Resolve 方法
	// 动态解析实际使用的 Host（如通过北极星服务发现）。
	// 请求完成后会通过返回的 Reporter 回报调用结果，供负载均衡/熔断使用。
	Resolver EndpointResolver
}

type Credentials struct {
	SecretID    string
	SecretKEY   string
	SecretToken string
}

// EndpointResolver 用于每次发送前动态解析出真实的目标地址。
// 实现方（例如北极星 PolarisResolver）在 Resolve 中执行服务发现，
// 返回本次请求应使用的 Host 以及一个 Reporter 用于请求结束后上报调用结果。
type EndpointResolver interface {
	// Resolve 返回本次请求要使用的 Endpoint。
	// 返回的 Reporter 可能为 nil，此时表示不需要上报。
	Resolve(ctx context.Context) (endpoint *ResolvedEndpoint, reporter Reporter, err error)
}

// ResolvedEndpoint 表示一次解析出的目标地址。
type ResolvedEndpoint struct {
	// Host 形如 "host:port" 或 "domain"；不需要带 scheme 前缀。
	Host string
	// Scheme 可选：http 或 https。为空时沿用 Options.Scheme。
	Scheme string
}

// Reporter 用于将一次请求的调用结果上报回 Resolver（如北极星）。
type Reporter interface {
	// Report 上报调用结果。err 为 nil 表示成功，非 nil 表示失败。
	// statusCode 为 HTTP 状态码；若发生网络错误无 HTTP 响应，可传 0。
	Report(err error, statusCode int, cost time.Duration)
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
	if options.Host == "" && options.Resolver == nil {
		return NewError(-1, "", MISSING_HOST, errors.New("host cannot be empty"))
	}

	//if options.Credentials.SecretID == "" || options.Credentials.SecretKEY == "" {
	//	return NewError(-1, "", MISS_ACCESS_KEY_ID, errors.New("SecretID or SecretKEY cannot be empty"))
	//}

	if options.CompressType == "" {
		options.CompressType = "lz4"
	}

	return nil
}

func (client *CLSClient) ResetSecretToken(secretID string, secretKEY string, secretToken string) *CLSError {
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
	// 确保Host包含正确的协议头
	if strings.HasPrefix(options.Host, "http://") {
		options.Scheme = "http"
		options.Host = strings.TrimPrefix(options.Host, "http://")
	} else if strings.HasPrefix(options.Host, "https://") {
		options.Scheme = "https"
		options.Host = strings.TrimPrefix(options.Host, "https://")
	} else {
		options.Scheme = "http"
	}
	client.options = options
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   time.Duration(options.Timeout) * time.Millisecond,
			KeepAlive: 300 * time.Second,
		}).DialContext,
		MaxIdleConns:        options.IdleConn,
		MaxIdleConnsPerHost: options.IdleConn,
		MaxConnsPerHost:     options.IdleConn,
		IdleConnTimeout:     time.Duration(300) * time.Second,
	}
	if options.Scheme == "https" {
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	}
	client.client = &http.Client{
		Transport: transport,
		Timeout:   time.Duration(options.Timeout) * time.Millisecond,
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

func (client *CLSClient) deflateCompress(body []byte, params url.Values, urlReport string) (*http.Request, *CLSError) {
	data, err := DeflateCompress(body)
	if err != nil {
		return nil, NewError(-1, "", BAD_REQUEST, err)
	}

	req, err := http.NewRequest(http.MethodPost, urlReport, bytes.NewBuffer(data))
	if err != nil {
		return nil, NewError(-1, "", BAD_REQUEST, err)
	}
	req.URL.RawQuery = params.Encode()
	req.Header.Add("x-cls-compress-type", "deflate")
	return req, nil
}

// Send cls实际发送接口
func (client *CLSClient) Send(ctx context.Context, topicId string, group ...*LogGroup) *CLSError {
	// 动态解析 Host（如通过北极星服务发现）
	host := client.options.Host
	scheme := client.options.Scheme
	var reporter Reporter
	if client.options.Resolver != nil {
		endpoint, rp, err := client.options.Resolver.Resolve(ctx)
		if err != nil {
			return NewError(-1, "", BAD_REQUEST, err)
		}
		if endpoint != nil && endpoint.Host != "" {
			host = endpoint.Host
		}
		if endpoint != nil && endpoint.Scheme != "" {
			scheme = endpoint.Scheme
		}
		reporter = rp
	}

	params := url.Values{"topic_id": []string{topicId}}
	headers := url.Values{"Host": {host}, "Content-Type": {"application/x-protobuf"}}

	authorization := signature(client.options.Credentials.SecretID, client.options.Credentials.SecretKEY, http.MethodPost,
		logUri, params, headers, 300)

	urlReport := fmt.Sprintf("%s://%s/structuredlog", scheme, host)
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
	} else if client.options.CompressType == "deflate" {
		if req, clsErr = client.deflateCompress(body, params, urlReport); clsErr != nil {
			return clsErr
		}
	} else {
		if req, clsErr = client.lz4Compress(body, params, urlReport); clsErr != nil {
			return clsErr
		}
	}

	req.Header.Add("Host", host)
	req.Header.Add("Content-Type", "application/x-protobuf")
	if client.options.Credentials.SecretID != "" && client.options.Credentials.SecretKEY != "" {
		req.Header.Add("Authorization", authorization)
	}
	req.Header.Add("User-Agent", getUserAgent())
	if client.options.Credentials.SecretToken != "" {
		req.Header.Add("X-Cls-Token", client.options.Credentials.SecretToken)
	}
	req = req.WithContext(ctx)

	start := time.Now()
	resp, err := client.client.Do(req)
	cost := time.Since(start)
	if err != nil {
		if reporter != nil {
			reporter.Report(err, 0, cost)
		}
		return NewError(-1, "--No RequestId--", BAD_REQUEST, err)
	}
	defer resp.Body.Close()
	// 400, 401, 403, 404, 413 直接返回错误
	if resp.StatusCode == 400 || resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 404 || resp.StatusCode == 413 {
		v, err := io.ReadAll(resp.Body)
		if err != nil {
			if reporter != nil {
				reporter.Report(errors.New("bad request"), resp.StatusCode, cost)
			}
			return NewError(int32(resp.StatusCode), resp.Header.Get("X-Cls-Requestid"), BAD_REQUEST, errors.New("bad request"))
		}
		var message ErrorMessage
		if err := json.Unmarshal(v, &message); err != nil {
			if reporter != nil {
				reporter.Report(errors.New("bad request"), resp.StatusCode, cost)
			}
			return NewError(int32(resp.StatusCode), resp.Header.Get("X-Cls-Requestid"), BAD_REQUEST, errors.New("bad request"))
		}
		if reporter != nil {
			// 4xx 通常是客户端问题（鉴权、参数），不作为服务端故障上报为失败
			reporter.Report(nil, resp.StatusCode, cost)
		}
		return NewError(int32(resp.StatusCode), resp.Header.Get("X-Cls-Requestid"), message.Code, errors.New(message.Message))
	}
	// 200 直接返回
	if resp.StatusCode == 200 {
		if reporter != nil {
			reporter.Report(nil, resp.StatusCode, cost)
		}
		return nil
	}

	// 如果被服务端写入限速
	if resp.StatusCode == 429 {
		if reporter != nil {
			reporter.Report(errors.New("write quota exceed"), resp.StatusCode, cost)
		}
		return NewError(int32(resp.StatusCode), resp.Header.Get("X-Cls-Requestid"), WRITE_QUOTA_EXCEED, errors.New("write quota exceed"))
	}
	// 如果是服务端错误
	if resp.StatusCode >= 500 {
		if reporter != nil {
			reporter.Report(errors.New("server internal error"), resp.StatusCode, cost)
		}
		return NewError(int32(resp.StatusCode), resp.Header.Get("X-Cls-Requestid"), INTERNAL_SERVER_ERROR, errors.New("server internal error"))
	}
	if reporter != nil {
		reporter.Report(errors.New("unknown error"), resp.StatusCode, cost)
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
