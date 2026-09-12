package acl

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestACLDetails(t *testing.T) {
	cases := map[string]struct {
		kind, name, id, native string
		want                   []detailRule
		wantErr                bool
	}{
		"standard order": {
			kind: "ipv4_standard", name: "97", id: "ip access-list standard 97",
			native: "ip access-list standard 97\n sequence 30 deny host 198.51.100.1\n sequence 10 permit 192.0.2.0 0.0.0.255\n",
			want: []detailRule{
				{ipRuleFields: ipRuleFields{Sequence: types.Int64Value(10), Action: types.StringValue("permit"), Source: types.StringValue("192.0.2.0/24")}},
				{ipRuleFields: ipRuleFields{Sequence: types.Int64Value(30), Action: types.StringValue("deny"), Source: types.StringValue("198.51.100.1/32")}},
			},
		},
		"extended zeros": {
			kind: "ipv4_extended", name: "EDGE", id: "ip access-list extended EDGE",
			native: "ip access-list extended EDGE\n sequence 20 permit tcp any any range ssl 444 dscp-matching 0 dscp-marking 0 internal-priority-marking 0\n",
			want: []detailRule{{ipRuleFields: ipRuleFields{
				Sequence: types.Int64Value(20), Action: types.StringValue("permit"), Source: types.StringValue("any"), Destination: types.StringValue("any"),
				Protocol: types.Int64Value(6), SourcePort: types.StringValue("any"), DestinationPort: types.StringValue("443..444"),
				DSCP: types.Int64Value(0), DSCPMark: types.Int64Value(0), Priority: types.Int64Value(0),
			}}},
		},
		"IPv6 logging": {
			kind: "ipv6", name: "EDGE", id: "ipv6 access-list EDGE",
			native: "ipv6 access-list EDGE\n sequence 10 permit tcp 2001:db8::/64 any eq ssl log\n",
			want: []detailRule{{Log: types.BoolValue(true), ipRuleFields: ipRuleFields{
				Sequence: types.Int64Value(10), Action: types.StringValue("permit"), Source: types.StringValue("2001:db8::/64"), Destination: types.StringValue("any"),
				Protocol: types.Int64Value(6), SourcePort: types.StringValue("any"), DestinationPort: types.StringValue("443"),
			}}},
		},
		"MAC order and masks": {
			kind: "mac", name: "EDGE", id: "mac access-list EDGE",
			native: "mac access-list EDGE\n permit 0200.0000.0001 ff00.ff00.ff00 any ether-type 86dd log\n deny any any\n",
			want: []detailRule{
				{ipRuleFields: ipRuleFields{Action: types.StringValue("permit"), Source: types.StringValue("02:00:00:00:00:01"), Destination: types.StringValue("any")}, SourceMask: types.StringValue("ff:00:ff:00:ff:00"), DestinationMask: types.StringValue("any"), EtherType: types.Int64Value(34525), Log: types.BoolValue(true)},
				{ipRuleFields: ipRuleFields{Action: types.StringValue("deny"), Source: types.StringValue("any"), Destination: types.StringValue("any")}, SourceMask: types.StringValue("any"), DestinationMask: types.StringValue("any"), Log: types.BoolValue(false)},
			},
		},
		"unsupported rule": {kind: "mac", name: "EDGE", native: "mac access-list EDGE\n deny any any accounting\n", wantErr: true},
		"missing ACL":      {kind: "ipv6", name: "EDGE", native: "mac access-list EDGE\n deny any any\n", wantErr: true},
		"invalid kind":     {kind: "other", name: "EDGE", wantErr: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := &standardSwitch{running: absentStandard + tc.native + "end"}
			d := NewDataSource()
			d.device = s.device(t, func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("ACL query attempted REST mutation: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(500)
			})
			var schema datasource.SchemaResponse
			d.Schema(ctx, datasource.SchemaRequest{}, &schema)
			state := tfsdk.State{Schema: schema.Schema}
			model := struct {
				Kind  types.String `tfsdk:"kind"`
				Name  types.String `tfsdk:"name"`
				ID    types.String `tfsdk:"id"`
				Rules []detailRule `tfsdk:"rules"`
			}{Kind: types.StringValue(tc.kind), Name: types.StringValue(tc.name)}
			if diags := state.Set(ctx, model); diags.HasError() {
				t.Fatal(diags)
			}
			request := datasource.ReadRequest{Config: tfsdk.Config{Schema: schema.Schema, Raw: state.Raw}}
			response := datasource.ReadResponse{State: state}

			d.Read(ctx, request, &response)
			if response.Diagnostics.HasError() != tc.wantErr {
				t.Fatalf("read diagnostics: %v", response.Diagnostics)
			}
			if s.writes != 0 || s.saves != 0 {
				t.Fatal("ACL query modified or saved configuration")
			}
			if tc.wantErr {
				return
			}
			if diags := response.State.Get(ctx, &model); diags.HasError() {
				t.Fatal(diags)
			}
			if model.ID.ValueString() != tc.id || !reflect.DeepEqual(model.Rules, tc.want) {
				t.Fatalf("ACL details: id=%s rules=%+v; want id=%s rules=%+v", model.ID, model.Rules, tc.id, tc.want)
			}

			s.running = absentStandard + tc.id + "\nend"
			d.Read(ctx, request, &response)
			if response.Diagnostics.HasError() {
				t.Fatal(response.Diagnostics)
			}
			if diags := response.State.Get(ctx, &model); diags.HasError() {
				t.Fatal(diags)
			}
			if model.Rules == nil || len(model.Rules) != 0 {
				t.Fatalf("empty ACL retained rules or returned null: %+v", model.Rules)
			}
		})
	}
}
