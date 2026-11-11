package oobe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type httpClient struct {
	base string
	auth string
	// hc serves the short JSON round-trips and carries a deadline, so a wedged
	// server cannot hang the UI. sse has no whole-request deadline: a search
	// streams until the server sends [[DONE]], and a client deadline would cut
	// long searches off mid-result-set. It still gets an explicit Transport so
	// dial, TLS handshake and idle keep-alive sockets are individually bounded
	// and can be released by Backend.Close.
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
		// sse 不设整请求 Timeout（长搜索必须流到 [[DONE]]），但给一个显式
		// Transport：拨号、TLS 握手、空闲连接各自有界，避免"没有 Timeout"
		// 被误读成任何一步都能无限期挂住，同时让 Backend.Close 能关掉它
		// 自己的空闲 keep-alive 池。
		sse: &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   requestTimeout,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   10,
				IdleConnTimeout:       requestTimeout,
				TLSHandshakeTimeout:   requestTimeout,
				ExpectContinueTimeout: 1 * time.Second,
			},
		},
	}
}

// closeIdle releases the idle keep-alive sockets of both clients. It is the
// inverse of the connections the ping and the searches opened; active
// requests are left alone (their bodies are owned by the caller).
func (c *httpClient) closeIdle() {
	if c.hc != nil {
		c.hc.CloseIdleConnections()
	}
	if c.sse != nil {
		c.sse.CloseIdleConnections()
	}
}

func (c *httpClient) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
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

func (c *httpClient) postSSE(ctx context.Context, path string, body any) (io.ReadCloser, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	// NewRequestWithContext: cancelling the request ctx closes the stream —
	// the only lever a deadline-less SSE read has.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(b))
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
