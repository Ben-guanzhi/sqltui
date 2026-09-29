package oobe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type httpClient struct {
	base string
	auth string
	hc   *http.Client
}

// newHTTPClient builds the HTTP client for an OpenObserve server. The host
// field may carry an explicit scheme ("https://obs.example.com"); bare hosts
// keep the historical plain-http default. TLS certificate verification is
// always on: the previous unconditional InsecureSkipVerify was dead config
// (the base URL was hardcoded to http) and would have silently exposed Basic
// credentials to a MITM once https was used.
func newHTTPClient(host, port, user, pass string) *httpClient {
	scheme := "http"
	switch lower := strings.ToLower(host); {
	case strings.HasPrefix(lower, "https://"):
		scheme = "https"
		host = host[8:]
	case strings.HasPrefix(lower, "http://"):
		host = host[7:]
	}
	return &httpClient{
		base: scheme + "://" + host + ":" + port,
		auth: "Basic " + basicAuth(user, pass),
		hc:   &http.Client{},
	}
}

func (c *httpClient) getJSON(path string, out any) error {
	req, err := http.NewRequest(http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.auth)
	req.Header.Set("Accept", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("openobserve: HTTP %d: %s", resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *httpClient) postSSE(path string, body any) (io.ReadCloser, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.base+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.auth)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		body2, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("openobserve: HTTP %d: %s", resp.StatusCode, string(body2))
	}
	return resp.Body, nil
}
