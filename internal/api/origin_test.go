package api

import (
	"net/http"
	"testing"
)

func TestCheckOrigin(t *testing.T) {
	s := &Server{allowedOrigins: normalizeOrigins([]string{"https://archivist.example.com", "trusted.local:9000"})}

	cases := []struct {
		name   string
		host   string
		origin string
		want   bool
	}{
		{
			// curl and other non-browser clients send no Origin header, and
			// are not subject to the attack this guards against.
			name: "no origin header is allowed", host: "archivist.local:8080", origin: "", want: true,
		},
		{name: "same origin", host: "archivist.local:8080", origin: "http://archivist.local:8080", want: true},
		{name: "same origin over https", host: "archivist.local:8080", origin: "https://archivist.local:8080", want: true},
		{name: "case insensitive host", host: "Archivist.Local:8080", origin: "http://archivist.local:8080", want: true},
		{
			// The attack: a page on another site opening the progress socket.
			name: "cross origin is rejected", host: "archivist.local:8080", origin: "http://evil.example.com", want: false,
		},
		{name: "different port is rejected", host: "archivist.local:8080", origin: "http://archivist.local:9999", want: false},
		{name: "configured origin allowed", host: "internal:8080", origin: "https://archivist.example.com", want: true},
		{name: "configured host:port allowed", host: "internal:8080", origin: "http://trusted.local:9000", want: true},
		{name: "malformed origin is rejected", host: "archivist.local:8080", origin: "://not a url", want: false},
		{name: "null origin is rejected", host: "archivist.local:8080", origin: "null", want: false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := http.NewRequest(http.MethodGet, "http://"+c.host+"/api/v1/ws/progress", nil)
			if err != nil {
				t.Fatal(err)
			}
			r.Host = c.host
			if c.origin != "" {
				r.Header.Set("Origin", c.origin)
			}

			if got := s.checkOrigin(r); got != c.want {
				t.Errorf("checkOrigin(host=%s, origin=%s) = %v, want %v", c.host, c.origin, got, c.want)
			}
		})
	}
}

func TestNormalizeOrigins(t *testing.T) {
	got := normalizeOrigins([]string{"https://Example.COM", " host:8080 ", "", "   "})
	want := map[string]bool{"example.com": true, "host:8080": true}

	if len(got) != len(want) {
		t.Fatalf("normalizeOrigins = %v, want %v", got, want)
	}
	for host := range want {
		if !got[host] {
			t.Errorf("missing %q in %v", host, got)
		}
	}
}
