package restconf

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxResponse = 8 << 20

var ErrNotFound = errors.New("RESTCONF object not found")

type HTTPError struct {
	Status  int
	missing bool
}

func (e *HTTPError) Error() string { return fmt.Sprintf("RESTCONF returned HTTP %d", e.Status) }
func (e *HTTPError) Is(target error) bool {
	return target == ErrNotFound && (e.Status == http.StatusNotFound || e.missing)
}

type Config struct {
	URL, Username, Password string
	CA, Certificate, Key    string
	InsecureSkipVerify      bool
	Timeout                 time.Duration
}

type Client struct {
	base               *url.URL
	http               *http.Client
	username, password string
}

func New(cfg Config) (*Client, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("RESTCONF URL must be an HTTPS URL without credentials, query, or fragment")
	}
	if cfg.Timeout <= 0 {
		return nil, errors.New("RESTCONF timeout must be positive")
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: cfg.InsecureSkipVerify}
	if cfg.CA != "" {
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM([]byte(cfg.CA)) {
			return nil, errors.New("invalid RESTCONF CA certificate")
		}
		tlsConfig.RootCAs = roots
	}
	if cfg.Certificate != "" || cfg.Key != "" {
		cert, err := tls.X509KeyPair([]byte(cfg.Certificate), []byte(cfg.Key))
		if err != nil {
			return nil, errors.New("invalid RESTCONF client certificate/key pair")
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	// Redirects can move credentials or turn writes into reads. Never follow them.
	client := &http.Client{Transport: transport, Timeout: cfg.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &Client{base: u, http: client, username: cfg.Username, password: cfg.Password}, nil
}

// Do sends one request. Mutation outcomes are resolved by the resource's read-back;
// the transport never retries a write or switches protocols.
func (c *Client) Do(ctx context.Context, method, path string, body, result any) error {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete:
	default:
		return errors.New("unsupported RESTCONF method")
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "?#\\\r\n") {
		return errors.New("invalid RESTCONF path")
	}
	decoded, err := url.PathUnescape(path)
	if err != nil {
		return errors.New("invalid RESTCONF path encoding")
	}
	for _, part := range strings.Split(decoded, "/") {
		if part == ".." || part == "." {
			return errors.New("invalid RESTCONF path traversal")
		}
	}
	var data []byte
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return errors.New("cannot encode RESTCONF request")
		}
	}
	u := *c.base
	u.Path = strings.TrimRight(c.base.Path, "/") + decoded
	u.RawPath = strings.TrimRight(c.base.EscapedPath(), "/") + path
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(data))
	if err != nil {
		return errors.New("cannot construct RESTCONF request")
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Accept", "application/yang-data+json")
	if body != nil {
		req.Header.Set("Content-Type", "application/yang-data+json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// HTTP errors can contain URLs or server-supplied text. Do not expose them.
		return errors.New("RESTCONF request failed; check connectivity, TLS trust, and timeout")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		failure := &HTTPError{Status: resp.StatusCode}
		if resp.StatusCode == http.StatusBadRequest {
			// FastIron reports an absent list instance as HTTP 400 with protocol
			// error 388. Other validation errors must never become absence.
			var details struct {
				Errors struct {
					Error []struct {
						Tag    string `json:"error-tag"`
						AppTag string `json:"error-app-tag"`
						Info   struct {
							Number int `json:"error-number"`
						} `json:"error-info"`
					} `json:"error"`
				} `json:"ietf-restconf:errors"`
			}
			raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
			if readErr == nil && json.Unmarshal(raw, &details) == nil && len(details.Errors.Error) == 1 {
				e := details.Errors.Error[0]
				failure.missing = e.Tag == "invalid-value" && e.AppTag == "data-invalid" && e.Info.Number == 388
			}
		}
		return failure
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return errors.New("cannot read RESTCONF response")
	}
	if len(raw) > maxResponse {
		return errors.New("RESTCONF response exceeds size limit")
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(raw, result); err != nil {
		return errors.New("invalid RESTCONF JSON response")
	}
	return nil
}
