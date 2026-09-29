package oobe

import "testing"

// The host field may carry an explicit scheme; bare hosts keep the plain-http
// default and TLS verification is never disabled.
func TestNewHTTPClientBase(t *testing.T) {
	cases := []struct{ host, want string }{
		{"obs.local", "http://obs.local:5080"},
		{"https://obs.local", "https://obs.local:5080"},
		{"http://obs.local", "http://obs.local:5080"},
		{"HTTPS://obs.local", "https://obs.local:5080"},
	}
	for _, c := range cases {
		got := newHTTPClient(c.host, "5080", "u", "p")
		if got.base != c.want {
			t.Errorf("newHTTPClient(%q).base = %q, want %q", c.host, got.base, c.want)
		}
		if got.hc.Transport != nil {
			t.Errorf("newHTTPClient(%q) must keep the default (verifying) transport", c.host)
		}
	}
}
