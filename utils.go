package tencentcloud_cls_sdk_go

import (
	"errors"
	"net"

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

func GetLogSizeCalculate(log *Log) int {
	sizeInBytes := 4
	logContent := log.GetContents()
	count := len(logContent)
	for i := 0; i < count; i++ {
		sizeInBytes += len(*logContent[i].Value)
		sizeInBytes += len(*logContent[i].Key)
	}

	return sizeInBytes

}
func GetLogListSize(logList []*Log) int {
	sizeInBytes := 0
	for _, log := range logList {
		sizeInBytes += GetLogSizeCalculate(log)
	}
	return sizeInBytes
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
