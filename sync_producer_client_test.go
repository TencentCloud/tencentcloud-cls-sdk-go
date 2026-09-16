package tencentcloud_cls_sdk_go

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestSyncProduce(t *testing.T) {
	credentials := requireIntegrationCredentials(t)

	config := GetDefaultSyncProducerClientConfig()
	config.Endpoint = "ap-guangzhou.cls.tencentcs.com"
	if credentials.Endpoint != "" {
		config.Endpoint = credentials.Endpoint
	}
	config.AccessKeyID = credentials.SecretID
	config.AccessKeySecret = credentials.SecretKey
	config.AccessToken = credentials.SecretToken
	config.CompressType = "zstd"
	topicID := credentials.TopicID
	client, err := NewSyncProducerClient(config)
	if err != nil {
		t.Fatalf("NewSyncProducerClient returned error: %v", err)
	}
	logList := make([]*Log, 0)
	for i := 0; i < 100; i++ {
		log := NewCLSLog(time.Now().Unix(), map[string]string{"number": fmt.Sprint(i), "topic_id": topicID})
		logList = append(logList, log)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = client.SendLogList(ctx, topicID, logList)
	if err != nil {
		t.Error(err)
	}
}

func TestSyncProduceByRegionAndNetworkType(t *testing.T) {
	credentials := requireIntegrationCredentials(t)

	config := GetDefaultSyncProducerClientConfig()
	config.SetEndpointByRegionAndNetworkType(Shanghai, Intranet)
	config.AccessKeyID = credentials.SecretID
	config.AccessKeySecret = credentials.SecretKey
	config.AccessToken = credentials.SecretToken
	config.CompressType = "zstd"
	topicID := credentials.TopicID
	client, err := NewSyncProducerClient(config)
	if err != nil {
		t.Fatalf("NewSyncProducerClient returned error: %v", err)
	}
	logList := make([]*Log, 0)
	for i := 0; i < 100; i++ {
		log := NewCLSLog(time.Now().Unix(), map[string]string{"number": fmt.Sprint(i), "topic_id": topicID})
		logList = append(logList, log)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = client.SendLogList(ctx, topicID, logList)
	if err != nil {
		t.Error(err)
	}
}
