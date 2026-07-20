package tencentcloud_cls_sdk_go

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/golang/snappy"
	"io/ioutil"
	"net/http"
	"net/url"
)

// PullLogsClient implements log pulling functionality

type PullLogsClient struct {
	Endpoint  string
	SecretId  string
	SecretKey string
	Token     string
	UserAgent string
}

func NewPullLogsClient(endpoint, secretId, secretKey, token, userAgent string) *PullLogsClient {
	return &PullLogsClient{
		Endpoint:  endpoint,
		SecretId:  secretId,
		SecretKey: secretKey,
		Token:     token,
		UserAgent: userAgent,
	}
}

type PullLogsRequest struct {
	StartOffset  int64  `json:"StartOffset"`
	Size         int    `json:"Size"`
	CompressType string `json:"CompressType"`
	PartitionId  int    `json:"PartitionId"`
	StartTime    int64  `json:"StartTime"`
	EndTime      *int64 `json:"EndTime,omitempty"`
	// Query is an optional DSL pre-filter expression applied on the server side
	// before logs are returned. Example:
	//   log_keep(op_and(op_gt(v("status"), 400), str_exist(v("message"), "access failed")))
	// See https://cloud.tencent.com/document/product/614/37908 for the full DSL syntax.
	Query string `json:"Query"`
}

// PullLogs pulls logs and returns snappy compressed binary data.
// query is an optional DSL pre-filter expression; pass "" to disable server-side filtering.
func (c *PullLogsClient) PullLogs(topicId string, partitionId int, size int, startTime *int64, offset int64, endTime *int64, query string) ([]byte, error) {
	// 处理 StartTime，如果为 nil 则使用 0
	var startTimeValue int64
	if startTime != nil {
		startTimeValue = *startTime
	}

	pullReq := PullLogsRequest{
		StartOffset:  offset,
		Size:         size,
		CompressType: "snappy",
		PartitionId:  partitionId,
		StartTime:    startTimeValue,
		EndTime:      endTime,
		Query:        query,
	}
	bodyBytes, err := json.Marshal(pullReq)
	if err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("topic_id", topicId)
	urlStr := fmt.Sprintf("https://%s/pull_log?%s", c.Endpoint, params.Encode())

	// 构造 header
	headers := make(http.Header)
	headers.Set("Host", c.Endpoint)
	headers.Set("Content-Type", "application/json")
	if c.UserAgent != "" {
		headers.Set("User-Agent", c.UserAgent)
	} else {
		headers.Set("User-Agent", "tc-cls-sdk-go/1.0.0")
	}
	if c.Token != "" {
		headers.Set("X-Cls-Token", c.Token)
	}

	// 生成v1签名
	sigHeaders := url.Values{}
	for k, v := range headers {
		for _, vv := range v {
			sigHeaders.Add(k, vv)
		}
	}
	sigParams := url.Values{}
	for k, v := range params {
		for _, vv := range v {
			sigParams.Add(k, vv)
		}
	}
	auth := signature(c.SecretId, c.SecretKey, "POST", "/pull_log", sigParams, sigHeaders, 300)
	headers.Set("Authorization", auth)

	// 构造请求
	req, err := http.NewRequest("POST", urlStr, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header = headers

	// 发送请求
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return respBytes, fmt.Errorf("pull logs failed, HTTP %d: %s", resp.StatusCode, string(respBytes))
	}

	return respBytes, nil
}

// PullLogsAndParse pulls and parses logs and outputs proto text format.
// query is an optional DSL pre-filter expression; pass "" to disable server-side filtering.
func (c *PullLogsClient) PullLogsAndParse(topicId string, partitionId int, size int, startTime *int64, offset int64, endTime *int64, query string) (*PullLogResponse, error) {
	respBytes, err := c.PullLogs(topicId, partitionId, size, startTime, offset, endTime, query)
	if err != nil {
		return nil, err
	}
	resp, err := ParsePullLogResponse(respBytes)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

type PullLogResponse struct {
	NextOffset      int64
	LogGroups       []*LogGroup
	Headers         map[string]string
	logGroupsJson   []*LogGroup
	flattenLogsJson []*LogGroup
}

// ParsePullLogResponse parses the binary response of pulling logs and returns a PullLogResponse object
func ParsePullLogResponse(respBytes []byte) (*PullLogResponse, error) {
	// 1. snappy decompression
	decompressed, err := snappy.Decode(nil, respBytes)
	if err != nil {
		return nil, fmt.Errorf("snappy decompression failed: %v", err)
	}

	// 2. parse json
	var respObj struct {
		Response struct {
			NextOffset int64    `json:"NextOffset"`
			Message    []string `json:"Message"`
		} `json:"Response"`
	}
	if err = json.Unmarshal(decompressed, &respObj); err != nil {
		return nil, fmt.Errorf("json parse failed: %v", err)
	}

	logGroups := make([]*LogGroup, 0, len(respObj.Response.Message))
	for _, msg := range respObj.Response.Message {
		bMsg, err1 := base64.StdEncoding.DecodeString(msg)
		if err1 != nil {
			return nil, fmt.Errorf("base64 decode failed: %v", err1)
		}
		if len(bMsg) < 96 {
			return nil, fmt.Errorf("Message length is less than 96 bytes")
		}
		version := int32(bytesToInt32(bMsg[0:4]))
		if version != 130 {
			return nil, fmt.Errorf("unsupported Message version: %d", version)
		}
		contentLen := int(bytesToInt32(bMsg[92:96]))
		if len(bMsg) < 96+contentLen {
			return nil, fmt.Errorf("Message length does not match contentLen")
		}
		pbData := bMsg[96 : 96+contentLen]
		var logGroup LogGroup
		if err1 := logGroup.Unmarshal(pbData); err1 != nil {
			return nil, fmt.Errorf("protobuf parse failed: %v", err1)
		}
		logGroups = append(logGroups, &logGroup)
	}

	return &PullLogResponse{
		NextOffset: respObj.Response.NextOffset,
		LogGroups:  logGroups,
		Headers:    nil,
	}, nil
}

// bytesToInt32 big endian byte to int32
func bytesToInt32(b []byte) int32 {
	return int32(b[0])<<24 | int32(b[1])<<16 | int32(b[2])<<8 | int32(b[3])
}

// GetLogGroups returns all log groups
func (r *PullLogResponse) GetLogGroups() []*LogGroup {
	return r.LogGroups
}

// GetLogCount returns the number of all logs
func (r *PullLogResponse) GetLogCount() int {
	return len(r.LogGroups)
}

// GetNextOffset returns the next offset
func (r *PullLogResponse) GetNextOffset() int64 {
	return r.NextOffset
}
