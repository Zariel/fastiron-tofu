package ethernet

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/testswitch"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

func TestDiscovery(t *testing.T) {
	var mu sync.Mutex
	server := testswitch.New(t, func(command string) string {
		t.Errorf("REST query issued CLI command %q", command)
		return "% Invalid input"
	})
	body := `{"openconfig-interfaces:interfaces":{"interface":[
{"name":"ve 53","config":{"name":"ve 53"}},
{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/12","description":"STALE","enabled":true},
 "state":{"description":"PHONE","enabled":false,"name":"ethernet 1/1/12","ifindex":12,"admin-status":"DOWN","oper-status":"DOWN","counters":{"in-octets":"18446744073709551615","out-octets":"9007199254740993","in-errors":0}},
 "openconfig-if-ethernet:ethernet":{"config":{"auto-negotiate":false,"duplex-mode":"FULL","port-speed":"openconfig-if-ethernet:SPEED_100MB","icx-openconfig-if-ethernet-aug:ethernet-clock":"none"},
 "state":{"auto-negotiate":true,"duplex-mode":"HALF","negotiated-duplex-mode":"FULL","negotiated-port-speed":"openconfig-if-ethernet:SPEED_UNKNOWN","icx-openconfig-if-ethernet-aug:negotiated-clock":"none"}}},
{"name":"ethernet 2/1/1","config":{"name":"ethernet 2/1/1","description":"","enabled":true},"state":{"description":"","enabled":true}}
]}}`
	server.HandleFunc("/interfaces", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method != http.MethodGet {
			t.Errorf("discovery issued mutation %s", r.Method)
			w.WriteHeader(405)
			return
		}
		fmt.Fprint(w, body)
	})
	device, err := fastiron.New(fastiron.Config{Host: "switch", Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}

	ports, err := readObservations(context.Background(), device)
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 2 {
		t.Fatalf("ports=%+v", ports)
	}
	phone := ports[0]
	if phone.config != (config{Port: "1/1/12", PortName: "PHONE", Enabled: false}) {
		t.Fatalf("configuration=%+v", phone.config)
	}
	if phone.IfIndex == nil || *phone.IfIndex != 12 || phone.AdminStatus == nil || *phone.AdminStatus != "DOWN" || phone.OperStatus == nil || *phone.OperStatus != "DOWN" {
		t.Fatalf("operational state=%+v", phone)
	}
	wantCounters := map[string]uint64{"in-octets": 18446744073709551615, "out-octets": 9007199254740993, "in-errors": 0}
	if !reflect.DeepEqual(phone.Counters, wantCounters) {
		t.Fatalf("counter precision lost: %+v", phone.Counters)
	}
	link := phone.Link
	if link == nil || link.AutoNegotiate == nil || *link.AutoNegotiate || link.ReportedAutoNegotiate == nil || !*link.ReportedAutoNegotiate {
		t.Fatalf("configured/reported negotiation conflated: %+v", link)
	}
	if link.Duplex == nil || *link.Duplex != "FULL" || link.ReportedDuplex == nil || *link.ReportedDuplex != "HALF" || link.NegotiatedDuplex == nil || *link.NegotiatedDuplex != "FULL" {
		t.Fatalf("configured/reported/negotiated duplex conflated: %+v", link)
	}
	if link.Speed == nil || *link.Speed != "openconfig-if-ethernet:SPEED_100MB" || link.NegotiatedSpeed == nil || *link.NegotiatedSpeed != "openconfig-if-ethernet:SPEED_UNKNOWN" {
		t.Fatalf("configured and negotiated speed conflated: %+v", link)
	}
	if link.Clock == nil || *link.Clock != "none" || link.NegotiatedClock == nil || *link.NegotiatedClock != "none" {
		t.Fatalf("clock state=%+v", link)
	}
	unknown := ports[1]
	if unknown.Port != "2/1/1" || unknown.IfIndex != nil || unknown.AdminStatus != nil || unknown.OperStatus != nil || unknown.Counters != nil || unknown.Link != nil {
		t.Fatalf("omitted state fabricated values: %+v", unknown)
	}

	for _, tc := range []struct{ name, entries string }{
		{"empty inventory", `[]`},
		{"no physical ports", `[{"name":"ve 53"}]`},
		{"noncanonical identity", `[{"name":"ethernet 01/1/12"}]`},
		{"missing configuration", `[{"name":"ethernet 1/1/12"}]`},
		{"mismatched configuration", `[{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/13","description":"","enabled":true}}]`},
		{"missing enabled", `[{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/12","description":""}}]`},
		{"duplicate identity", `[{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/12","description":"","enabled":true}},{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/12","description":"","enabled":true}}]`},
		{"negative counter", `[{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/12","description":"","enabled":true},"state":{"description":"","enabled":true,"counters":{"in-octets":"-1"}}}]`},
		{"overflow counter", `[{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/12","description":"","enabled":true},"state":{"description":"","enabled":true,"counters":{"in-octets":"18446744073709551616"}}}]`},
		{"fractional counter", `[{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/12","description":"","enabled":true},"state":{"description":"","enabled":true,"counters":{"in-octets":"1.5"}}}]`},
		{"mismatched operational identity", `[{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/12","description":"","enabled":true},"state":{"description":"PHONE","enabled":false,"name":"ethernet 1/1/13"}}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mu.Lock()
			body = `{"openconfig-interfaces:interfaces":{"interface":` + tc.entries + `}}`
			mu.Unlock()
			if _, err := readObservations(context.Background(), device); err == nil {
				t.Fatal("accepted ambiguous or invalid inventory")
			}
		})
	}
}
