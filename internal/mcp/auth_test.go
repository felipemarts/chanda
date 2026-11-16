package mcp

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/felipemarts/chanda/pkg/chanda"
)

const testToken = "s3cret-token"

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func authServer(t *testing.T, token string) *Server {
	t.Helper()
	cfg := testConfig("read")
	cfg.MCP.HTTPTokenEnv = "TEST_CHANDA_TOKEN"
	s, err := New(&fakeAPI{}, cfg, Options{Getenv: envOf(map[string]string{"TEST_CHANDA_TOKEN": token})})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// headerRT adds an Authorization header to every request of the SDK client.
type headerRT struct{ value string }

func (h headerRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", h.value)
	return http.DefaultTransport.RoundTrip(r)
}

// HTTP transport auth: with a token, a request without the right bearer is 401.
func TestHTTPBearerTokenRequired(t *testing.T) {
	s := authServer(t, testToken)
	if !s.Authenticated() {
		t.Fatal("token not picked up from the configured env var")
	}
	ts := httptest.NewServer(s.HTTPHandler())
	defer ts.Close()
	cases := []struct {
		name, auth string
		want       int
	}{
		{"no header", "", http.StatusUnauthorized},
		{"wrong token", "Bearer nope", http.StatusUnauthorized},
		{"token prefix", "Bearer " + testToken[:4], http.StatusUnauthorized},
		{"basic scheme", "Basic " + testToken, http.StatusUnauthorized},
		{"empty bearer", "Bearer ", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodPost, ts.URL+HTTPPath, strings.NewReader(`{}`))
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Fatalf("status %d, want %d", resp.StatusCode, tc.want)
			}
			if !strings.HasPrefix(resp.Header.Get("WWW-Authenticate"), "Bearer") {
				t.Fatalf("WWW-Authenticate %q", resp.Header.Get("WWW-Authenticate"))
			}
		})
	}

	// The right token opens an MCP session.
	for _, scheme := range []string{"Bearer ", "bearer "} {
		hc := &http.Client{Transport: headerRT{scheme + testToken}}
		cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "v0"}, nil).
			Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: ts.URL + HTTPPath, HTTPClient: hc}, nil)
		if err != nil {
			t.Fatalf("%q: %v", scheme, err)
		}
		if got := listNames(t, cs); !slices.Equal(got, toolSet(ProfileRead)) {
			t.Fatalf("tools %v", got)
		}
		cs.Close()
	}
	// Without a token the SDK client cannot connect.
	if _, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "v0"}, nil).
		Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: ts.URL + HTTPPath}, nil); err == nil {
		t.Fatal("connected without a token")
	}
}

// The SDK's DNS-rebinding protection stays on behind the auth middleware.
func TestHTTPLocalhostProtectionKept(t *testing.T) {
	for _, token := range []string{"", testToken} {
		s := authServer(t, token)
		ts := httptest.NewServer(s.HTTPHandler())
		req, _ := http.NewRequest(http.MethodPost, ts.URL+HTTPPath, strings.NewReader(`{}`))
		req.Host = "evil.example"
		req.Header.Set("Authorization", "Bearer "+testToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		ts.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("token %q: status %d, want 403", token, resp.StatusCode)
		}
	}
}

// fakeListener reports an address without binding any socket.
type fakeListener struct {
	addr   net.Addr
	closed bool
}

func (f *fakeListener) Accept() (net.Conn, error) { return nil, net.ErrClosed }
func (f *fakeListener) Close() error              { f.closed = true; return nil }
func (f *fakeListener) Addr() net.Addr            { return f.addr }

// Without a token, HTTP serves only on loopback addresses.
func TestHTTPRefusesNonLoopbackWithoutToken(t *testing.T) {
	cases := []struct {
		name  string
		addr  net.Addr
		token string
		ok    bool
	}{
		{"ipv4 loopback", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080}, "", true},
		{"ipv6 loopback", &net.TCPAddr{IP: net.IPv6loopback, Port: 8080}, "", true},
		{"all interfaces", &net.TCPAddr{IP: net.IPv4zero, Port: 8080}, "", false},
		{"ipv6 unspecified", &net.TCPAddr{IP: net.IPv6unspecified, Port: 8080}, "", false},
		{"lan address", &net.TCPAddr{IP: net.IPv4(192, 168, 1, 10), Port: 8080}, "", false},
		{"all interfaces with token", &net.TCPAddr{IP: net.IPv4zero, Port: 8080}, testToken, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := authServer(t, tc.token)
			err := s.CheckListenAddr(tc.addr)
			if (err == nil) != tc.ok {
				t.Fatalf("CheckListenAddr(%v) = %v, want ok=%v", tc.addr, err, tc.ok)
			}
			if tc.ok {
				return
			}
			if chanda.CodeOf(err) != chanda.CodePermissionDenied {
				t.Fatalf("code %v", chanda.CodeOf(err))
			}
			ln := &fakeListener{addr: tc.addr}
			if err := s.Serve(context.Background(), ln); err == nil || !ln.closed {
				t.Fatalf("Serve: err %v, closed %v", err, ln.closed)
			}
		})
	}
}
