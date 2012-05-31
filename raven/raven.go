package raven

import (
	"bytes"
	"crypto/rand"
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

	httpClient := &http.Client{}
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
	encoder := json.NewEncoder(buf)
	if err := encoder.Encode(packet); err != nil {
		return "", err
	}

	resp, err := client.send(buf, timestamp)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return "", errors.New(resp.Status)
	}

	return eventId, nil
}

func (client RavenClient) send(packet *bytes.Buffer, timestamp time.Time) (response *http.Response, err error) {
	apiURL := *client.URL
	apiURL.Path = path.Join(apiURL.Path, "/api/" + client.Project + "/store")
	apiURL.User = nil
	req, err := http.NewRequest("POST", apiURL.String(), packet)
	authHeader := fmt.Sprintf(headerTemplate, timestamp.Unix(), client.PublicKey)
	req.Header.Add("X-Sentry-Auth", authHeader)

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
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
