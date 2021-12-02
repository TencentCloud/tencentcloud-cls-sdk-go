package tencentcloud_cls_sdk_go

import (
	"crypto/hmac"
	"crypto/sha1"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

func calSha1sum(msg string) string {
	h := sha1.New()
	h.Write([]byte(msg))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func calSha1HMACDigest(key, msg string) string {
	h := hmac.New(sha1.New, []byte(key))
	h.Write([]byte(msg))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func checkHeaderKey(key string) bool {
	var lowerKey = strings.ToLower(key)
	if lowerKey == "content-type" || lowerKey == "content-md5" || lowerKey == "host" || lowerKey[0] == 'x' {
		return true
	}
	return false
}

func getURLEncode(v url.Values) (string, []string) {
	lowerURL := url.Values{}
	var lowerList []string
	for key, values := range v {
		for _, value := range values {
			var lowerKey = strings.ToLower(key)
			lowerURL.Add(lowerKey, value)
			lowerList = append(lowerList, lowerKey)
		}
	}
	// 按照key排序
	sort.Strings(lowerList)
	return lowerURL.Encode(), lowerList
}

// signature 计算请求签名，https://cloud.tencent.com/document/product/614/12445
func signature(secretID, secretKey, method, path string, params, headers url.Values, expire int64) string {
	// header先过滤出需要签名的参数
	hv := url.Values{}
	for key, values := range headers {
		for _, value := range values {
			if checkHeaderKey(key) {
				hv.Add(key, value)
			}
		}
	}

	// header与params计算签名参数排序
	formatHeaders, signedHeaderList := getURLEncode(hv)
	formatParameters, signedParameterList := getURLEncode(params)

	var formatString = fmt.Sprintf("%s\n%s\n%s\n%s\n", strings.ToLower(method),
		path, formatParameters, formatHeaders)
	var signTime = fmt.Sprintf("%d;%d", time.Now().Unix()-60, time.Now().Unix()+expire)
	var stringToSign = fmt.Sprintf("sha1\n%s\n%s\n", signTime, calSha1sum(formatString))
	var signKey = calSha1HMACDigest(secretKey, signTime)
	var signature = calSha1HMACDigest(signKey, stringToSign)
	return strings.Join([]string{
		"q-sign-algorithm=sha1",
		"q-ak=" + secretID,
		"q-sign-time=" + signTime,
		"q-key-time=" + signTime,
		"q-header-list=" + strings.Join(signedHeaderList, ";"),
		"q-url-param-list=" + strings.Join(signedParameterList, ";"),
		"q-signature=" + signature,
	}, "&")
}
