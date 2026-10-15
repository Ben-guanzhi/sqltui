package oobe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type httpClient struct {
	base string
	auth string
	// hc serves the short JSON round-trips and carries a deadline, so a wedged
	// server cannot hang the UI. sse has none: a search streams until the
	// server sends [[DONE]], and a client deadline would cut long searches off
	// mid-result-set.
	hc  *http.Client
	sse *http.Client
}

// requestTimeout bounds one JSON round-trip against OpenObserve.
const requestTimeout = 30 * time.Second

// defaultScheme picks the transport for a host that did not carry one. Remote
// hosts go over https; loopback stays on plain http, because OpenObserve's
// quick-start has no TLS and pointing it at https would break the common local
// case. Anything with an explicit scheme in the host field overrides this.
func defaultScheme(host string) string {
	switch strings.ToLower(strings.Trim(host, "[]")) {
	case "localhost", "127.0.0.1", "::1":
		return "http"
	default:
		return "https"
	}
}

// newHTTPClient builds the HTTP client for an OpenObserve server. The host
// field may carry an explicit scheme ("http://localhost", "https://obs.example.com").
// TLS certificate verification is always on: the previous unconditional
// InsecureSkipVerify was dead config (the base URL was hardcoded to http) and
// would have silently exposed Basic credentials to a MITM once https was used.
func newHTTPClient(host, port, user, pass string) *httpClient {
	scheme := defaultScheme(host)
	switch lower := strings.ToLower(host); {
	case strings.HasPrefix(lower, "https://"):
		scheme = "https"
		host = host[8:]
	case strings.HasPrefix(lower, "http://"):
		scheme = "http"
		host = host[7:]
	}
	return &httpClient{
		base: scheme + "://" + host + ":" + port,
		auth: "Basic " + basicAuth(user, pass),
		hc:   &http.Client{Timeout: requestTimeout},
		sse:  &http.Client{},
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
	resp, err := c.sse.Do(req)
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
