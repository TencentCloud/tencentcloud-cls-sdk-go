package tencentcloud_cls_sdk_go

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go.uber.org/zap"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Responsible for unified request encapsulation for cloud APIs

type YunApiLogClient struct {
	*CLSClient
	region    string
	secretId  string
	secretKey string
	internal  bool
	source    string
}

// YunApiLogClientConfig configuration struct, supports optional parameters
type YunApiLogClientConfig struct {
	AccessKeyId   string
	AccessKey     string
	Internal      bool
	SecurityToken string
	Source        string
	Region        string
}

// NewYunApiLogClientSimple simple constructor, only requires essential parameters
func NewYunApiLogClientSimple(accessKeyId, accessKey string) *YunApiLogClient {
	return NewYunApiLogClientWithConfig(YunApiLogClientConfig{
		AccessKeyId:   accessKeyId,
		AccessKey:     accessKey,
		Internal:      false, // default value
		SecurityToken: "",    // default value None
		Source:        "",    // default value None
		Region:        "",    // default value ''
	})
}

// NewYunApiLogClientWithConfig constructor using configuration struct, supports optional parameters
func NewYunApiLogClientWithConfig(config YunApiLogClientConfig) *YunApiLogClient {
	// Set default values
	if config.Internal == false && config.SecurityToken == "" && config.Source == "" && config.Region == "" {
		// If all optional parameters are zero values
		config.Internal = false   // default value
		config.SecurityToken = "" // default value None
		config.Source = ""        // default value None
		config.Region = ""        // default value ''
	}

	return NewYunApiLogClient(
		config.AccessKeyId,
		config.AccessKey,
		config.Internal,
		config.SecurityToken,
		config.Source,
		config.Region,
	)
}

// NewYunApiLogClient constructor
func NewYunApiLogClient(accessKeyId, accessKey string, internal bool, securityToken, source, region string) *YunApiLogClient {
	// Endpoint resolution:
	//   1. internal=true  => always use the VPC internal endpoint, regardless of region.
	//      The region is still kept on the client and sent via the X-TC-Region header
	//      (required public parameter on the server side).
	//   2. region != ""   => use the region-specific public endpoint.
	//   3. otherwise      => use the default public endpoint.
	var endpoint string
	if internal {
		endpoint = "cls.internal.tencentcloudapi.com"
	} else if region != "" {
		endpoint = fmt.Sprintf("cls.%s.tencentcloudapi.com", region)
	} else {
		endpoint = "cls.tencentcloudapi.com"
	}

	clsClient, _ := NewCLSClient(&Options{
		Host:         endpoint,
		Timeout:      30000,
		IdleConn:     50,
		CompressType: "lz4",
		Credentials: Credentials{
			SecretID:    accessKeyId,
			SecretKEY:   accessKey,
			SecretToken: securityToken,
		},
	})

	return &YunApiLogClient{
		CLSClient: clsClient,
		region:    region,
		secretId:  accessKeyId,
		secretKey: accessKey,
		internal:  internal,
		source:    source,
	}
}

