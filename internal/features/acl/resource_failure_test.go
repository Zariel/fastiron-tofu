package acl

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestACLAbsentUpdate(t *testing.T) {
	for name, tc := range map[string]struct {
		resource                 resource.Resource
		identity, rule, restType string
		ruleType                 attr.Type
	}{
		"standard": {&StandardResource{}, "ip access-list standard 90", "sequence 10 permit any", "ACL_IPV4", standardRuleType},
		"extended": {NewExtendedResource(), "ip access-list extended EDGE", "sequence 10 permit ip any any", "ACL_IPV4", ipRuleType(ipv4ACL)},
		"IPv6":     {NewIPv6Resource(), "ipv6 access-list EDGE", "sequence 10 permit ipv6 any any", "ACL_IPV6", ipRuleType(ipv6ACL)},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			initial := absentStandard + tc.identity + "\n " + tc.rule + "\n" + neighbor
			fields := strings.Fields(tc.identity)
			aclName := fields[len(fields)-1]
			s := &standardSwitch{running: initial}
			s.restACLs = map[string]any{"openconfig-acl:acl-sets": map[string]any{"acl-set": []any{
				map[string]any{"name": aclName, "type": tc.restType, "acl-entries": map[string]any{"acl-entry": []any{map[string]int64{"sequence-id": 10}}}},
			}}}
			device := s.device(t, func(w http.ResponseWriter, r *http.Request) {
				s.running = absentStandard + neighbor
				http.Error(w, "failed after removing parent", http.StatusInternalServerError)
			})
			configured := resource.ConfigureResponse{}
			tc.resource.(resource.ResourceWithConfigure).Configure(ctx, resource.ConfigureRequest{ProviderData: device}, &configured)
			if configured.Diagnostics.HasError() {
				t.Fatal(configured.Diagnostics)
			}
			var schema resource.SchemaResponse
			tc.resource.Schema(ctx, resource.SchemaRequest{}, &schema)
			state := tfsdk.State{Schema: schema.Schema}
			model := ipModel{ID: types.StringValue(tc.identity), Name: types.StringValue(aclName), Rules: types.SetNull(tc.ruleType), PersistencePending: types.BoolValue(false)}
			if diagnostics := state.Set(ctx, model); diagnostics.HasError() {
				t.Fatal(diagnostics)
			}
			observed := resource.ReadResponse{State: state}
			tc.resource.Read(ctx, resource.ReadRequest{State: state}, &observed)
			if observed.Diagnostics.HasError() {
				t.Fatal(observed.Diagnostics)
			}

			plan := tfsdk.Plan{Raw: observed.State.Raw, Schema: schema.Schema}
			var rules types.Set
			if diagnostics := plan.GetAttribute(ctx, path.Root("rule"), &rules); diagnostics.HasError() {
				t.Fatal(diagnostics)
			}
			rule := rules.Elements()[0].(types.Object)
			attributes := rule.Attributes()
			attributes["action"] = types.StringValue("deny")
			planned := types.SetValueMust(tc.ruleType, []attr.Value{types.ObjectValueMust(rule.AttributeTypes(ctx), attributes)})
			if diagnostics := plan.SetAttribute(ctx, path.Root("rule"), planned); diagnostics.HasError() {
				t.Fatal(diagnostics)
			}
			updated := resource.UpdateResponse{State: observed.State}
			tc.resource.Update(ctx, resource.UpdateRequest{State: observed.State, Plan: plan}, &updated)
			if !updated.Diagnostics.HasError() {
				t.Fatal("partial update failure accepted")
			}
			if s.running != absentStandard+neighbor || s.startup != initial || s.saves != 0 {
				t.Fatal("failed update must leave an unsaved deletion")
			}

			refreshed := resource.ReadResponse{State: updated.State}
			tc.resource.Read(ctx, resource.ReadRequest{State: updated.State}, &refreshed)
			if refreshed.Diagnostics.HasError() {
				t.Fatal(refreshed.Diagnostics)
			}
			if refreshed.State.Raw.IsNull() {
				t.Fatal("refresh forgot the unsaved deletion after update")
			}
			destroyed := resource.DeleteResponse{State: refreshed.State}
			tc.resource.Delete(ctx, resource.DeleteRequest{State: refreshed.State}, &destroyed)
			if destroyed.Diagnostics.HasError() {
				t.Fatal(destroyed.Diagnostics)
			}
			if s.startup != absentStandard+neighbor {
				t.Fatal("destroy did not save the ACL's absence")
			}
		})
	}
}
