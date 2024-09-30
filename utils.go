package tencentcloud_cls_sdk_go

import (
	"errors"
	"fmt"
	"hash/crc64"
	"math"
	"net"
	"strings"
	"time"

	"go.uber.org/atomic"
	"google.golang.org/protobuf/proto"
)

// NewCLSLog ...
func NewCLSLog(logTime int64, addLogMap map[string]string) *Log {
	var content = make([]*Log_Content, 0)
	for key, value := range addLogMap {
		content = append(content, &Log_Content{
			Key:   proto.String(key),
			Value: proto.String(value),
		})
	}
	return &Log{
		Time:     proto.Int64(logTime),
		Contents: content,
	}
}

func GetTimeMs(t int64) int64 {
	return t / 1000 / 1000
}

func GetLogSizeCalculate(log *Log) (int, error) {
	sizeInBytes := 4
	logContent := log.GetContents()
	count := len(logContent)

	for i := 0; i < count; i++ {
		if len(*logContent[i].Value) > 1*1024*1024 {
			return 0, fmt.Errorf("content value can not be than 1M")
		}
		sizeInBytes += len(*logContent[i].Value)
		sizeInBytes += len(*logContent[i].Key)
	}

	return sizeInBytes, nil

}
func GetLogListSize(logList []*Log) (int, error) {
	sizeInBytes := 0
	for _, log := range logList {
		sz, err := GetLogSizeCalculate(log)
		if err != nil {
			return 0, err
		}
		sizeInBytes += sz
	}
	return sizeInBytes, nil
}

// GetLocalIP get local IP with string format
func GetLocalIP() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}
	for _, address := range addrs {
		// 检查ip地址判断是否回环地址
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String(), nil
			}
		}
	}
	return "", errors.New("can not find local ip")
}

// GenerateProducerHash ...
func GenerateProducerHash(instanceID string) string {
	table := crc64.MakeTable(crc64.ECMA)
	hash := crc64.Checksum([]byte(instanceID), table)
	hashString := fmt.Sprintf("%08x", hash)
	return strings.ToUpper(fmt.Sprintf("%s%08x", hashString, time.Now().Unix()))
}

func generatePackageId(producerHash string, batchId *atomic.Int64) string {
	if batchId.Load() >= math.MaxInt64 {
		batchId.Store(0)
	}
	return strings.ToUpper(fmt.Sprintf("%s-%016x", producerHash, batchId.Inc()))
}
