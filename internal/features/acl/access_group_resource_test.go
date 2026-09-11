package acl

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func groupState(t *testing.T, r *accessGroupResource) tfsdk.State {
	t.Helper()
	var schema resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &schema)
	state := tfsdk.State{Schema: schema.Schema}
	model := accessGroupModel{ID: types.StringValue("ethernet 1/1/9 in"), Interface: types.StringValue("ethernet 1/1/9"), Direction: types.StringValue("in"), ACL: types.StringValue("90"), PersistencePending: types.BoolValue(false)}
	if diagnostics := state.Set(context.Background(), model); diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	return state
}

func TestAccessGroupDestroyRetry(t *testing.T) {
	ctx := context.Background()
	s := &groupSwitch{active: "90", rest: map[string]bool{"90": true}, failSave: true}
	r := NewIPAccessGroupResource()
	r.device = s.device(t)
	state := groupState(t, r)
	deleted := resource.DeleteResponse{State: state}
	r.Delete(ctx, resource.DeleteRequest{State: state}, &deleted)
	if !deleted.Diagnostics.HasError() || s.active != "" || len(s.rest) != 0 {
		t.Fatal("fixture did not remove the binding before the save failed")
	}
	if !strings.Contains(s.startup, "interface ethernet 1/1/9\n ip access-group 90 in") {
		t.Fatal("fixture lost the unsaved binding")
	}

	refreshed := resource.ReadResponse{State: deleted.State}
	r.Read(ctx, resource.ReadRequest{State: deleted.State}, &refreshed)
	if refreshed.Diagnostics.HasError() || refreshed.State.Raw.IsNull() {
		t.Fatalf("refresh forgot pending save: %v", refreshed.Diagnostics)
	}
	s.failSave = false
	retried := resource.DeleteResponse{State: refreshed.State}
	r.Delete(ctx, resource.DeleteRequest{State: refreshed.State}, &retried)
	if retried.Diagnostics.HasError() || s.startup != groupParents+groupNeighbor || s.saves != 1 {
		t.Fatalf("destroy retry failed: %v", retried.Diagnostics)
	}
}

func TestAccessGroupRefreshRecovery(t *testing.T) {
	ctx := context.Background()
	s := &groupSwitch{rest: map[string]bool{"90": true}}
	r := NewIPAccessGroupResource()
	r.device = s.device(t)
	state := groupState(t, r)
	refreshed := resource.ReadResponse{State: state}
	r.Read(ctx, resource.ReadRequest{State: state}, &refreshed)
	if refreshed.Diagnostics.HasError() || refreshed.State.Raw.IsNull() {
		t.Fatalf("refresh forgot stale REST binding: %v", refreshed.Diagnostics)
	}
	var model accessGroupModel
	if diagnostics := refreshed.State.Get(ctx, &model); diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	if !model.PersistencePending.ValueBool() {
		t.Fatal("stale REST binding was reported converged")
	}

	updated := resource.UpdateResponse{State: refreshed.State}
	plan := tfsdk.Plan{Schema: state.Schema, Raw: state.Raw}
	r.Update(ctx, resource.UpdateRequest{Plan: plan, State: refreshed.State}, &updated)
	if updated.Diagnostics.HasError() {
		t.Fatal(updated.Diagnostics)
	}
	if s.active != "90" || len(s.rest) != 1 || !s.rest["90"] || s.startup != groupParents+"interface ethernet 1/1/9\n ip access-group 90 in\n"+groupNeighbor {
		t.Fatal("update did not restore one consistent native and REST binding")
	}

	s.active = ""
	s.rest = map[string]bool{}
	refreshed = resource.ReadResponse{State: updated.State}
	r.Read(ctx, resource.ReadRequest{State: updated.State}, &refreshed)
	if refreshed.Diagnostics.HasError() || !refreshed.State.Raw.IsNull() {
		t.Fatalf("ordinary external deletion retained state: %v", refreshed.Diagnostics)
	}
}
