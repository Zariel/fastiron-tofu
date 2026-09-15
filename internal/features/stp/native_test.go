package stp

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/testswitch"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
	"github.com/zariel/fastiron-tofu/internal/transport/ssh"
)

func TestNativeInterfaces(t *testing.T) {
	server := testswitch.New(t, func(command string) string {
		switch command {
		case "skip-page-display":
			return ""
		case "show running-config":
			return "ver 09.0.10k\ninterface ethernet 1/1/11\n stp-bpdu-guard\ninterface ethernet 1/1/13\n spanning-tree 802-1w admin-edge-port\nend"
		default:
			t.Errorf("unexpected command %q", command)
			return "% Invalid input"
		}
	})
	server.HandleFunc("/stp/interfaces", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected mutation %s", r.Method)
		}
		fmt.Fprint(w, `{"openconfig-spanning-tree:interfaces":{"interface":[{"name":"ethernet 1/1/11","config":{"name":"ethernet 1/1/11"}},{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/12","guard":"ROOT","bpdu-guard":true}}]}}`)
	})
	device, err := fastiron.New(fastiron.Config{Host: server.SSHAddress, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: time.Second}, SSH: &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := readInterfaces(context.Background(), device)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interfaceConfig{"ethernet 1/1/11": {BPDUGuard: true}, "ethernet 1/1/12": {}, "ethernet 1/1/13": {AdminEdge: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("flags=%v; want native flags %v", got, want)
	}
}
