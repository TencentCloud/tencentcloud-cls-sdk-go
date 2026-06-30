//go:build examples
// +build examples

package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tencentcloud/tencentcloud-cls-sdk-go"
	"github.com/tencentcloud/tencentcloud-cls-sdk-go/consumer"
)

// SampleProcessor example processor
type SampleProcessor struct {
	*consumer.ProcessorBase
}

func NewSampleProcessor() *SampleProcessor {
	return &SampleProcessor{
		ProcessorBase: consumer.NewProcessorBase(),
	}
}

func (p *SampleProcessor) Process(logs []*tencentcloud_cls_sdk_go.Log, tracker *consumer.OffsetTracker) (interface{}, error) {
	log.Printf("Processing %d logs", len(logs))
	for _, logItem := range logs {
		// user-defined logic to process logs
		log.Printf("Processing log: time=%d, contents=%+v", logItem.Time, logItem.Contents)
	}
	// submit offset
	p.SaveOffset(tracker, false)
	return nil, nil
}

func main() {
	log.Println("=== CLS consumer demo ===")

	// user-defined configuration parameters, please replace
	var (
		endpoint      = "ap-beijing.cls.tencentcs.com"
		secretID      = "Your_Secret_ID"
		secretKey     = "Your_Secret_Key"
		logsetID      = "Your_Logset_ID"
		TopicIDs      = []string{"Your_Topic_ID"} // multiple topics separated by commas
		consumerGroup = "Your_Consumer_Group"
		consumerName1 = "Your_Consumer_Name"
		region        = "ap-beijing"
	)

	// create consumer configuration 1
	consumerOption1 := &consumer.ConsumerOption{
		Endpoint:             endpoint,
		AccessKeyID:          secretID,
		AccessKey:            secretKey,
		Region:               region,
		LogsetID:             logsetID,
		TopicIDs:             TopicIDs,
		ConsumerGroup:        consumerGroup,
		ConsumerName:         consumerName1,
		HeartbeatInterval:    3,
		DataFetchInterval:    1,
		OffsetStartTime:      "begin", // start from the beginning
		MaxFetchLogGroupSize: 1000,    // default
		OffsetEndTime:        "",      // do not set end time, consume all data
		ConsumerGroupTimeout: 20,
		StartTime:            int64(0),
		EndTime:              int64(0),
		// (Optional) DSL pre-filter expression for server-side filtering.
		// When set on ConsumerOption.Query, only logs that match the expression are
		// returned to the consumer, which saves both network bandwidth and
		// client-side CPU. Leave it unset / empty to disable server-side filtering
		// (default behavior).
		//
		// Example: only keep logs whose `status` field is greater than 400 AND
		// whose `message` field contains the keyword "access failed":
		//
		//   query := `log_keep(op_and(op_gt(v("status"), 400), str_exist(v("message"), "access failed")))`
		//   consumerOption1.Query = query
		//
		// DSL syntax reference:
		// https://cloud.tencent.com/document/product/614/37908
		Query: "",
	}

	// create processor
	processor1 := NewSampleProcessor()

	// create consumer Worker
	worker1 := consumer.NewConsumerWorker(consumerOption1, processor1)

	// create context for signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// start consumer
	err1 := worker1.Run(ctx)
	if err1 != nil {
		log.Fatalf("Failed to run consumer1: %v", err1)
	}

	log.Println("Consumer running successfully")
	log.Println("Press Ctrl+C to stop the consumer...")

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			}
		}
	}()

	// listen to system signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// wait for signal or context cancellation
	select {
	case sig := <-sigChan:
		log.Printf("Received signal: %v, shutting down...", sig)
	case <-ctx.Done():
		log.Println("Context cancelled, shutting down...")
	}
	cancel()

	// wait for consumer to completely stop
	log.Println("Waiting for consumer to shutdown...")
	time.Sleep(5 * time.Second)

	log.Println("Consumer shutdown completed")
}
