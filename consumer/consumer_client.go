package consumer

import (
	"fmt"
	"strings"

	cls "github.com/tencentcloud/tencentcloud-cls-sdk-go"
	"go.uber.org/zap"
)

// ConsumerClient
// Responsible for consumer group management, heartbeat, offset management, and log pulling

type ConsumerClient struct {
	YunApiClient   *cls.YunApiLogClient
	PullLogsClient *cls.PullLogsClient
	LogsetID       string
	TopicIDs       []string
	ConsumerGroup  string
	Consumer       string
	Region         string
}

// NewConsumerClient constructor
func NewConsumerClient(yunapi *cls.YunApiLogClient, pulllogsclient *cls.PullLogsClient, logsetID string, topicIDs []string, consumerGroup, consumer, region string) *ConsumerClient {
	return &ConsumerClient{
		YunApiClient:   yunapi,
		PullLogsClient: pulllogsclient,
		LogsetID:       logsetID,
		TopicIDs:       topicIDs,
		ConsumerGroup:  consumerGroup,
		Consumer:       consumer,
		Region:         region,
	}
}

// CreateConsumerGroup create consumer group
func (c *ConsumerClient) CreateConsumerGroup(timeout int) (*cls.CreateConsumerGroupResponse, error) {
	res, err := c.YunApiClient.CreateConsumerGroup(c.LogsetID, c.ConsumerGroup, timeout, c.TopicIDs)
	if err != nil {
		return nil, fmt.Errorf("error occur when create consumer group: %v", err)
	}

	// check error field in response
	if res.Response.Error != nil {
		errMsg := res.Response.Error.Message
		// check if it is an "already exists" error
		if strings.Contains(errMsg, "consumer group already exists") {
			// if it already exists, update automatically
			updateres, updateErr := c.YunApiClient.UpdateConsumerGroup(c.LogsetID, c.ConsumerGroup, c.TopicIDs, &timeout)
			if updateErr != nil {
				return nil, fmt.Errorf("error occur when update consumer group: %v", updateErr)
			}
			if updateres.Response.Error != nil {
				return nil, fmt.Errorf("error occur when update consumer group: %s - %s", updateres.Response.Error.Code, updateres.Response.Error.Message)
			}
			// update succeeded; clear the "already exists" error so callers
			// inspecting res.Response.Error do not misjudge this as a failure.
			res.Response.Error = nil
			return res, nil
		}
		// other errors return directly
		return nil, fmt.Errorf("CreateConsumerGroup API error: %s - %s", res.Response.Error.Code, res.Response.Error.Message)
	}
	return res, nil
}

// ListConsumerGroup query current consumer group information
func (c *ConsumerClient) ListConsumerGroup() (*cls.ConsumerGroupEntity, error) {
	resp, err := c.YunApiClient.ListConsumerGroup(c.LogsetID, c.TopicIDs)
	if err != nil {
		return nil, err
	}
	for _, group := range resp.Response.ConsumerGroupsInfo {
		if group.ConsumerGroup == c.ConsumerGroup {
			return &group, nil
		}
	}
	return nil, nil // not found
}

// DeleteConsumerGroup delete consumer group
func (c *ConsumerClient) DeleteConsumerGroup() error {
	resp, err := c.YunApiClient.DeleteConsumerGroup(c.LogsetID, c.ConsumerGroup)
	if err != nil {
		return err
	}
	if resp.Response.Error != nil {
		return fmt.Errorf("DeleteConsumerGroup API error: %s - %s", resp.Response.Error.Code, resp.Response.Error.Message)
	}
	return nil
}

// Heartbeat heartbeat
func (c *ConsumerClient) Heartbeat(partitions []cls.PartitionInfo, response *[]cls.PartitionInfo) bool {
	zapLogger, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	defer zapLogger.Sync()
	logger := cls.NewZapLogger(zapLogger)
	resp, err := c.YunApiClient.Heartbeat(c.LogsetID, c.ConsumerGroup, c.Consumer, partitions)
	if err != nil {
		logger.Warn("Heartbeat error:",
			cls.Field{Key: "error", Value: err.Error()},
		)
		return false
	}
	if resp.Response.Error != nil {
		logger.Warn("Heartbeat API error:",
			cls.Field{Key: "errorCode", Value: resp.Response.Error.Code},
			cls.Field{Key: "errorMessage", Value: resp.Response.Error.Message},
		)
		return false
	}
	if response != nil {
		*response = resp.Response.TopicPartitionsInfo
	}
	return true
}

// UpdateOffsets
func (c *ConsumerClient) UpdateOffsets(offsets []cls.TopicPartitionOffsetsInfo) error {
	resp, err := c.YunApiClient.UpdateOffsets(c.LogsetID, c.ConsumerGroup, c.Consumer, offsets)
	if err != nil {
		return err
	}
	if resp.Response.Error != nil {
		return fmt.Errorf("UpdateOffsets API error: %s - %s", resp.Response.Error.Code, resp.Response.Error.Message)
	}
	return nil
}

// GetOffsets get offsets
func (c *ConsumerClient) GetOffsets(topicID string, partitionID int, position string) ([]cls.TopicPartitionOffsetsInfo, error) {
	resp, err := c.YunApiClient.GetOffsets(c.LogsetID, c.ConsumerGroup, topicID, partitionID, position)
	if err != nil {
		return nil, err
	}
	offsets := resp.Response.TopicPartitionOffsetsInfo
	if len(offsets) == 0 {
		return nil, fmt.Errorf("fail to get offsets")
	}
	return offsets, nil
}

// Pull logs, call YunApiLogClient's PullLogsAndParse
func (c *ConsumerClient) PullLogs(topicId string, partitionId int, size int, startTime *int64, offset int64, endTime *int64) (*cls.PullLogResponse, error) {
	return c.PullLogsClient.PullLogsAndParse(topicId, partitionId, size, startTime, offset, endTime)
}

// GetPartitionOffsets get offset of specified partition
func (c *ConsumerClient) GetPartitionOffsets(topicID string, partitionID int, position string) (int64, error) {
	if topicID == "" || partitionID < 0 {
		return -1, fmt.Errorf("topicID or partitionID is invalid")
	}
	resp, err := c.YunApiClient.GetOffsets(c.LogsetID, c.ConsumerGroup, topicID, partitionID, position)
	if err != nil {
		return -1, err
	}
	for _, topicInfo := range resp.Response.TopicPartitionOffsetsInfo {
		if topicInfo.TopicID == topicID {
			for _, part := range topicInfo.PartitionOffsets {
				if part.PartitionID == partitionID {
					return part.Offset, nil
				}
			}
			return -1, fmt.Errorf("partition %d not found in topic %s", partitionID, topicID)
		}
	}
	return -1, fmt.Errorf("topic %s not found", topicID)
}
