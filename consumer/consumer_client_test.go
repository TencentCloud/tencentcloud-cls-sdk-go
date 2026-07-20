package consumer

import (
	"strings"
	"testing"

	cls "github.com/tencentcloud/tencentcloud-cls-sdk-go"
)

func TestGetPartitionOffsetsInvalidParams(t *testing.T) {
	client := &ConsumerClient{}

	testCases := []struct {
		name        string
		topicID     string
		partitionID int
		expectedErr string
	}{
		{name: "empty topic", topicID: "", partitionID: 0, expectedErr: "topicID or partitionID is invalid"},
		{name: "negative partition", topicID: "topic1", partitionID: -1, expectedErr: "topicID or partitionID is invalid"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			offset, err := client.GetPartitionOffsets(tc.topicID, tc.partitionID, "begin")
			if err == nil {
				t.Fatal("GetPartitionOffsets should return error")
			}
			if offset != -1 {
				t.Fatalf("offset should be -1, got %d", offset)
			}
			if !strings.Contains(err.Error(), tc.expectedErr) {
				t.Fatalf("error should contain %q, got %q", tc.expectedErr, err.Error())
			}
		})
	}
}

func TestNewConsumerClientSetsFields(t *testing.T) {
	client := NewConsumerClient(
		nil,
		nil,
		"test-logset-id",
		[]string{"topic1", "topic2"},
		"test-consumer-group",
		"test-consumer",
		"ap-beijing",
	)

	if client == nil {
		t.Fatal("NewConsumerClient should return client")
	}
	if client.LogsetID != "test-logset-id" {
		t.Fatalf("LogsetID = %q", client.LogsetID)
	}
	if len(client.TopicIDs) != 2 || client.TopicIDs[0] != "topic1" || client.TopicIDs[1] != "topic2" {
		t.Fatalf("TopicIDs = %#v", client.TopicIDs)
	}
	if client.ConsumerGroup != "test-consumer-group" {
		t.Fatalf("ConsumerGroup = %q", client.ConsumerGroup)
	}
	if client.Consumer != "test-consumer" {
		t.Fatalf("Consumer = %q", client.Consumer)
	}
	if client.Region != "ap-beijing" {
		t.Fatalf("Region = %q", client.Region)
	}
}

func TestConsumerClientEmptyFields(t *testing.T) {
	client := &ConsumerClient{}

	if client.LogsetID != "" {
		t.Fatalf("LogsetID should be empty, got %q", client.LogsetID)
	}
	if len(client.TopicIDs) != 0 {
		t.Fatalf("TopicIDs should be empty, got %d", len(client.TopicIDs))
	}
	if client.ConsumerGroup != "" {
		t.Fatalf("ConsumerGroup should be empty, got %q", client.ConsumerGroup)
	}
	if client.Consumer != "" {
		t.Fatalf("Consumer should be empty, got %q", client.Consumer)
	}
	if client.Region != "" {
		t.Fatalf("Region should be empty, got %q", client.Region)
	}
}

func TestConsumerClientTopicIDsManipulation(t *testing.T) {
	client := &ConsumerClient{
		TopicIDs: []string{"topic1", "topic2", "topic3"},
	}

	if len(client.TopicIDs) != 3 {
		t.Fatalf("TopicIDs should have length 3, got %d", len(client.TopicIDs))
	}
	client.TopicIDs = append(client.TopicIDs, "topic4")
	if len(client.TopicIDs) != 4 {
		t.Fatalf("after append, TopicIDs should have length 4, got %d", len(client.TopicIDs))
	}
	if client.TopicIDs[3] != "topic4" {
		t.Fatalf("TopicIDs[3] should be topic4, got %q", client.TopicIDs[3])
	}
}

func TestPullLogResponseUsesCurrentSDKFields(t *testing.T) {
	logTime := int64(1234567890)
	key := "message"
	value := "test log"
	resp := &cls.PullLogResponse{
		NextOffset: 101,
		LogGroups: []*cls.LogGroup{
			{
				Logs: []*cls.Log{
					{
						Time: &logTime,
						Contents: []*cls.Log_Content{
							{Key: &key, Value: &value},
						},
					},
				},
			},
		},
	}

	if resp.GetNextOffset() != 101 {
		t.Fatalf("next offset should be 101, got %d", resp.GetNextOffset())
	}
	if len(resp.GetLogGroups()) != 1 {
		t.Fatalf("expected 1 log group, got %d", len(resp.GetLogGroups()))
	}
	if resp.GetLogCount() != 1 {
		t.Fatalf("expected log count 1, got %d", resp.GetLogCount())
	}
	logs := resp.GetLogGroups()[0].Logs
	if len(logs) != 1 || logs[0].GetTime() != logTime {
		t.Fatalf("unexpected logs: %#v", logs)
	}
	contents := logs[0].GetContents()
	if len(contents) != 1 || contents[0].GetKey() != key || contents[0].GetValue() != value {
		t.Fatalf("unexpected contents: %#v", contents)
	}
}

func BenchmarkConsumerClientFieldAccess(b *testing.B) {
	client := &ConsumerClient{
		LogsetID:      "test-logset-id",
		TopicIDs:      []string{"topic1", "topic2", "topic3"},
		ConsumerGroup: "test-consumer-group",
		Consumer:      "test-consumer",
		Region:        "ap-beijing",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = client.LogsetID
		_ = client.TopicIDs
		_ = client.ConsumerGroup
		_ = client.Consumer
		_ = client.Region
	}
}
