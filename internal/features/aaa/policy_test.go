package aaa

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

func TestAAAPolicyIncomplete(t *testing.T) {
	for name, body := range map[string]string{
		"missing login": `{"openconfig-system:aaa":{"authentication":{"icx-openconfig-aaa-aug:dot1x":{"default":"none"}},"authorization":{"icx-openconfig-aaa-aug:coa":{"enable":false,"ignore":{"disable-port":false,"dm-request":false,"flip-port":false,"modify-acl":false,"reauth-host":false}}}}}`,
		"null ignore":   `{"openconfig-system:aaa":{"authentication":{"icx-openconfig-aaa-aug:login":{"default":["local"]},"icx-openconfig-aaa-aug:dot1x":{"default":"none"}},"authorization":{"icx-openconfig-aaa-aug:coa":{"enable":false,"ignore":{"disable-port":null,"dm-request":false,"flip-port":false,"modify-acl":false,"reauth-host":false}}}}}`,

		"missing root":           `{}`,
		"missing containers":     `{"openconfig-system:aaa":{}}`,
		"missing settings":       `{"openconfig-system:aaa":{"authentication":{},"authorization":{}}}`,
		"missing ignore actions": `{"openconfig-system:aaa":{"authentication":{"icx-openconfig-aaa-aug:login":{"default":["local"]},"icx-openconfig-aaa-aug:dot1x":{"default":"none"}},"authorization":{"icx-openconfig-aaa-aug:coa":{"enable":false,"ignore":{"dm-request":false}}}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/restconf/data/system/aaa" {
					http.NotFound(w, r)
					return
				}
				fmt.Fprint(w, body)
			}))
			defer server.Close()
			d, err := fastiron.New(fastiron.Config{Host: server.URL, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.URL + "/restconf/data", InsecureSkipVerify: true, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}

			if policy, err := readPolicy(context.Background(), d); err == nil || policy != nil {
				t.Fatalf("incomplete policy was accepted: %v, %v", policy, err)
			}
		})
	}
}

func TestAAAPolicyWithoutDot1X(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"openconfig-system:aaa":{"authentication":{"icx-openconfig-aaa-aug:login":{"default":["local"]}},"authorization":{"icx-openconfig-aaa-aug:coa":{"enable":false,"ignore":{"disable-port":false,"dm-request":false,"flip-port":false,"modify-acl":false,"reauth-host":false}}}}}`)
	}))
	defer server.Close()
	d, err := fastiron.New(fastiron.Config{Host: server.URL, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.URL + "/restconf/data", InsecureSkipVerify: true, Timeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}

	policy, err := readPolicy(context.Background(), d)
	if err != nil || policy == nil {
		t.Fatalf("deleted dot1x policy was not readable: %v", err)
	}
	if policy.Dot1XDefault != "" {
		t.Fatal("absent dot1x policy was reported as explicitly configured")
	}
}
