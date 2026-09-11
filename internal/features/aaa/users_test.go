package aaa

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

func TestUsers(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       []account
		failure    bool
	}{
		{"empty", `{"openconfig-system:users":{}}`, []account{}, false},
		{"privileges", `{"openconfig-system:users":{"user":[{"username":"viewer","config":{"username":"viewer","password":"opaque-password-hash","icx-openconfig-aaa-aug:privilege":5}},{"username":"super","config":{"username":"super","icx-openconfig-aaa-aug:privilege":0}}]}}`, []account{{Username: "super", Privilege: 0}, {Username: "viewer", Privilege: 5}}, false},
		{"missing root", `{}`, nil, true},
		{"missing privilege", `{"openconfig-system:users":{"user":[{"username":"viewer","config":{"username":"viewer"}}]}}`, nil, true},
		{"identity mismatch", `{"openconfig-system:users":{"user":[{"username":"viewer","config":{"username":"super","icx-openconfig-aaa-aug:privilege":0}}]}}`, nil, true},
		{"duplicate", `{"openconfig-system:users":{"user":[{"username":"viewer","config":{"username":"viewer","icx-openconfig-aaa-aug:privilege":5}},{"username":"viewer","config":{"username":"viewer","icx-openconfig-aaa-aug:privilege":0}}]}}`, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/restconf/data/system/aaa/authentication/users" {
					http.NotFound(w, r)
					return
				}
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			d, err := fastiron.New(fastiron.Config{Host: server.URL, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.URL + "/restconf/data", InsecureSkipVerify: true, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}
			got, err := readUsers(context.Background(), d)
			if (err != nil) != tc.failure || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("users=%v error=%v", got, err)
			}
		})
	}
}
