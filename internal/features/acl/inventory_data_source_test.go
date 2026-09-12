package acl

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
)

func TestACLInventory(t *testing.T) {
	for name, tc := range map[string]struct {
		configuration string
		want          []aclIdentity
		wantErr       bool
	}{
		"all kinds": {
			configuration: `mac access-list SHARED
 remark operator-owned rules
 deny any any accounting
ipv6 access-list SHARED
ip access-list standard 90
 sequence 10 permit any
ip access-list extended SHARED
 sequence 20 permit tcp any any established
interface ethernet 1/1/9
 ip access-group 90 in
`,
			want: []aclIdentity{
				{ID: "ip access-list extended SHARED", Kind: "ipv4_extended", Name: "SHARED"},
				{ID: "ip access-list standard 90", Kind: "ipv4_standard", Name: "90"},
				{ID: "ipv6 access-list SHARED", Kind: "ipv6", Name: "SHARED"},
				{ID: "mac access-list SHARED", Kind: "mac", Name: "SHARED"},
			},
		},
		"empty":              {configuration: "vlan 1\n router-interface ve 1\n", want: []aclIdentity{}},
		"duplicate identity": {configuration: "mac access-list EDGE\n deny any any\nmac access-list EDGE\n", wantErr: true},
		"missing name":       {configuration: "ipv6 access-list\n", wantErr: true},
		"unknown kind":       {configuration: "ip access-list other EDGE\n", wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := &standardSwitch{running: absentStandard + tc.configuration + "end"}
			d := NewInventoryDataSource()
			d.device = s.device(t, func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("inventory attempted REST mutation: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(500)
			})
			var schema datasource.SchemaResponse
			d.Schema(ctx, datasource.SchemaRequest{}, &schema)
			response := datasource.ReadResponse{State: tfsdk.State{Schema: schema.Schema}}

			d.Read(ctx, datasource.ReadRequest{}, &response)
			if response.Diagnostics.HasError() != tc.wantErr {
				t.Fatalf("read diagnostics: %v", response.Diagnostics)
			}
			if s.writes != 0 || s.saves != 0 {
				t.Fatal("inventory modified or saved configuration")
			}
			if tc.wantErr {
				return
			}
			var result struct {
				ACLs []aclIdentity `tfsdk:"acls"`
			}
			if diags := response.State.Get(ctx, &result); diags.HasError() {
				t.Fatal(diags)
			}
			if !slices.Equal(result.ACLs, tc.want) || result.ACLs == nil {
				t.Fatalf("ACL inventory=%+v; want %+v", result.ACLs, tc.want)
			}
		})
	}
}
