package tencentcloud_cls_sdk_go

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/pierrec/lz4"
)

const (
	timeoutDefault  = 10000 // 默认上报请求超时时间
	idleConnDefault = 50    // 默认空闲连接数
	logUri          = "/structuredlog"
)

type Options struct {
	Host      string
	SecretID  string
	SecretKEY string
	Timeout   int
	IdleConn  int
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

	if options.SecretID == "" || options.SecretKEY == "" {
		return NewError(-1, "", MISS_ACCESS_KEY_ID, errors.New("SecretID or SecretKEY cannot be empty"))
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
			IdleConnTimeout:     time.Duration(300) * time.Second,
		},
		Timeout: time.Duration(options.Timeout) * time.Millisecond,
	}
	return client, nil
}

// send cls实际发送接口
func (client *CLSClient) Send(topicId string, group *LogGroup) *CLSError {
	params := url.Values{"topic_id": []string{topicId}}
	headers := url.Values{"Host": {client.options.Host}, "Content-Type": {"application/x-protobuf"}}
	authorization := signature(client.options.SecretID, client.options.SecretKEY, http.MethodPost,
		logUri, params, headers, 300)

	urlReport := fmt.Sprintf("http://%s/structuredlog", client.options.Host)

	var logGroupList LogGroupList
	logGroupList.LogGroupList = append(logGroupList.LogGroupList, group)

	body, _ := logGroupList.Marshal()

	out := make([]byte, lz4.CompressBlockBound(len(body)))
	var hashTable [1 << 16]int
	n, err := lz4.CompressBlock(body, out, hashTable[:])
	if err != nil {
		return NewError(-1, "", BAD_REQUEST, err)
	}
	// copy incompressible data as lz4 format
	if n == 0 {
		n, _ = copyIncompressible(body, out)
	}
	req, err := http.NewRequest(http.MethodPost, urlReport, bytes.NewBuffer(out[:n]))

	if err != nil {
		return NewError(-1, "", BAD_REQUEST, err)
	}
	req.URL.RawQuery = params.Encode()
	req.Header.Add("Host", client.options.Host)
	req.Header.Add("Content-Type", "application/x-protobuf")
	req.Header.Add("Authorization", authorization)
	req.Header.Add("x-cls-compress-type", "lz4")

	resp, err := client.client.Do(req)
	if err != nil {
		return NewError(-1, "", BAD_REQUEST, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(ioutil.Discard, resp.Body)
	// 401, 403, 404 直接返回错误
	if resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 404 {
		return NewError(int32(resp.StatusCode), resp.Header.Get("X-Cls-Requestid"), BAD_REQUEST, errors.New("bad request"))
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
