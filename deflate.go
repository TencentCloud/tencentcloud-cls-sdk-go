package tencentcloud_cls_sdk_go

import (
	"bytes"
	"compress/flate"
)

// DeflateCompress ...
func DeflateCompress(data []byte) ([]byte, error) {
	var compressedData bytes.Buffer
	compressor, _ := flate.NewWriter(&compressedData, flate.DefaultCompression)
	if _, err := compressor.Write(data); err != nil {
		return nil, err
	}
	if err := compressor.Close(); err != nil {
		return nil, err
	} // 必须关闭写入器以确保所有数据都被刷新
	return compressedData.Bytes(), nil
}
