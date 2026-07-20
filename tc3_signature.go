package tencentcloud_cls_sdk_go

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// SignatureWithTC3 generates TC3-HMAC-SHA256 signature
func SignatureWithTC3(secretID, secretKey, service, method, path string, queryString string, headers map[string]string, payload []byte, timestamp int64) (string, error) {
	algorithm := "TC3-HMAC-SHA256"
	canonicalURI := path
	canonicalQueryString := queryString

	// Normalize headers, only sign content-type and host
	var signedHeaders []string
	var canonicalHeaders strings.Builder
	lowerHeaders := make(map[string]string)
	for k, v := range headers {
		lk := strings.ToLower(k)
		if lk == "content-type" || lk == "host" {
			lowerHeaders[lk] = strings.TrimSpace(v)
		}
	}
	for k := range lowerHeaders {
		signedHeaders = append(signedHeaders, k)
	}
	sort.Strings(signedHeaders)
	for _, k := range signedHeaders {
		canonicalHeaders.WriteString(fmt.Sprintf("%s:%s\n", k, lowerHeaders[k]))
	}
	sh := strings.Join(signedHeaders, ";")

	payloadHash := sha256Hex(payload)

	canonicalRequest := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		method,
		canonicalURI,
		canonicalQueryString,
		canonicalHeaders.String(),
		sh,
		payloadHash,
	)

	date := time.Unix(timestamp, 0).UTC().Format("2006-01-02")
	credentialScope := fmt.Sprintf("%s/%s/tc3_request", date, service)
	canonicalRequestHash := sha256Hex([]byte(canonicalRequest))
	stringToSign := fmt.Sprintf("%s\n%d\n%s\n%s",
		algorithm,
		timestamp,
		credentialScope,
		canonicalRequestHash,
	)

	signingKey := getTC3SignatureKey(secretKey, date, service)
	signature := hmacSHA256Hex(signingKey, stringToSign)

	// Authorization header
	auth := fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		algorithm,
		secretID,
		credentialScope,
		sh,
		signature,
	)
	return auth, nil
}

// sha256Hex calculates SHA256 hash and returns hexadecimal string
func sha256Hex(data []byte) string {
	h := sha256.New()
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// hmacSHA256Hex calculates HMAC-SHA256 and returns hexadecimal string
func hmacSHA256Hex(key []byte, msg string) string {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(msg))
	return hex.EncodeToString(h.Sum(nil))
}

// getTC3SignatureKey derives TC3 signature key
func getTC3SignatureKey(secretKey, date, service string) []byte {
	kDate := hmacSHA256([]byte("TC3"+secretKey), date)
	kService := hmacSHA256(kDate, service)
	kSigning := hmacSHA256(kService, "tc3_request")
	return kSigning
}

// hmacSHA256 calculates HMAC-SHA256
func hmacSHA256(key []byte, msg string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(msg))
	return h.Sum(nil)
}
