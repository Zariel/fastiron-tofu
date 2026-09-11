package acl

import (
	"context"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestStandardDestroyRetry(t *testing.T) {
	ctx := context.Background()
	s := &standardSwitch{running: populatedStandard + neighbor, failSave: true}
	device := s.device(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("unexpected method %s", r.Method)
		}
		s.running = absentStandard + neighbor
		w.WriteHeader(204)
	})
	r := &StandardResource{device: device}
	var schema resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schema)
	state := tfsdk.State{Schema: schema.Schema}
	rules, diagnostics := types.SetValueFrom(ctx, standardRuleType, []standardRuleModel{{Sequence: types.Int64Value(10), Action: types.StringValue("permit"), Source: types.StringValue("192.0.2.0/24")}})
	if diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	model := standardModel{ID: types.StringValue("ip access-list standard 90"), Name: types.StringValue("90"), Rules: rules, PersistencePending: types.BoolValue(false)}
	if diagnostics := state.Set(ctx, model); diagnostics.HasError() {
		t.Fatal(diagnostics)
	}

	deleted := resource.DeleteResponse{State: state}
	r.Delete(ctx, resource.DeleteRequest{State: state}, &deleted)
	if !deleted.Diagnostics.HasError() {
		t.Fatal("save failure accepted")
	}
	if s.running != absentStandard+neighbor || s.startup != populatedStandard+neighbor {
		t.Fatal("fixture must remove running ACL while retaining saved ACL")
	}

	refreshed := resource.ReadResponse{State: deleted.State}
	r.Read(ctx, resource.ReadRequest{State: deleted.State}, &refreshed)
	if refreshed.Diagnostics.HasError() {
		t.Fatal(refreshed.Diagnostics)
	}
	if refreshed.State.Raw.IsNull() {
		t.Fatal("refresh forgot the pending save after deletion")
	}
	if diagnostics := refreshed.State.Get(ctx, &model); diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	if !model.PersistencePending.ValueBool() {
		t.Fatal("pending save was cleared during refresh")
	}

	s.failSave = false
	retried := resource.DeleteResponse{State: refreshed.State}
	r.Delete(ctx, resource.DeleteRequest{State: refreshed.State}, &retried)
	if retried.Diagnostics.HasError() {
		t.Fatal(retried.Diagnostics)
	}
	if s.writes != 1 || s.saves != 1 || s.startup != absentStandard+neighbor {
		t.Fatalf("retry: writes=%d saves=%d startup=%q", s.writes, s.saves, s.startup)
	}

	model.PersistencePending = types.BoolValue(false)
	if diagnostics := state.Set(ctx, model); diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	refreshed = resource.ReadResponse{State: state}
	r.Read(ctx, resource.ReadRequest{State: state}, &refreshed)
	if refreshed.Diagnostics.HasError() || !refreshed.State.Raw.IsNull() {
		t.Fatalf("ordinary external deletion retained state: %v", refreshed.Diagnostics)
	}
}
