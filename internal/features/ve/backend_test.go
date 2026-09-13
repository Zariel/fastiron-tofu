package ve

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

func TestEmptyInterfaceDatabase(t *testing.T) {
	// A RESTCONF restart returned these shapes despite native physical and
	// logical interfaces still existing. They cannot establish resource absence.
	for _, body := range []string{
		`{"openconfig-interfaces:interfaces":{}}`,
		`{"openconfig-interfaces:interfaces":{"interface":[]}}`,
	} {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, body)
		}))
		t.Cleanup(server.Close)
		device, err := fastiron.New(fastiron.Config{Host: server.URL, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.URL, InsecureSkipVerify: true, Timeout: time.Second}})
		if err != nil {
			t.Fatal(err)
		}

		if _, err := Read(context.Background(), device, 53); err == nil || errors.Is(err, fastiron.ErrNotFound) {
			t.Fatalf("incomplete database reported as confirmed state: %v", err)
		}
	}
}

func TestReadIdentity(t *testing.T) {
	for _, id := range []int64{1, 4095, 0, 4096} {
		t.Run(fmt.Sprint(id), func(t *testing.T) {
			valid := id == 1 || id == 4095
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !valid {
					t.Error("invalid VE identity reached the switch")
				}
				fmt.Fprintf(w, `{"openconfig-interfaces:interfaces":{"interface":[{"name":"ve %d","config":{"name":"ve %d","type":"iana-if-type:l3ipvlan","description":"EDGE"},"openconfig-vlan:routed-vlan":{"config":{"vlan":%d}}}]}}`, id, id, id)
			}))
			t.Cleanup(server.Close)
			device, err := fastiron.New(fastiron.Config{Host: server.URL, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.URL, InsecureSkipVerify: true, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}
			observed, err := Read(context.Background(), device, id)
			if !valid {
				if err == nil {
					t.Fatal("accepted invalid VE identity")
				}
				return
			}
			if err != nil || observed.ID != id || observed.VLANID != id || observed.PortName != "EDGE" {
				t.Fatalf("VE=%+v error=%v", observed, err)
			}
		})
	}
}
