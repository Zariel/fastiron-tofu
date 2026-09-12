package vlan

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

func TestVLANDiscovery(t *testing.T) {
	const collection = `{"openconfig-network-instance:vlans":{"vlan":[{"vlan-id":53,"config":{"vlan-id":53,"name":"INFRA"}},{"vlan-id":1,"config":{"vlan-id":1,"name":"DEFAULT-VLAN"}},{"vlan-id":50,"config":{"vlan-id":50}}]}}`
	for name, tc := range map[string]struct {
		collection   bool
		id           int64
		item, parent string
		want         map[string]vlanStatus
		wantErr      bool
	}{
		"collection":            {collection: true, parent: collection, want: map[string]vlanStatus{"1": {1, "DEFAULT-VLAN"}, "50": {50, ""}, "53": {53, "INFRA"}}},
		"empty collection":      {collection: true, parent: `{"openconfig-network-instance:vlans":{"vlan":[]}}`, want: map[string]vlanStatus{}},
		"missing container":     {collection: true, parent: `{}`, wantErr: true},
		"duplicate identity":    {collection: true, parent: `{"openconfig-network-instance:vlans":{"vlan":[{"vlan-id":1,"config":{"vlan-id":1}},{"vlan-id":1,"config":{"vlan-id":1}}]}}`, wantErr: true},
		"inconsistent identity": {collection: true, parent: `{"openconfig-network-instance:vlans":{"vlan":[{"vlan-id":1,"config":{"vlan-id":2}}]}}`, wantErr: true},
		"invalid identity":      {collection: true, parent: `{"openconfig-network-instance:vlans":{"vlan":[{"vlan-id":4095,"config":{"vlan-id":4095}}]}}`, wantErr: true},
		"default":               {id: 1, item: `{"openconfig-network-instance:vlan":[{"vlan-id":1,"config":{"vlan-id":1,"name":"DEFAULT-VLAN"}}]}`, want: map[string]vlanStatus{"selected": {1, "DEFAULT-VLAN"}}},
		"unnamed":               {id: 50, item: `{"openconfig-network-instance:vlan":[{"vlan-id":50,"config":{"vlan-id":50}}]}`, want: map[string]vlanStatus{"selected": {50, ""}}},
		"collection fallback":   {id: 53, parent: collection, want: map[string]vlanStatus{"selected": {53, "INFRA"}}},
		"missing VLAN":          {id: 54, parent: collection, wantErr: true},
		"unavailable endpoint":  {id: 54, wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("discovery attempted %s", r.Method)
					w.WriteHeader(405)
					return
				}
				body := tc.item
				if r.URL.Path == vlanPath {
					body = tc.parent
				}
				if body == "" {
					w.WriteHeader(404)
					return
				}
				fmt.Fprint(w, body)
			}))
			defer server.Close()
			device, err := fastiron.New(fastiron.Config{Host: server.URL, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.URL, InsecureSkipVerify: true, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}
			d := &dataSource{device: device, collection: tc.collection}
			ctx := context.Background()
			var schema datasource.SchemaResponse
			d.Schema(ctx, datasource.SchemaRequest{}, &schema)
			state := tfsdk.State{Schema: schema.Schema}
			if !tc.collection {
				if diagnostics := state.Set(ctx, vlanStatus{ID: tc.id}); diagnostics.HasError() {
					t.Fatal(diagnostics)
				}
			}
			request := datasource.ReadRequest{Config: tfsdk.Config{Schema: schema.Schema, Raw: state.Raw}}
			response := datasource.ReadResponse{State: state}

			d.Read(ctx, request, &response)
			if response.Diagnostics.HasError() != tc.wantErr {
				t.Fatalf("read diagnostics: %v", response.Diagnostics)
			}
			if tc.wantErr {
				return
			}
			var model struct {
				VLANs map[string]vlanStatus `tfsdk:"vlans"`
			}
			if tc.collection {
				if diagnostics := response.State.Get(ctx, &model); diagnostics.HasError() {
					t.Fatal(diagnostics)
				}
			} else {
				var selected vlanStatus
				if diagnostics := response.State.Get(ctx, &selected); diagnostics.HasError() {
					t.Fatal(diagnostics)
				}
				model.VLANs = map[string]vlanStatus{"selected": selected}
			}
			if model.VLANs == nil || !maps.Equal(model.VLANs, tc.want) {
				t.Fatalf("VLANs=%+v; want %+v", model.VLANs, tc.want)
			}
		})
	}
}
