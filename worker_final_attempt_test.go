package tencentcloud_cls_sdk_go

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type recordingCallback struct {
	done   chan *Result
	failed bool
}

func (c *recordingCallback) Success(result *Result) { c.done <- result }
func (c *recordingCallback) Fail(result *Result)    { c.failed = true; c.done <- result }

// The callback of a batch that fails for good must see the error of the last
// request, whether retries are disabled or simply used up.
func TestFailCallbackCarriesLastAttempt(t *testing.T) {
	for _, retries := range []int{0, 2} {
		t.Run(fmt.Sprintf("retries=%d", retries), func(t *testing.T) {
			var requests int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := atomic.AddInt64(&requests, 1)
				w.Header().Set("X-Cls-Requestid", fmt.Sprintf("req-%d", n))
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer server.Close()

			config := GetDefaultAsyncProducerClientConfig()
			config.Endpoint = server.URL
			config.AccessKeyID = "id"
			config.AccessKeySecret = "key"
			config.LingerMs = 100
			config.Retries = retries
			config.BaseRetryBackoffMs = 10
			config.MaxRetryBackoffMs = 20

			producer, err := NewAsyncProducerClient(config)
			if err != nil {
				t.Fatal(err)
			}
			producer.Start()
			cb := &recordingCallback{done: make(chan *Result, 1)}
			if err := producer.SendLog("topic", NewCLSLog(time.Now().Unix(), map[string]string{"k": "v"}), cb); err != nil {
				t.Fatal(err)
			}
			var result *Result
			select {
			case result = <-cb.done:
			case <-time.After(10 * time.Second):
				t.Fatal("callback not called")
			}
			_ = producer.Close(5000)

			sent := atomic.LoadInt64(&requests)
			if !cb.failed {
				t.Fatalf("expected Fail callback")
			}
			if sent != int64(retries+1) {
				t.Fatalf("server saw %d requests, want %d", sent, retries+1)
			}
			if got := len(result.GetReservedAttempts()); got != retries+1 {
				t.Errorf("reserved attempts = %d, want %d (one per request)", got, retries+1)
			}
			if got, want := result.GetRequestId(), fmt.Sprintf("req-%d", sent); got != want {
				t.Errorf("GetRequestId() = %q, want %q (last request)", got, want)
			}
			if got := result.GetErrorCode(); got != INTERNAL_SERVER_ERROR {
				t.Errorf("GetErrorCode() = %q, want %q", got, INTERNAL_SERVER_ERROR)
			}
		})
	}
}

// Control: a batch that succeeds after failures keeps one attempt per request.
func TestSuccessAfterRetriesKeepsAllAttempts(t *testing.T) {
	var requests int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(&requests, 1)
		w.Header().Set("X-Cls-Requestid", fmt.Sprintf("req-%d", n))
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := GetDefaultAsyncProducerClientConfig()
	config.Endpoint = server.URL
	config.AccessKeyID = "id"
	config.AccessKeySecret = "key"
	config.LingerMs = 100
	config.Retries = 2
	config.BaseRetryBackoffMs = 10
	config.MaxRetryBackoffMs = 20
	producer, err := NewAsyncProducerClient(config)
	if err != nil {
		t.Fatal(err)
	}
	producer.Start()
	cb := &recordingCallback{done: make(chan *Result, 1)}
	if err := producer.SendLog("topic", NewCLSLog(time.Now().Unix(), map[string]string{"k": "v"}), cb); err != nil {
		t.Fatal(err)
	}
	var result *Result
	select {
	case result = <-cb.done:
	case <-time.After(10 * time.Second):
		t.Fatal("callback not called")
	}
	_ = producer.Close(5000)
	if cb.failed || !result.IsSuccessful() {
		t.Fatalf("expected Success callback")
	}
	if got := len(result.GetReservedAttempts()); got != 3 {
		t.Errorf("reserved attempts = %d, want 3", got)
	}
}
