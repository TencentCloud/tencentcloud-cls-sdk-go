package consumer

import (
	"strings"
	"testing"

	cls "github.com/tencentcloud/tencentcloud-cls-sdk-go"
)

func newTestProcessor() Processor {
	return &ConsumerProcessorAdaptor{
		ProcessorBase: NewProcessorBase(),
		Func: func(topicID string, partitionID int, logs []*cls.Log) interface{} {
			return true
		},
	}
}

func newValidConsumerOption() *ConsumerOption {
	return &ConsumerOption{
		Endpoint:             "ap-guangzhou.cls.tencentcs.com",
		AccessKeyID:          "test-ak",
		AccessKey:            "test-sk",
		Region:               "ap-guangzhou",
		LogsetID:             "test-logset-id",
		TopicIDs:             []string{"topic1"},
		ConsumerGroup:        "test-consumer-group",
		ConsumerName:         "test-consumer",
		HeartbeatInterval:    3,
		DataFetchInterval:    1,
		OffsetStartTime:      "begin",
		MaxFetchLogGroupSize: 1000,
		ConsumerGroupTimeout: 20,
	}
}

func TestNewConsumerWorkerWithOptionInvalidConfig(t *testing.T) {
	optionWithoutAK := newValidConsumerOption()
	optionWithoutAK.AccessKeyID = ""

	optionWithoutSK := newValidConsumerOption()
	optionWithoutSK.AccessKey = ""

	optionWithoutEndpoint := newValidConsumerOption()
	optionWithoutEndpoint.Endpoint = ""

	cases := []struct {
		name       string
		option     *ConsumerOption
		processor  Processor
		wantErrMsg string
	}{
		{
			name:       "option 为 nil",
			option:     nil,
			processor:  newTestProcessor(),
			wantErrMsg: "consumer option cannot be nil",
		},
		{
			name:       "processor 为 nil",
			option:     newValidConsumerOption(),
			processor:  nil,
			wantErrMsg: "processor cannot be nil",
		},
		{
			name:       "endpoint 为空",
			option:     optionWithoutEndpoint,
			processor:  newTestProcessor(),
			wantErrMsg: "consumer option endpoint cannot be empty",
		},
		{
			name:       "AccessKeyID 为空",
			option:     optionWithoutAK,
			processor:  newTestProcessor(),
			wantErrMsg: "failed to create yunapi log client",
		},
		{
			name:       "AccessKey 为空",
			option:     optionWithoutSK,
			processor:  newTestProcessor(),
			wantErrMsg: "failed to create yunapi log client",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			worker, err := NewConsumerWorkerWithOption(tc.option, tc.processor)
			if err == nil {
				t.Fatal("NewConsumerWorkerWithOption should return error")
			}
			if worker != nil {
				t.Fatalf("worker should be nil when config is invalid, got %#v", worker)
			}
			if !strings.Contains(err.Error(), tc.wantErrMsg) {
				t.Fatalf("error should contain %q, got %q", tc.wantErrMsg, err.Error())
			}
		})
	}
}

func TestNewConsumerWorkerWithOptionSuccess(t *testing.T) {
	option := newValidConsumerOption()
	processor := newTestProcessor()

	worker, err := NewConsumerWorkerWithOption(option, processor)
	if err != nil {
		t.Fatalf("NewConsumerWorkerWithOption returned error: %v", err)
	}
	if worker == nil {
		t.Fatal("worker should not be nil")
	}
	defer worker.Shutdown()

	if worker.ConsumerOption != option {
		t.Fatal("ConsumerOption should be the passed option")
	}
	if worker.Processor != processor {
		t.Fatal("Processor should be the passed processor")
	}
	if worker.ConsumerClient == nil {
		t.Fatal("ConsumerClient should not be nil")
	}
	if worker.ConsumerClient.YunApiClient == nil || worker.ConsumerClient.YunApiClient.CLSClient == nil {
		t.Fatal("YunApiClient and its embedded CLSClient should not be nil")
	}
	if worker.ConsumerClient.PullLogsClient == nil {
		t.Fatal("PullLogsClient should not be nil")
	}
	if worker.ConsumerClient.PullLogsClient.Endpoint != option.Endpoint {
		t.Fatalf("PullLogsClient endpoint = %q, want %q", worker.ConsumerClient.PullLogsClient.Endpoint, option.Endpoint)
	}
	if worker.ConsumerClient.LogsetID != option.LogsetID || worker.ConsumerClient.Region != option.Region {
		t.Fatalf("unexpected consumer client fields: %#v", worker.ConsumerClient)
	}
	if worker.PartitionWorkers == nil {
		t.Fatal("PartitionWorkers map should be initialized")
	}
	if worker.Logger == nil {
		t.Fatal("Logger should be initialized")
	}
	if worker.IsShutdown() {
		t.Fatal("new worker should not be shutdown")
	}
	if worker.ctx == nil || worker.cancel == nil {
		t.Fatal("context and cancel func should be initialized")
	}
	select {
	case <-worker.ctx.Done():
		t.Fatal("context should not be cancelled right after construction")
	default:
	}
}

func TestDeprecatedNewConsumerWorker(t *testing.T) {
	invalidOption := newValidConsumerOption()
	invalidOption.AccessKeyID = ""
	if worker := NewConsumerWorker(invalidOption, newTestProcessor()); worker != nil {
		t.Fatalf("NewConsumerWorker should return nil on invalid credentials, got %#v", worker)
	}
	if worker := NewConsumerWorker(nil, newTestProcessor()); worker != nil {
		t.Fatalf("NewConsumerWorker should return nil on nil option, got %#v", worker)
	}
	if worker := NewConsumerWorker(newValidConsumerOption(), nil); worker != nil {
		t.Fatalf("NewConsumerWorker should return nil on nil processor, got %#v", worker)
	}

	option := newValidConsumerOption()
	worker := NewConsumerWorker(option, newTestProcessor())
	if worker == nil {
		t.Fatal("NewConsumerWorker should return worker on valid option")
	}
	defer worker.Shutdown()

	if worker.ConsumerClient == nil || worker.ConsumerClient.YunApiClient == nil ||
		worker.ConsumerClient.YunApiClient.CLSClient == nil {
		t.Fatal("deprecated constructor should build a fully usable consumer client")
	}
	if worker.ConsumerOption != option {
		t.Fatal("ConsumerOption should be the passed option")
	}
}
