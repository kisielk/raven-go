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
	"net/http"
	"net/url"
	"path"
	"time"
)

type RavenClient struct {
	URL        *url.URL
	PublicKey  string
	SecretKey  string
	Project    string
	httpClient *http.Client
}

type sentryRequest struct {
	EventId   string `json:"event_id"`
	Project   string `json:"project"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Logger    string `json:"logger"`
}

type sentryResponse struct {
	ResultId string `json:"result_id"`
}

const headerTemplate = "Sentry sentry_version=2.0, sentry_client=raven-go/0.1, sentry_timestamp=%v, sentry_key=%v"

const iso8601 = "2006-01-02T15:04:05"

func NewRavenClient(dsn string) (client *RavenClient, err error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, err
	}

	basePath := path.Dir(u.Path)
	project := path.Base(u.Path)
	publicKey := u.User.Username()
	secretKey, _ := u.User.Password()
	u.Path = basePath

	check := func(req *http.Request, via []*http.Request) error {
		fmt.Printf("%+v", req)
		return nil
	}

	httpClient := &http.Client{nil, check, nil}
	return &RavenClient{URL: u, PublicKey: publicKey, SecretKey: secretKey, httpClient: httpClient, Project: project}, nil
}

func (client RavenClient) CaptureMessage(message string) (result string, err error) {
	eventId, err := uuid4()
	if err != nil {
		return "", err
	}
	timestamp := time.Now().UTC()
	timestampStr := timestamp.Format(iso8601)

	packet := sentryRequest{
		EventId:   eventId,
		Project:   client.Project,
		Message:   message,
		Timestamp: timestampStr,
		Level:     "error",
		Logger:    "root",
	}

	buf := new(bytes.Buffer)
	b64Encoder := base64.NewEncoder(base64.StdEncoding, buf)
	writer := zlib.NewWriter(b64Encoder)
	jsonEncoder := json.NewEncoder(writer)

	if err := jsonEncoder.Encode(packet); err != nil {
		return "", err
	}

	err = writer.Close()
	if err != nil {
		return "", err
	}

	err = b64Encoder.Close()
	if err != nil {
		return "", err
	}

	resp, err := client.send(buf.Bytes(), timestamp)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return "", errors.New(resp.Status)
	}

	return eventId, nil
}

func (client RavenClient) send(packet []byte, timestamp time.Time) (response *http.Response, err error) {
	apiURL := *client.URL
	apiURL.Path = path.Join(apiURL.Path, "/api/"+client.Project+"/store/")
	apiURL.User = nil
	location := apiURL.String()

	for {
		buf := bytes.NewBuffer(packet)
		req, err := http.NewRequest("POST", location, buf)
		if err != nil {
			return nil, err
		}

		authHeader := fmt.Sprintf(headerTemplate, timestamp.Unix(), client.PublicKey)
		req.Header.Add("X-Sentry-Auth", authHeader)
		req.Header.Add("Content-Type", "application/octet-stream")
		req.Header.Add("Connection", "close")
		req.Header.Add("Accept-Encoding", "identity")

		resp, err := client.httpClient.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == 301 {
			location = resp.Header["Location"][0]
		} else {
			return resp, nil
		}
	}
	return nil, nil
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
