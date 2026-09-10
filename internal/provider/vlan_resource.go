package provider

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

type vlanResource struct{ device *fastiron.Device }
type vlanModel struct {
	ID                 types.String `tfsdk:"id"`
	VLANID             types.Int64  `tfsdk:"vlan_id"`
	Name               types.String `tfsdk:"name"`
	PersistencePending types.Bool   `tfsdk:"persistence_pending"`
}

var (
	_ resource.ResourceWithConfigure      = (*vlanResource)(nil)
	_ resource.ResourceWithImportState    = (*vlanResource)(nil)
	_ resource.ResourceWithValidateConfig = (*vlanResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*vlanResource)(nil)
)

func (r *vlanResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vlan"
}
func (r *vlanResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Owns VLAN existence and its name. Membership, spanning tree, and routed interfaces are separate domains. Import with vlan <id>.", Attributes: map[string]schema.Attribute{
		"id":                  schema.StringAttribute{Computed: true, Description: "Canonical identity: vlan <id>.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"vlan_id":             schema.Int64Attribute{Required: true, Description: "VLAN identifier, 2 through 4094. The default VLAN is not managed.", PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()}},
		"name":                schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString(""), Description: "VLAN name. Omission clears the name; import requires matching HCL to preserve a non-default name."},
		"persistence_pending": schema.BoolAttribute{Computed: true, Description: "True when a failed operation still requires reconciliation or persistence."},
	}}
}

func (r *vlanResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *vlanResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model vlanModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if model.VLANID.IsUnknown() || model.VLANID.IsNull() {
		return
	}
	if err := fastiron.ValidateVLAN(fastiron.VLAN{ID: model.VLANID.ValueInt64(), Name: model.Name.ValueString()}); err != nil {
		resp.Diagnostics.AddError("Invalid VLAN configuration", err.Error())
	}
}

func (r *vlanResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || r.device == nil {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
	var plan vlanModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || plan.VLANID.IsUnknown() || plan.Name.IsUnknown() {
		return
	}
	if err := r.device.CheckVLAN(ctx, fastiron.VLAN{ID: plan.VLANID.ValueInt64(), Name: plan.Name.ValueString()}); err != nil {
		resp.Diagnostics.AddError("VLAN is not supported by the configured switch", err.Error())
	}
}

func (r *vlanResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vlanModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := r.device.ApplyVLAN(ctx, fastiron.VLAN{ID: plan.VLANID.ValueInt64(), Name: plan.Name.ValueString()})
	if observed != nil {
		state := vlanState(*observed)
		state.PersistencePending = types.BoolValue(err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot apply VLAN", err.Error())
	}
}
func (r *vlanResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan vlanModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := r.device.ApplyVLAN(ctx, fastiron.VLAN{ID: plan.VLANID.ValueInt64(), Name: plan.Name.ValueString()})
	if observed != nil {
		state := vlanState(*observed)
		state.PersistencePending = types.BoolValue(err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot update VLAN", err.Error())
	}
}
func (r *vlanResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vlanModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	v, err := r.device.VLAN(ctx, state.VLANID.ValueInt64())
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
func (r *vlanResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vlanModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.device.DeleteVLAN(ctx, state.VLANID.ValueInt64()); err != nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
		resp.Diagnostics.AddError("Cannot delete VLAN", err.Error())
	}
}
func (r *vlanResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(strings.TrimPrefix(req.ID, "vlan "), 10, 64)
	if err != nil || req.ID != "vlan "+strconv.FormatInt(id, 10) || fastiron.ValidateVLAN(fastiron.VLAN{ID: id}) != nil {
		resp.Diagnostics.AddError("Invalid VLAN import identity", "Use vlan <id>, with an ID between 2 and 4094.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("vlan_id"), id)...)
}

func vlanState(v fastiron.VLAN) vlanModel {
	return vlanModel{ID: types.StringValue("vlan " + strconv.FormatInt(v.ID, 10)), VLANID: types.Int64Value(v.ID), Name: types.StringValue(v.Name), PersistencePending: types.BoolValue(false)}
}
