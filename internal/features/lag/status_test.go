package lag

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestLAGStatus(t *testing.T) {
	captured, err := os.ReadFile("testdata/interface-status.json")
	if err != nil {
		t.Fatal(err)
	}
	device := lagDevice(t, "", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/interfaces/interface=lag 44" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(405)
			return
		}
		w.Write(captured)
	}, time.Second)
	got, err := readStatus(context.Background(), device, "lag 44")
	if err != nil {
		t.Fatal(err)
	}
	if got.IfIndex == nil || *got.IfIndex != 3116 || got.AdminStatus == nil || *got.AdminStatus != "UP" || got.OperStatus == nil || *got.OperStatus != "DOWN" {
		t.Fatalf("captured LAG status: %+v", got)
	}
	if len(got.Counters) != 16 {
		t.Fatalf("counters: %v", got.Counters)
	}
	for name, value := range got.Counters {
		if value != 0 {
			t.Errorf("%s=%d; want 0", name, value)
		}
	}
}

func TestLAGStatusValidation(t *testing.T) {
	const entry = `{"name":"lag 5","config":{"name":"lag 5","type":"iana-if-type:ieee8023adLag"},"state":%s}`
	for _, tc := range []struct {
		name, state string
		valid       bool
	}{
		{"omitted", `null`, true},
		{"empty counters", `{"counters":{}}`, true},
		{"large counters", `{"counters":{"in-octets":"18446744073709551615","out-octets":9007199254740993}}`, true},
		{"wrong identity", `{"name":"lag 6"}`, false},
		{"negative counter", `{"counters":{"in-octets":"-1"}}`, false},
		{"overflow", `{"counters":{"in-octets":"18446744073709551616"}}`, false},
		{"fraction", `{"counters":{"in-octets":1.5}}`, false},
		{"missing counter value", `{"counters":{"in-octets":null}}`, false},
		{"invalid index", `{"ifindex":-1}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			device := lagDevice(t, "", func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"openconfig-interfaces:interface":[%s]}`, fmt.Sprintf(entry, tc.state))
			}, time.Second)
			got, err := readStatus(context.Background(), device, "lag 5")
			if (err == nil) != tc.valid {
				t.Fatalf("status=%+v error=%v", got, err)
			}
			if tc.name == "large counters" && (got.Counters["in-octets"] != 18446744073709551615 || got.Counters["out-octets"] != 9007199254740993) {
				t.Fatalf("counter precision lost: %v", got.Counters)
			}
			if tc.name == "omitted" && (got.Counters != nil || got.IfIndex != nil || got.AdminStatus != nil || got.OperStatus != nil) {
				t.Fatalf("invented omitted status: %+v", got)
			}
			if tc.name == "empty counters" && got.Counters == nil {
				t.Fatal("empty counters became absent")
			}
		})
	}

	for _, tc := range []struct{ name, raw string }{
		{"missing container", `{}`},
		{"empty collection", `{"openconfig-interfaces:interface":[]}`},
		{"multiple interfaces", `{"openconfig-interfaces:interface":[{},{}]}`},
		{"missing identity", `{"openconfig-interfaces:interface":[{"name":"lag 5"}]}`},
		{"wrong kind", `{"openconfig-interfaces:interface":[{"name":"lag 5","config":{"name":"lag 5","type":"iana-if-type:ethernetCsmacd"}}]}`},
		{"wrong interface", `{"openconfig-interfaces:interface":[{"name":"lag 6","config":{"name":"lag 6","type":"iana-if-type:ieee8023adLag"}}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			device := lagDevice(t, "", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.raw) }, time.Second)
			if _, err := readStatus(context.Background(), device, "lag 5"); err == nil {
				t.Fatal("accepted invalid interface response")
			}
		})
	}
}