// DoRequest
func (c *YunApiLogClient) DoRequest(
	method string,
	resource string,
	params map[string]string,
	headers map[string]string,
	body []byte,
	region string,
	action string,
	responseBodyType string,
	service string,
) ([]byte, error) {
	if service == "" {
		service = "cls"
	}
	if responseBodyType == "" {
		responseBodyType = "json"
	}
	if region == "" {
		region = c.region
	}
	// Create zap logger
	zapLogger, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	defer zapLogger.Sync()
	logger := NewZapLogger(zapLogger)

	// Construct URL
	host := c.CLSClient.options.Host
	// Tencent Cloud CLS
	urlStr := "https://" + host + resource
	if len(params) > 0 {
		q := url.Values{}
		for k, v := range params {
			q.Set(k, v)
		}
		urlStr = urlStr + "?" + q.Encode()
	}
	// Retry mechanism
	maxRetries := 10
	var lastErr error

	// Construct headers
	headers2 := make(map[string]string)
	for k, v := range headers {
		headers2[k] = v
	}

	timestamp := time.Now().Unix()
	headers2["X-TC-Timestamp"] = fmt.Sprintf("%d", timestamp)
	headers2["X-TC-Action"] = action
	headers2["X-TC-Region"] = region
	headers2["X-TC-Language"] = "zh-CN"
	headers2["Host"] = host

	// Add securityToken support
	if c.CLSClient.options.Credentials.SecretToken != "" {
		headers2["X-Cls-Token"] = c.CLSClient.options.Credentials.SecretToken
	}

	if _, ok := headers2["Content-Type"]; !ok {
		headers2["Content-Type"] = "application/json"
	}

	// Calculate signature using v3 version
	auth, err := SignatureWithTC3(
		c.secretId, c.secretKey, service, method, resource, "", headers2, body, timestamp,
	)
	if err != nil {
		return nil, err
	}
	headers2["Authorization"] = auth
	for i := 0; i < maxRetries; i++ {
		// Construct HTTP request
		req, err := http.NewRequest(method, urlStr, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		for k, v := range headers2 {
			req.Header.Set(k, v)
		}

		// Send request
		resp, err := c.CLSClient.client.Do(req)
		if err != nil {
			lastErr = err
			// Retry on network error
			if i < maxRetries-1 {
				time.Sleep(1 * time.Second)
				continue
			}
			return nil, err
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = err
			if i < maxRetries-1 {
				time.Sleep(1 * time.Second)
				continue
			}
			return nil, err
		}

		var apiErr struct {
			Response struct {
				Error *APIError `json:"Error"`
			} `json:"Response"`
		}

		// Check response status code
		if resp.StatusCode >= 500 {
			// Server error, need to retry
			lastErr = fmt.Errorf("server error: %d", resp.StatusCode)
			logger.Error("Server error occurred",
				Field{Key: "status_code", Value: resp.StatusCode},
				Field{Key: "error_message", Value: lastErr.Error()},
				Field{Key: "retry_count", Value: i + 1},
			)
			if i < maxRetries-1 {
				time.Sleep(1 * time.Second)
				continue
			}
		} else if resp.StatusCode >= 400 {
			// Client error, further determine business error code
			_ = json.Unmarshal(respBody, &apiErr)
			if apiErr.Response.Error != nil {
				code := apiErr.Response.Error.Code
				if code == "InternalError" || code == "Timeout" || code == "SpeedQuotaExceed" {
					lastErr = fmt.Errorf("api error: %s - %s", code, apiErr.Response.Error.Message)
					if i < maxRetries-1 {
						time.Sleep(1 * time.Second)
						continue
					}
				}
			}
			// Other 4xx errors return directly
			return respBody, nil
		}
		// Successful response
		return respBody, nil
	}
	return nil, lastErr
}

type APIError struct {
	Code    string `json:"Code"`
	Message string `json:"Message"`
}

// CreateConsumerGroupRequest create consumer group request
type CreateConsumerGroupRequest struct {
	ConsumerGroup string   `json:"ConsumerGroup"`
	Timeout       int      `json:"Timeout"`
	Topics        []string `json:"Topics"`
	LogsetId      string   `json:"LogsetId"`
}

type CreateConsumerGroupResponse struct {
	Response struct {
		RequestId     string    `json:"RequestId"`
		Error         *APIError `json:"Error,omitempty"`
		ConsumerGroup string    `json:"ConsumerGroup,omitempty"`
	} `json:"Response"`
}

// ListConsumerGroupRequest list consumer group request
type ListConsumerGroupRequest struct {
	LogsetId string   `json:"LogsetId"`
	Topics   []string `json:"Topics"`
}

type ConsumerGroupEntity struct {
	ConsumerGroup string   `json:"ConsumerGroup"`
	Timeout       int      `json:"Timeout"`
	Topics        []string `json:"Topics"`
}

type ListConsumerGroupResponse struct {
	Response struct {
		ConsumerGroupsInfo []ConsumerGroupEntity `json:"ConsumerGroupsInfo"`
		RequestId          string                `json:"RequestId"`
		Error              *APIError             `json:"Error,omitempty"`
	} `json:"Response"`
}

// UpdateConsumerGroupRequest update consumer group request
type UpdateConsumerGroupRequest struct {
	LogsetId      string   `json:"LogsetId"`
	ConsumerGroup string   `json:"ConsumerGroup"`
	Topics        []string `json:"Topics,omitempty"`
	Timeout       *int     `json:"Timeout,omitempty"`
}

// UpdateConsumerGroupResponse supports capturing all fields
type UpdateConsumerGroupResponse struct {
	Response struct {
		RequestId string    `json:"RequestId"`
		Error     *APIError `json:"Error,omitempty"`
	} `json:"Response"`
}

// DeleteConsumerGroupRequest delete consumer group request
type DeleteConsumerGroupRequest struct {
	LogsetId      string `json:"LogsetId"`
	ConsumerGroup string `json:"ConsumerGroup"`
}

// DeleteConsumerGroupResponse supports capturing all fields
type DeleteConsumerGroupResponse struct {
	Response struct {
		RequestId string    `json:"RequestId"`
		Error     *APIError `json:"Error,omitempty"`
	} `json:"Response"`
}

// CreateConsumerGroup create consumer group
func (c *YunApiLogClient) CreateConsumerGroup(logsetId, consumerGroup string, timeout int, topics []string) (*CreateConsumerGroupResponse, error) {
	request := CreateConsumerGroupRequest{
		ConsumerGroup: consumerGroup,
		Timeout:       timeout,
		Topics:        topics,
		LogsetId:      logsetId,
	}
	zapLogger, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	defer zapLogger.Sync()
	logger := NewZapLogger(zapLogger)
	bodyBytes, err := json.Marshal(request)
	if err != nil {
		logger.Error("JSON serialization failed",
			Field{Key: "error", Value: err.Error()},
		)
	}
	headers := map[string]string{
		"Host":         c.CLSClient.options.Host,
		"Content-Type": "application/json",
		"X-TC-Version": "2020-10-16",
	}
	params := map[string]string{}
	resource := "/"
	region := c.region
	action := "CreateConsumerGroup"
	service := "cls"
	respBytes, err := c.DoRequest("POST", resource, params, headers, bodyBytes, region, action, "json", service)
	if err != nil {
		return nil, err
	}
	var resp CreateConsumerGroupResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListConsumerGroup list consumer groups
func (c *YunApiLogClient) ListConsumerGroup(logsetId string, topics []string) (*ListConsumerGroupResponse, error) {
	request := ListConsumerGroupRequest{
		LogsetId: logsetId,
		Topics:   topics,
	}
	bodyBytes, err := json.Marshal(request)
	zapLogger, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	defer zapLogger.Sync()
	logger := NewZapLogger(zapLogger)
	if err != nil {
		logger.Error("JSON serialization failed",
			Field{Key: "error", Value: err.Error()},
		)
	}
	headers := map[string]string{
		"Host":         c.CLSClient.options.Host,
		"Content-Type": "application/json",
		"X-TC-Version": "2020-10-16",
	}
	params := map[string]string{}
	resource := "/"
	region := c.region
	action := "DescribeConsumerGroups"
	service := "cls"
	respBytes, err := c.DoRequest("POST", resource, params, headers, bodyBytes, region, action, "json", service)
	if err != nil {
		return nil, err
	}
	var resp ListConsumerGroupResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// UpdateConsumerGroup update consumer group
func (c *YunApiLogClient) UpdateConsumerGroup(logsetId, consumerGroup string, topics []string, timeout *int) (*UpdateConsumerGroupResponse, error) {
	request := UpdateConsumerGroupRequest{
		LogsetId:      logsetId,
		ConsumerGroup: consumerGroup,
		Topics:        topics,
		Timeout:       timeout,
	}
	bodyBytes, err := json.Marshal(request)
	zapLogger, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	defer zapLogger.Sync()
	logger := NewZapLogger(zapLogger)
	if err != nil {
		logger.Error("JSON serialization failed",
			Field{Key: "error", Value: err.Error()},
		)
	}
	headers := map[string]string{
		"Host":         c.CLSClient.options.Host,
		"Content-Type": "application/json",
		"X-TC-Version": "2020-10-16",
	}
	params := map[string]string{}
	resource := "/"
	region := c.region
	action := "ModifyConsumerGroup"
	service := "cls"
	respBytes, err := c.DoRequest("POST", resource, params, headers, bodyBytes, region, action, "json", service)
	if err != nil {
		return nil, err
	}
	var resp UpdateConsumerGroupResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DeleteConsumerGroup delete consumer group
func (c *YunApiLogClient) DeleteConsumerGroup(logsetId, consumerGroup string) (*DeleteConsumerGroupResponse, error) {
	request := DeleteConsumerGroupRequest{
		LogsetId:      logsetId,
		ConsumerGroup: consumerGroup,
	}
	bodyBytes, err := json.Marshal(request)
	zapLogger, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	defer zapLogger.Sync()
	logger := NewZapLogger(zapLogger)
	if err != nil {
		logger.Error("JSON serialization failed",
			Field{Key: "error", Value: err.Error()},
		)
	}
	headers := map[string]string{
		"Host":         c.CLSClient.options.Host,
		"Content-Type": "application/json",
		"X-TC-Version": "2020-10-16",
	}
	params := map[string]string{}
	resource := "/"
	region := c.region
	action := "DeleteConsumerGroup"
	service := "cls"
	respBytes, err := c.DoRequest("POST", resource, params, headers, bodyBytes, region, action, "json", service)
	if err != nil {
		return nil, err
	}
	var resp DeleteConsumerGroupResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetOffsetsRequest get offset request body
type GetOffsetsRequest struct {
	ConsumerGroup string `json:"ConsumerGroup"`
	TopicId       string `json:"TopicId"`
	PartitionId   string `json:"PartitionId,omitempty"`
	LogsetId      string `json:"LogsetId"`
	From          string `json:"From,omitempty"`
}

// ConsumerGroupGetOffsetsResponse get offset response
type ConsumerGroupGetOffsetsResponse struct {
	Response struct {
		ConsumerGroup             string                      `json:"ConsumerGroup"`
		TopicPartitionOffsetsInfo []TopicPartitionOffsetsInfo `json:"TopicPartitionOffsetsInfo"`
		RequestId                 string                      `json:"RequestId"`
		Error                     *APIError                   `json:"Error,omitempty"`
	} `json:"Response"`
}

// TopicPartitionOffsetsInfo topic partition offset information
type TopicPartitionOffsetsInfo struct {
	TopicID          string            `json:"TopicID"`
	PartitionOffsets []PartitionOffset `json:"PartitionOffsets"`
}

// PartitionOffset partition offset
type PartitionOffset struct {
	PartitionID int   `json:"PartitionId"`
	Offset      int64 `json:"Offset"`
}

// GetOffsets get offsets
func (c *YunApiLogClient) GetOffsets(logsetId, consumerGroup, topicId string, partitionId int, position string) (*ConsumerGroupGetOffsetsResponse, error) {
	req := GetOffsetsRequest{
		ConsumerGroup: consumerGroup,
		TopicId:       topicId,
		PartitionId:   fmt.Sprintf("%d", partitionId),
		LogsetId:      logsetId,
		From:          position,
	}
	bodyBytes, err := json.Marshal(req)
	zapLogger, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	defer zapLogger.Sync()
	logger := NewZapLogger(zapLogger)
	if err != nil {
		logger.Error("JSON serialization failed",
			Field{Key: "error", Value: err.Error()},
		)
	}
	headers := map[string]string{
		"Host":         c.CLSClient.options.Host,
		"Content-Type": "application/json",
		"X-TC-Version": "2020-10-16",
	}
	params := map[string]string{}
	resource := "/"
	region := c.region
	action := "DescribeConsumerOffsets"
	service := "cls"
	respBytes, err := c.DoRequest("POST", resource, params, headers, bodyBytes, region, action, "json", service)
	if err != nil {
		return nil, err
	}
	var resp ConsumerGroupGetOffsetsResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// UpdateOffsetsRequest update offset request body
type UpdateOffsetsRequest struct {
	TopicPartitionOffsetsInfo []TopicPartitionOffsetsInfo `json:"TopicPartitionOffsetsInfo"`
	ConsumerGroup             string                      `json:"ConsumerGroup"`
	Consumer                  string                      `json:"Consumer"`
	LogsetId                  string                      `json:"LogsetId"`
}

// ConsumerGroupUpdateOffsetsResponse update offset response
type ConsumerGroupUpdateOffsetsResponse struct {
	Response struct {
		RequestId string    `json:"RequestId"`
		Error     *APIError `json:"Error,omitempty"`
	} `json:"Response"`
}

// UpdateOffsets update offsets
func (c *YunApiLogClient) UpdateOffsets(logsetId, consumerGroup, consumer string, offsets []TopicPartitionOffsetsInfo) (*ConsumerGroupUpdateOffsetsResponse, error) {
	// Construct request body
	req := UpdateOffsetsRequest{
		TopicPartitionOffsetsInfo: offsets,
		ConsumerGroup:             consumerGroup,
		Consumer:                  consumer,
		LogsetId:                  logsetId,
	}

	bodyBytes, err := json.Marshal(req)
	zapLogger, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	defer zapLogger.Sync()
	logger := NewZapLogger(zapLogger)
	if err != nil {
		logger.Error("JSON serialization failed",
			Field{Key: "error", Value: err.Error()},
		)
	}

	headers := map[string]string{
		"Host":         c.CLSClient.options.Host,
		"Content-Type": "application/json",
		"X-TC-Version": "2020-10-16",
	}
	params := map[string]string{}
	resource := "/"
	region := c.region
	action := "CommitConsumerOffsets"
	service := "cls"
	respBytes, err := c.DoRequest("POST", resource, params, headers, bodyBytes, region, action, "json", service)
	if err != nil {
		return nil, err
	}
	var resp ConsumerGroupUpdateOffsetsResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// HeartbeatRequest heartbeat request body
type HeartbeatRequest struct {
	ConsumerGroup       string          `json:"ConsumerGroup"`
	Consumer            string          `json:"Consumer"`
	LogsetId            string          `json:"LogsetId"`
	TopicPartitionsInfo []PartitionInfo `json:"TopicPartitionsInfo"`
	PartitionStrategy   int             `json:"PartitionStrategy"`
}

// Heartbeat heartbeat
func (c *YunApiLogClient) Heartbeat(logsetId, consumerGroup, consumer string, partitions []PartitionInfo) (*ConsumerGroupHeartBeatResponse, error) {
	// Construct request body
	req := HeartbeatRequest{
		ConsumerGroup:       consumerGroup,
		Consumer:            consumer,
		LogsetId:            logsetId,
		TopicPartitionsInfo: partitions,
		PartitionStrategy:   2,
	}

	bodyBytes, err := json.Marshal(req)
	zapLogger, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	defer zapLogger.Sync()
	logger := NewZapLogger(zapLogger)
	if err != nil {
		logger.Error("JSON serialization failed",
			Field{Key: "error", Value: err.Error()},
		)
	}
	headers := map[string]string{
		"Host":         c.CLSClient.options.Host,
		"Content-Type": "application/json",
		"X-TC-Version": "2020-10-16",
	}
	params := map[string]string{}
	resource := "/"
	region := c.region
	action := "SendConsumerHeartbeat"
	service := "cls"
	respBytes, err := c.DoRequest("POST", resource, params, headers, bodyBytes, region, action, "json", service)
	if err != nil {
		return nil, err
	}
	var resp ConsumerGroupHeartBeatResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ConsumerGroupHeartBeatResponse heartbeat response
type ConsumerGroupHeartBeatResponse struct {
	Response struct {
		ConsumerGroup       string          `json:"ConsumerGroup,omitempty"`
		TopicPartitionsInfo []PartitionInfo `json:"TopicPartitionsInfo,omitempty"`
		RequestId           string          `json:"RequestId"`
		Error               *APIError       `json:"Error,omitempty"`
	} `json:"Response"`
}

type PartitionInfo struct {
	TopicID    string `json:"TopicID"`
	Partitions []int  `json:"Partitions"`
}
