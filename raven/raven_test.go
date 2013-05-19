package raven

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"testing"
)

func TestCaptureMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, req *http.Request) {
			fmt.Fprint(w, "hello")
		}))
	defer server.Close()

	publicKey := "abcd"
	secretKey := "efgh"
	project := "1"
	sentryPath := "/sentry/path"

	// Build the client
	u, _ := url.Parse(server.URL)
	u.User = url.UserPassword(publicKey, secretKey)
	u.Path = path.Join(sentryPath, project)
	client, err := NewClient(u.String())
	if err != nil {
		t.Fatalf("failed to make client: %s", err)
	}

	// Test the client is set up correctly
	if client.PublicKey != publicKey {
		t.Logf("bad public key: got %s, want %s", client.PublicKey, publicKey)
		t.Fail()
	}
	if client.SecretKey != secretKey {
		t.Logf("bad public key: got %s, want %s", client.PublicKey, publicKey)
		t.Fail()
	}
	if client.Project != project {
		t.Logf("bad project: got %s, want %s", client.Project, project)
		t.Fail()
	}
	if client.URL.Path != sentryPath {
		t.Logf("bad path: got %s, want %s", client.URL.Path, sentryPath)
		t.Fail()
	}

	_, err = client.CaptureMessage("test message")
	if err != nil {
		t.Logf("CaptureMessage failed: %s", err)
		t.Fail()
	}
}
