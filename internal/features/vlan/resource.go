package vlan

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	Resource  struct{ device *fastiron.Device }
	vlanModel struct {
		ID                 types.String `tfsdk:"id"`
		VLANID             types.Int64  `tfsdk:"vlan_id"`
		Name               types.String `tfsdk:"name"`
		PersistencePending types.Bool   `tfsdk:"persistence_pending"`
	}
)

var (
	_ resource.ResourceWithConfigure      = (*Resource)(nil)
	_ resource.ResourceWithImportState    = (*Resource)(nil)
	_ resource.ResourceWithValidateConfig = (*Resource)(nil)
	_ resource.ResourceWithModifyPlan     = (*Resource)(nil)
)

func (r *Resource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vlan"
}

func (r *Resource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Owns VLAN existence and its name. Membership, spanning tree, and routed interfaces are separate domains. Import with vlan <id>.", Attributes: map[string]schema.Attribute{
		"id":                  schema.StringAttribute{Computed: true, Description: "Canonical identity: vlan <id>.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"vlan_id":             schema.Int64Attribute{Required: true, Description: "VLAN identifier, 1 through 4094. The active default VLAN is managed separately.", PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()}},
		"name":                schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString(""), Description: "VLAN name. Omission clears the name; import requires matching HCL to preserve a non-default name. DEFAULT-VLAN is reserved for global default VLAN selection."},
		"persistence_pending": schema.BoolAttribute{Computed: true, Description: "True when a failed operation still requires reconciliation or persistence."},
	}}
}

func (r *Resource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	device, ok := req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
		return
	}
	r.device = device
}

func (r *Resource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model vlanModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if model.VLANID.IsUnknown() || model.VLANID.IsNull() {
		return
	}
	if err := Validate(Config{ID: model.VLANID.ValueInt64(), Name: model.Name.ValueString()}); err != nil {
		resp.Diagnostics.AddError("Invalid VLAN configuration", err.Error())
	}
}

func (r *Resource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || r.device == nil {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
	var plan vlanModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || plan.VLANID.IsUnknown() || plan.Name.IsUnknown() {
		return
	}
	if err := check(ctx, r.device, Config{ID: plan.VLANID.ValueInt64(), Name: plan.Name.ValueString()}); err != nil {
		resp.Diagnostics.AddError("VLAN is not supported by the configured switch", err.Error())
	}
}

func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vlanModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := apply(ctx, r.device, Config{ID: plan.VLANID.ValueInt64(), Name: plan.Name.ValueString()})
	if observed != nil {
		state := vlanState(*observed)
		state.PersistencePending = types.BoolValue(err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot apply VLAN", err.Error())
	}
}

func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan vlanModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := apply(ctx, r.device, Config{ID: plan.VLANID.ValueInt64(), Name: plan.Name.ValueString()})
	if observed != nil {
		state := vlanState(*observed)
		state.PersistencePending = types.BoolValue(err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot update VLAN", err.Error())
	}
}

func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vlanModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	v, err := Read(ctx, r.device, state.VLANID.ValueInt64())
	if errors.Is(err, fastiron.ErrNotFound) {
		// Retain a failed delete until its persistence step can be retried.
		if state.PersistencePending.ValueBool() {
			return
		}
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read VLAN", err.Error())
		return
	}
	observed := vlanState(v)
	observed.PersistencePending = state.PersistencePending
	if observed.PersistencePending.IsNull() {
		observed.PersistencePending = types.BoolValue(false)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, observed)...)
}

func (r *Resource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vlanModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := remove(ctx, r.device, state.VLANID.ValueInt64()); err != nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
		resp.Diagnostics.AddError("Cannot delete VLAN", err.Error())
	}
}

func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(strings.TrimPrefix(req.ID, "vlan "), 10, 64)
	if err != nil || req.ID != "vlan "+strconv.FormatInt(id, 10) || Validate(Config{ID: id}) != nil {
		resp.Diagnostics.AddError("Invalid VLAN import identity", "Use vlan <id>, with an ID between 1 and 4094.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("vlan_id"), id)...)
}

func vlanState(v Config) vlanModel {
	return vlanModel{ID: types.StringValue("vlan " + strconv.FormatInt(v.ID, 10)), VLANID: types.Int64Value(v.ID), Name: types.StringValue(v.Name), PersistencePending: types.BoolValue(false)}
}
