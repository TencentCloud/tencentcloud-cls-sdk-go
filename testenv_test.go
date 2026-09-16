package tencentcloud_cls_sdk_go

import (
	"os"
	"testing"
)

// integrationCredentials holds the credentials and target topic used by the
// producer integration tests.
type integrationCredentials struct {
	SecretID    string
	SecretKey   string
	SecretToken string
	TopicID     string
	Endpoint    string
}

// requireIntegrationCredentials reads the credentials from the environment and
// skips the calling test when they are not configured. Secrets are never
// hard-coded in the repository, so tests that need to reach a real CLS endpoint
// are opt-in via environment variables.
func requireIntegrationCredentials(t *testing.T) integrationCredentials {
	t.Helper()

	credentials := integrationCredentials{
		SecretID:    os.Getenv("TENCENTCLOUD_SECRET_ID"),
		SecretKey:   os.Getenv("TENCENTCLOUD_SECRET_KEY"),
		SecretToken: os.Getenv("TENCENTCLOUD_SECRET_TOKEN"),
		TopicID:     os.Getenv("CLS_TOPIC_ID"),
		Endpoint:    os.Getenv("CLS_ENDPOINT"),
	}
	if credentials.SecretID == "" || credentials.SecretKey == "" || credentials.TopicID == "" {
		t.Skip("skipping integration test: set TENCENTCLOUD_SECRET_ID, TENCENTCLOUD_SECRET_KEY and CLS_TOPIC_ID to enable it")
	}
	return credentials
}
