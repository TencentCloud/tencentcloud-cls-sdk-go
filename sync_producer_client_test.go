package tencentcloud_cls_sdk_go

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
)

func TestSyncProduce(t *testing.T) {
	config := GetDefaultSyncProducerClientConfig()
	config.Endpoint = "ap-guangzhou.cls.tencentcs.com"
	config.Credential = common.NewCredential("", "")

	config.CompressType = "zstd"
	topicID := ""
	client, err := NewSyncProducerClient(config)
	if err != nil {
		t.Error(err)
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
