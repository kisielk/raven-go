/*

	Package raven is a client and library for sending messages and exceptions to Sentry: http://getsentry.com

	Usage:

	Create a new client using the NewClient() function. The value for the DSN parameter can be obtained
	from the project page in the Sentry web interface. After the client has been created use the CaptureMessage
	method to send messages to the server.

		client, err := raven.NewClient(dsn)
		...
		id, err := client.CaptureMessage("some text")

	If you want to have more finegrained control over the send event, you can create the event instance yourself

		client.Capture(&raven.Event{Message: "Some Text", Logger:"auth"})

*/
package raven

import (
	"bytes"
	"compress/zlib"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	URL        *url.URL
	PublicKey  string
	SecretKey  string
	Project    string
	httpClient *http.Client
	encoder    EventEncoder
}

type Frame struct {
	Filename   string `json:"filename"`
	LineNumber int    `json:"lineno"`
	FilePath   string `json:"abs_path"`
	Function   string `json:"function"`
	Module     string `json:"module"`
}

type Stacktrace struct {
	Frames []Frame `json:"frames"`
}

func generateStacktrace() (stacktrace Stacktrace) {
	maxDepth := 10
	// Start on depth 1 to avoid stack for generateStacktrace
	for depth := 1; depth < maxDepth; depth++ {
		pc, filePath, line, ok := runtime.Caller(depth)
		if !ok {
			break
		}
		f := runtime.FuncForPC(pc)
		if strings.Contains(f.Name(), "runtime") {
			// Stop when reaching runtime
			break
		}
		if strings.Contains(f.Name(), "raven.Client") {
			// Skip internal calls
			continue
		}
		functionName := f.Name()
		var moduleName string
		if strings.Contains(f.Name(), "(") {
			components := strings.SplitN(f.Name(), ".(", 2)
			functionName = "(" + components[1]
			moduleName = components[0]
		}
		fileName := path.Base(filePath)
		frame := Frame{Filename: fileName, LineNumber: line, FilePath: filePath,
			Function: functionName, Module: moduleName}
		stacktrace.Frames = append(stacktrace.Frames, frame)
	}
	return
}

type Event struct {
	EventId    string     `json:"event_id"`
	Project    string     `json:"project"`
	Message    string     `json:"message"`
	Timestamp  string     `json:"timestamp"`
	Level      string     `json:"level"`
	Logger     string     `json:"logger"`
	Culprit    string     `json:"culprit"`
	Stacktrace Stacktrace `json:"stacktrace"`
}

type sentryResponse struct {
	ResultId string `json:"result_id"`
}

// Template for the X-Sentry-Auth header
const xSentryAuthTemplate = "Sentry sentry_version=2.0, sentry_client=raven-go/0.1, sentry_timestamp=%v, sentry_key=%v"

// An iso8601 timestamp without the timezone. This is the format Sentry expects.
const iso8601 = "2006-01-02T15:04:05"

const defaultTimeout = 3 * time.Second

// NewClient creates a new client for a server identified by the given dsn
// A dsn is a string in the form:
//	{PROTOCOL}://{PUBLIC_KEY}:{SECRET_KEY}@{HOST}/{PATH}{PROJECT_ID}
// eg:
//	http://abcd:efgh@sentry.example.com/sentry/project1
func NewClient(dsn string) (client *Client, err error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, err
	}

	basePath := path.Dir(u.Path)
	project := path.Base(u.Path)

	if u.User == nil {
		return nil, fmt.Errorf("the DSN must contain a public and secret key")
	}
	publicKey := u.User.Username()
	secretKey, keyIsSet := u.User.Password()
	if !keyIsSet {
		return nil, fmt.Errorf("the DSN must contain a secret key")
	}

	u.Path = basePath

	check := func(req *http.Request, via []*http.Request) error {
		fmt.Printf("%+v", req)
		return nil
	}

	httpConnectTimeout := defaultTimeout
	httpReadWriteTimeout := defaultTimeout
	if st := u.Query().Get("timeout"); st != "" {
		if timeout, err := strconv.Atoi(st); err == nil {
			httpConnectTimeout = time.Duration(timeout) * time.Second
			httpReadWriteTimeout = time.Duration(timeout) * time.Second
		} else {
			return nil, fmt.Errorf("Timeout should have an Integer argument")
		}
	}

	transport := &transport{
		httpTransport: &http.Transport{
			Dial:  timeoutDialer(httpConnectTimeout),
			Proxy: http.ProxyFromEnvironment,
		}, timeout: httpReadWriteTimeout}
	httpClient := &http.Client{
		Transport:     transport,
		CheckRedirect: check,
	}
	return &Client{URL: u, PublicKey: publicKey, SecretKey: secretKey, httpClient: httpClient, Project: project, encoder: &Encoder{}}, nil
}

