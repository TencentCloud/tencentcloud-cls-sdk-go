package tencentcloud_cls_sdk_go

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

type Callback struct {
}

func (callback *Callback) Success(result *Result) {
	attemptList := result.GetReservedAttempts()
	for _, attempt := range attemptList {
		fmt.Printf("%+v \n", attempt)
	}
}

func (callback *Callback) Fail(result *Result) {
	fmt.Println(result.IsSuccessful())
	fmt.Println(result.GetErrorCode())
	fmt.Println(result.GetErrorMessage())
	fmt.Println(result.GetReservedAttempts())
	fmt.Println(result.GetRequestId())
	fmt.Println(result.GetTimeStampMs())
}

func TestNewAsyncProducerClient(t *testing.T) {
	producerConfig := GetDefaultAsyncProducerClientConfig()
	producerConfig.Endpoint = "ap-guangzhou.cls.tencentcs.com"
	producerConfig.AccessKeyID = ""
	producerConfig.AccessKeySecret = ""
	topicId := ""
	producerInstance, err := NewAsyncProducerClient(producerConfig)
	if err != nil {
		t.Error(err)
	}

	producerInstance.Start()
	var m sync.WaitGroup
	callBack := &Callback{}
	for i := 0; i < 10; i++ {
		m.Add(1)
		go func() {
			defer m.Done()
			for i := 0; i < 1000; i++ {
				log := NewCLSLog(time.Now().Unix(), map[string]string{"content": "hello world| I'm from Beijing", "content2": fmt.Sprintf("%v", i)})
				err = producerInstance.SendLog(topicId, log, callBack)
				if err != nil {
					t.Error(err)
				}
			}
		}()
	}
	m.Wait()
	producerInstance.Close(60000)
}
