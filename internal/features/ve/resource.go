package ve

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
	Resource struct{ device *fastiron.Device }
	veModel  struct {
		ID                 types.String `tfsdk:"id"`
		VEID               types.Int64  `tfsdk:"ve_id"`
		VLANID             types.Int64  `tfsdk:"vlan_id"`
		Name               types.String `tfsdk:"name"`
		PortName           types.String `tfsdk:"port_name"`
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
	resp.TypeName = req.ProviderTypeName + "_interface_ve"
}

func (r *Resource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Owns a VE interface and its port name. Addresses and protocol bindings are separate resources and block parent deletion. Import with ve <id>.", Attributes: map[string]schema.Attribute{
		"id":                  schema.StringAttribute{Computed: true, Description: "Canonical identity: ve <id>.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"ve_id":               schema.Int64Attribute{Required: true, Description: "VE identifier, 2 through 4094.", PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()}},
		"vlan_id":             schema.Int64Attribute{Required: true, Description: "Existing VLAN identifier. Must match ve_id.", PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()}},
		"name":                schema.StringAttribute{Computed: true, Description: "Canonical interface name.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"port_name":           schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString(""), Description: "Interface description. Omission clears it."},
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
	var model veModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if model.VEID.IsUnknown() || model.VEID.IsNull() || model.VLANID.IsUnknown() || model.VLANID.IsNull() {
		return
	}
	if err := validate(config{ID: model.VEID.ValueInt64(), VLANID: model.VLANID.ValueInt64(), PortName: model.PortName.ValueString()}); err != nil {
		resp.Diagnostics.AddError("Invalid VE configuration", err.Error())
	}
}

func (r *Resource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || r.device == nil {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
	var plan veModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || plan.VEID.IsUnknown() || plan.VLANID.IsUnknown() || plan.PortName.IsUnknown() {
		return
	}
	if err := check(ctx, r.device, config{ID: plan.VEID.ValueInt64(), VLANID: plan.VLANID.ValueInt64(), PortName: plan.PortName.ValueString()}); err != nil {
		resp.Diagnostics.AddError("VE is not supported by the configured switch", err.Error())
	}
}

func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan veModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := apply(ctx, r.device, config{ID: plan.VEID.ValueInt64(), VLANID: plan.VLANID.ValueInt64(), PortName: plan.PortName.ValueString()})
	if observed != nil {
		state := veState(*observed)
		state.PersistencePending = types.BoolValue(err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot apply VE", err.Error())
	}
}

func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan veModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := apply(ctx, r.device, config{ID: plan.VEID.ValueInt64(), VLANID: plan.VLANID.ValueInt64(), PortName: plan.PortName.ValueString()})
	if observed != nil {
		state := veState(*observed)
		state.PersistencePending = types.BoolValue(err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot update VE", err.Error())
	}
}

func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state veModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	v, err := Read(ctx, r.device, state.VEID.ValueInt64())
	if errors.Is(err, fastiron.ErrNotFound) {
		// Retain a failed delete until its persistence step can be retried.
		if state.PersistencePending.ValueBool() {
			return
		}
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read VE", err.Error())
		return
	}
	observed := veState(v)
	observed.PersistencePending = state.PersistencePending
	if observed.PersistencePending.IsNull() {
		observed.PersistencePending = types.BoolValue(false)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, observed)...)
}

func (r *Resource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state veModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := remove(ctx, r.device, state.VEID.ValueInt64()); err != nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
		resp.Diagnostics.AddError("Cannot delete VE", err.Error())
	}
}

func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(strings.TrimPrefix(req.ID, "ve "), 10, 64)
	if err != nil || req.ID != "ve "+strconv.FormatInt(id, 10) || validate(config{ID: id, VLANID: id}) != nil {
		resp.Diagnostics.AddError("Invalid VE identity", "Use ve <id>, with an ID between 2 and 4094.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("ve_id"), id)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("vlan_id"), id)...)
}

func veState(v config) veModel {
	return veModel{ID: types.StringValue("ve " + strconv.FormatInt(v.ID, 10)), Name: types.StringValue("ve " + strconv.FormatInt(v.ID, 10)), VEID: types.Int64Value(v.ID), VLANID: types.Int64Value(v.VLANID), PortName: types.StringValue(v.PortName), PersistencePending: types.BoolValue(false)}
}