// CaptureMessage sends a message to the Sentry server.
// It returns the Sentry event ID or an empty string and any error that occurred.
func (client Client) CaptureMessage(message ...string) (string, error) {
	ev := Event{Message: strings.Join(message, " ")}
	sentryErr := client.Capture(&ev)

	if sentryErr != nil {
		return "", sentryErr
	}
	return ev.EventId, nil
}

// CaptureMessagef is similar to CaptureMessage except it is using Printf to format the args in
// to the given format string.
func (client Client) CaptureMessagef(format string, args ...interface{}) (string, error) {
	return client.CaptureMessage(fmt.Sprintf(format, args...))
}

// Capture sends the given event to Sentry.
// Fields which are left blank are populated with default values.
func (client Client) Capture(ev *Event) error {
	// Fill in defaults
	ev.Project = client.Project
	if ev.EventId == "" {
		eventId, err := uuid4()
		if err != nil {
			return err
		}
		ev.EventId = eventId
	}
	if ev.Level == "" {
		ev.Level = "error"
	}
	if ev.Logger == "" {
		ev.Logger = "root"
	}
	if ev.Timestamp == "" {
		now := time.Now().UTC()
		ev.Timestamp = now.Format(iso8601)
	}

	if len(ev.Stacktrace.Frames) == 0 {
		ev.Stacktrace = generateStacktrace()
	}

	buf, err := client.encoder.Encode(ev)
	if err != nil {
		return err
	}

	// Send
	timestamp, err := time.Parse(iso8601, ev.Timestamp)
	if err != nil {
		return err
	}

	err = client.send(buf.Bytes(), timestamp)
	if err != nil {
		return err
	}

	return nil
}

// sends a packet to the sentry server with a given timestamp
func (client Client) send(packet []byte, timestamp time.Time) (err error) {
	apiURL := *client.URL
	apiURL.Path = path.Join(apiURL.Path, "/api/"+client.Project+"/store")
	apiURL.Path += "/"
	location := apiURL.String()

	buf := bytes.NewBuffer(packet)
	req, err := http.NewRequest("POST", location, buf)
	if err != nil {
		return err
	}

	authHeader := fmt.Sprintf(xSentryAuthTemplate, timestamp.Unix(), client.PublicKey)
	req.Header.Add("X-Sentry-Auth", authHeader)
	req.Header.Add("Content-Type", "application/octet-stream")
	req.Header.Add("Connection", "close")
	req.Header.Add("Accept-Encoding", "identity")

	resp, err := client.httpClient.Do(req)

	if err != nil {
		return err
	}

	defer resp.Body.Close()

	switch resp.StatusCode {
	case 200:
		return nil
	default:
		return errors.New(resp.Status)
	}
	// should never get here
	panic("oops")
}

func uuid4() (string, error) {
	//TODO: Verify this algorithm or use an external library
	uuid := make([]byte, 16)
	n, err := rand.Read(uuid)
	if n != len(uuid) || err != nil {
		return "", err
	}
	uuid[8] = 0x80
	uuid[4] = 0x40

	return hex.EncodeToString(uuid), nil
}

func timeoutDialer(cTimeout time.Duration) func(net, addr string) (c net.Conn, err error) {
	return func(netw, addr string) (net.Conn, error) {
		conn, err := net.DialTimeout(netw, addr, cTimeout)
		if err != nil {
			return nil, err
		}
		return conn, nil
	}
}

// A custom http.Transport which allows us to put a timeout on each request.
type transport struct {
	httpTransport *http.Transport
	timeout       time.Duration
}

// Make use of Go 1.1's CancelRequest to close an outgoing connection if it
// took longer than [timeout] to get a response.
func (T *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	timer := time.AfterFunc(T.timeout, func() {
		T.httpTransport.CancelRequest(req)
	})
	defer timer.Stop()
	return T.httpTransport.RoundTrip(req)
}

type EventEncoder interface {
	Encode(*Event) (*bytes.Buffer, error)
}

type Encoder struct{}

func (encoder *Encoder) Encode(ev *Event) (buf *bytes.Buffer, err error) {
	buf = new(bytes.Buffer)
	b64Encoder := base64.NewEncoder(base64.StdEncoding, buf)
	writer := zlib.NewWriter(b64Encoder)
	jsonEncoder := json.NewEncoder(writer)

	if err = jsonEncoder.Encode(ev); err != nil {
		return
	}
	err = writer.Close()
	if err != nil {
		return
	}

	if err = b64Encoder.Close(); err != nil {
		return
	}
	return buf, nil
}
