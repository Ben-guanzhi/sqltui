package oobe

import "testing"

// Bare remote hosts default to https, loopback stays plain-http, and an
// explicit scheme in the host field overrides either. TLS verification is
// never disabled, and only the short JSON client carries a deadline.
func TestNewHTTPClientBase(t *testing.T) {
	cases := []struct{ host, want string }{
		{"obs.local", "https://obs.local:5080"},
		{"https://obs.local", "https://obs.local:5080"},
		{"http://obs.local", "http://obs.local:5080"},
		{"HTTPS://obs.local", "https://obs.local:5080"},
		{"localhost", "http://localhost:5080"},
		{"127.0.0.1", "http://127.0.0.1:5080"},
		{"[::1]", "http://[::1]:5080"},
	}
	for _, c := range cases {
		got := newHTTPClient(c.host, "5080", "u", "p")
		if got.base != c.want {
			t.Errorf("newHTTPClient(%q).base = %q, want %q", c.host, got.base, c.want)
		}
		if got.hc.Transport != nil {
			t.Errorf("newHTTPClient(%q) must keep the default (verifying) transport", c.host)
		}
		if got.hc.Timeout != requestTimeout {
			t.Errorf("newHTTPClient(%q).hc.Timeout = %v, want %v", c.host, got.hc.Timeout, requestTimeout)
		}
		if got.sse.Timeout != 0 {
			t.Errorf("newHTTPClient(%q).sse.Timeout = %v, want 0 (an SSE stream runs until [[DONE]])", c.host, got.sse.Timeout)
		}
	}
}
