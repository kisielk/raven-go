package raven

import (
	"compress/zlib"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"testing"
	"time"
)

func BuildSentryDSN(baseUrl, publicKey, secretKey, project, sentryPath string) string {
	u, _ := url.Parse(baseUrl)
	u.User = url.UserPassword(publicKey, secretKey)
	u.Path = path.Join(sentryPath, project)
	return u.String()
}

func GetServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, req *http.Request) {
			fmt.Fprint(w, "hello")
		}))
}

func GetClient(server *httptest.Server) *Client {
	publicKey := "abcd"
	secretKey := "efgh"
	project := "1"
	sentryPath := "/sentry/path"

	// Build the client
	client, err := NewClient(BuildSentryDSN(server.URL, publicKey, secretKey, project, sentryPath))
	if err != nil {
		panic(fmt.Sprintf("failed to make client: %s", err))
	}
	return client
}

func TestClientSetup(t *testing.T) {
	publicKey := "abcd"
	secretKey := "efgh"
	project := "1"
	sentryPath := "/sentry/path"
	server := GetServer()
	defer server.Close()
	client := GetClient(server)

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
}

func TestCaptureMessage(t *testing.T) {
	server := GetServer()
	defer server.Close()
	client := GetClient(server)
	_, err := client.CaptureMessage("test message")
	if err != nil {
		t.Logf("CaptureMessage failed: %s", err)
		t.Fail()
	}
}

func TestCapture(t *testing.T) {
	server := GetServer()
	defer server.Close()
	client := GetClient(server)

	// Send the message
	testEvent := func(ev *Event) {
		err := client.Capture(ev)
		if err != nil {
			t.Fatal(err)
		}
		// All fields must be set
		if ev.EventId == "" {
			t.Error("EventId must not be empty.")
		}
		if ev.Project == "" {
			t.Error("Project must not be empty.")
		}
		if ev.Timestamp == "" {
			t.Error("Timestamp must not be empty.")
		}
		if ev.Level == "" {
			t.Error("Level must not be empty.")
		}
		if ev.Logger == "" {
			t.Error("Logger must not be empty.")
		}
		if fmt.Sprintf("test.%s.%s", ev.Logger, ev.Level) != ev.Message {
			t.Errorf("Expected message to match error and logger %s == test.%s.%s", ev.Message, ev.Logger, ev.Level)
		}
	}

	testEvent(&Event{Message: "test.root.error"})
	testEvent(&Event{Message: "test.root.warn", Level: "warn"})
	testEvent(&Event{Message: "test.auth.error", Logger: "auth"})
	testEvent(&Event{Message: "test.root.error", Timestamp: "2013-10-17T11:25:59"})
	testEvent(&Event{Message: "test.root.error", EventId: "1234-34567-8912-124123"})
	testEvent(&Event{Message: "test.auth.info", Level: "info", Logger: "auth"})
}

func TestTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, req *http.Request) {
			time.Sleep(3100 * time.Millisecond)
			fmt.Fprint(w, "hello")
		}))
	defer server.Close()
	client := GetClient(server)

	_, err := client.CaptureMessage("Test message")
	if err == nil {
		t.Fatalf("Request should have timed out")
	}

	// Build the client with a timeout
	client, err = NewClient(client.URL.String() + "?timeout=4")
	if err != nil {
		t.Fatalf("failed to make client: %s", err)
	}
	_, err = client.CaptureMessage("Test message")
	if err != nil {
		t.Fatalf("Request should not have timed out")
	}
}

func decode(buf io.ReadCloser) (ev *Event, err error) {
	ev = new(Event)
	b64Decoder := base64.NewDecoder(base64.StdEncoding, buf)
	reader, err := zlib.NewReader(b64Decoder)
	if err != nil {
		return
	}

	jsonDecoder := json.NewDecoder(reader)
	if err = jsonDecoder.Decode(ev); err != nil {
		return
	}

	if err = reader.Close(); err != nil {
		return
	}
	return ev, nil
}

func TestStacktrace(t *testing.T) {
	var capturedEvent *Event
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, req *http.Request) {
			fmt.Fprint(w, "hello")
			capturedEvent, _ = decode(req.Body)
		}))
	defer server.Close()
	client := GetClient(server)

	// We nest the calls, and ensur that the correct part of the stack is present
	func() {
		func() {
			client.CaptureMessage("Test With trace")
		}()
	}()

	// Should be four frames on stack, two for testrunner, two for nesting
	if len(capturedEvent.Stacktrace.Frames) != 4 {
		t.Fatalf("Wrong number of frames on stack, %v", capturedEvent.Stacktrace)
	}
}
