package poe

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/testswitch"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
	"github.com/zariel/fastiron-tofu/internal/transport/ssh"
)

func TestInventory(t *testing.T) {
	server := testswitch.New(t, func(command string) string {
		switch command {
		case "skip-page-display":
			return ""
		case "show running-config":
			return "ver 09.0.10k\ninterface ethernet 1/1/11\n no inline power\ninterface ethernet 1/1/12\n inline power power-by-class 2\ninterface ethernet 1/1/13\n inline power priority 1 power-limit 18000\nend"
		default:
			t.Errorf("unexpected command %q", command)
			return "% Invalid input"
		}
	})
	server.HandleFunc("/interfaces", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method %s", r.Method)
		}
		fmt.Fprint(w, `{"openconfig-interfaces:interfaces":{"interface":[
   {"name":"ethernet 1/1/11","openconfig-if-ethernet:ethernet":{"icx-openconfig-if-poe-aug:poe":{"config":{"enabled":true},"state":{"power-allocated":"0.0"}}}},
   {"name":"ethernet 1/1/12","openconfig-if-ethernet:ethernet":{"icx-openconfig-if-poe-aug:poe":{"config":{"enabled":false},"state":{"power-class":4,"power-used":"7000.0","power-allocated":"18000.5"}}}},
   {"name":"ethernet 1/1/13","openconfig-if-ethernet:ethernet":{"icx-openconfig-if-poe-aug:poe":{"config":{"enabled":false,"priority":3,"power-by-class":4,"power-limit":12000}}}}
  ]}}`)
	})
	device, err := fastiron.New(fastiron.Config{Host: server.SSHAddress, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: time.Second}, SSH: &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	source := &DataSource{device: device}
	var schema datasource.SchemaResponse
	source.Schema(ctx, datasource.SchemaRequest{}, &schema)
	response := datasource.ReadResponse{State: tfsdk.State{Schema: schema.Schema}}
	source.Read(ctx, datasource.ReadRequest{}, &response)
	if response.Diagnostics.HasError() {
		t.Fatal(response.Diagnostics)
	}
	var observed poeInterfacesModel
	if diagnostics := response.State.Get(ctx, &observed); diagnostics.HasError() {
		t.Fatal(diagnostics)
	}

	expected := map[string]poeStatusModel{
		"ethernet 1/1/11": {PowerAllocated: types.Float64Value(0), Enabled: types.BoolValue(false), Priority: types.Int64Value(3), PowerByClass: types.Int64Value(0), PowerLimit: types.Int64Value(0), PowerClass: types.Int64Null(), PowerUsed: types.Float64Null()},
		"ethernet 1/1/12": {PowerAllocated: types.Float64Value(18000.5), Enabled: types.BoolValue(true), Priority: types.Int64Value(3), PowerByClass: types.Int64Value(2), PowerLimit: types.Int64Value(0), PowerClass: types.Int64Value(4), PowerUsed: types.Float64Value(7000)},
		"ethernet 1/1/13": {PowerAllocated: types.Float64Null(), Enabled: types.BoolValue(true), Priority: types.Int64Value(1), PowerByClass: types.Int64Value(0), PowerLimit: types.Int64Value(18000), PowerClass: types.Int64Null(), PowerUsed: types.Float64Null()},
	}
	if !reflect.DeepEqual(observed.Interfaces, expected) {
		t.Fatalf("inventory = %#v; want %#v", observed.Interfaces, expected)
	}
}
