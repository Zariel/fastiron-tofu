package restconf

import (
	"context"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testClient(t *testing.T, s *httptest.Server) *Client {
	t.Helper()
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.Certificate().Raw})
	c, err := New(Config{URL: s.URL + "/restconf/data", Username: "automation", Password: "credential-marker", CA: string(ca), Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestMethods(t *testing.T) {
	for _, method := range []string{"GET", "POST", "PATCH", "PUT", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != method || r.RequestURI != "/restconf/data/interfaces/interface=ethernet%201%2F2%2F3" {
					t.Errorf("request: %s %s", r.Method, r.RequestURI)
				}
				user, pass, ok := r.BasicAuth()
				if !ok || user != "automation" || pass != "credential-marker" {
					t.Error("missing authentication")
				}
				if r.Header.Get("Accept") != "application/yang-data+json" {
					t.Error("missing media type")
				}
				b, _ := io.ReadAll(r.Body)
				if method == "PATCH" || method == "PUT" || method == "POST" {
					if string(b) != `{"enabled":false}` {
						t.Errorf("body %s", b)
					}
				} else if len(b) != 0 {
					t.Error("unexpected body")
				}
				fmt.Fprint(w, `{"enabled":true}`)
			}))
			defer s.Close()
			var body any
			if method == "PATCH" || method == "PUT" || method == "POST" {
				body = map[string]bool{"enabled": false}
			}
			var got struct{ Enabled bool }
			err := testClient(t, s).Do(context.Background(), method, "/interfaces/interface=ethernet%201%2F2%2F3", body, &got)
			if err != nil {
				t.Fatal(err)
			}
			if !got.Enabled {
				t.Fatal("incorrect response")
			}
		})
	}
}

func TestErrors(t *testing.T) {
	for _, status := range []int{301, 401, 403, 404, 409, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "/secret-marker")
				w.WriteHeader(status)
				fmt.Fprint(w, `{"password":"secret-marker"}`)
			}))
			defer s.Close()
			err := testClient(t, s).Do(context.Background(), "GET", "/system/aaa", nil, nil)
			var httpErr *HTTPError
			if !errors.As(err, &httpErr) || httpErr.Status != status {
				t.Fatalf("error: %v", err)
			}
			if errors.Is(err, ErrNotFound) != (status == 404) {
				t.Fatal("incorrect absence classification")
			}
			if strings.Contains(err.Error(), "marker") {
				t.Fatal("secret leaked")
			}
		})
	}
}

func TestTrustAndCancellation(t *testing.T) {
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{}`) }))
	defer s.Close()
	c, err := New(Config{URL: s.URL, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Do(context.Background(), "GET", "/", nil, nil); err == nil {
		t.Fatal("accepted untrusted certificate")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = testClient(t, s).Do(ctx, "PATCH", "/vlans", map[string]string{"name": "test"}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestInvalidResponse(t *testing.T) {
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `secret-marker not json`) }))
	defer s.Close()
	var result map[string]any
	err := testClient(t, s).Do(context.Background(), "GET", "/system", nil, &result)
	if err == nil || strings.Contains(err.Error(), "secret-marker") {
		t.Fatalf("unsafe error: %v", err)
	}
}
