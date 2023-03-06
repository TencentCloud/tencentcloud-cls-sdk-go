package tencentcloud_cls_sdk_go

import (
	"context"
	"errors"
)

// SyncProducerClient synchronized producer client
type SyncProducerClient struct {
	config *SyncProducerClientConfig
	client *CLSClient
	source string
}

// NewSyncProducerClient new sync producer client
func NewSyncProducerClient(config *SyncProducerClientConfig) (*SyncProducerClient, error) {
	c := new(SyncProducerClient)
	c.validateConfig(config)
	c.config = config
	client, err := NewCLSClient(&Options{
		Host:         config.Endpoint,
		SecretID:     config.AccessKeyID,
		SecretKEY:    config.AccessKeySecret,
		SecretToken:  config.AccessToken,
		Timeout:      config.Timeout,
		IdleConn:     config.IdleConn,
		CompressType: config.CompressType,
	})
	if err != nil {
		return nil, err
	}
	c.client = client
	return c, nil
}

// validateConfig validate config parameters
func (c *SyncProducerClient) validateConfig(config *SyncProducerClientConfig) {
	if config.Timeout <= 0 {
		config.Timeout = 10000
	}
	if config.IdleConn <= 0 {
		config.IdleConn = 50
	}
	if config.NeedSource {
		c.source, _ = GetLocalIP()
	}
}

// SendLogList send batched logs to cls
func (c *SyncProducerClient) SendLogList(ctx context.Context, topicID string, logList []*Log) error {
	size, err := GetLogListSize(logList)
	if err != nil {
		return err
	}
	if size > 5242880 || len(logList) > 10000 {
		return errors.New("logs must be less than 5M and 10000 lines")
	}
	logGroup := &LogGroup{
		Logs: logList,
	}
	if c.config.NeedSource {
		logGroup.Source = &c.source
	}
	clsErr := c.client.Send(ctx, topicID, logGroup)
	if clsErr != nil {
		return clsErr
	}
	return nil
}

// ResetSecretToken reset temporary secret info
func (c *SyncProducerClient) ResetSecretToken(secretID string, secretKEY string, secretToken string) error {
	return c.client.ResetSecretToken(secretID, secretKEY, secretToken)
}
