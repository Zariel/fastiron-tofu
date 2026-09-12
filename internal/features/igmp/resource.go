package igmp

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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	vlanResource struct{ device *fastiron.Device }
	vlanModel    struct {
		ID      types.String `tfsdk:"id"`
		VLANID  types.Int64  `tfsdk:"vlan_id"`
		Mode    types.String `tfsdk:"querier_mode"`
		Version types.Int64  `tfsdk:"version"`
		Pending types.Bool   `tfsdk:"persistence_pending"`
	}
)

func NewVLANResource() *vlanResource { return &vlanResource{} }

var (
	_ resource.ResourceWithConfigure      = (*vlanResource)(nil)
	_ resource.ResourceWithValidateConfig = (*vlanResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*vlanResource)(nil)
	_ resource.ResourceWithImportState    = (*vlanResource)(nil)
)

func (r *vlanResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vlan_igmp_snooping"
}

func (r *vlanResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Owns IGMP snooping mode and version overrides on an existing VLAN. Omission and deletion restore inheritance from global settings. Other multicast settings remain separately owned. Import with vlan <id>.", Attributes: map[string]schema.Attribute{
		"id":                  schema.StringAttribute{Computed: true, Description: "Canonical identity: vlan <id>.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"vlan_id":             schema.Int64Attribute{Required: true, Description: "Existing VLAN identifier, 1 through 4095.", PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()}},
		"querier_mode":        schema.StringAttribute{Optional: true, Description: "active or passive. Omit to inherit the global mode. Native disabled overrides are reported on refresh, but cannot be configured through the tested RESTCONF API."},
		"version":             schema.Int64Attribute{Optional: true, Description: "IGMP version 2 or 3. Omit to inherit the global version."},
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
	if !model.VLANID.IsNull() && !model.VLANID.IsUnknown() {
		if err := validate(model.VLANID.ValueInt64(), settings{}); err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("vlan_id"), "Invalid VLAN", err.Error())
		}
	}
	if !model.Mode.IsNull() && !model.Mode.IsUnknown() && model.Mode.ValueString() != "active" && model.Mode.ValueString() != "passive" {
		resp.Diagnostics.AddAttributeError(path.Root("querier_mode"), "Invalid IGMP mode", "Use active or passive, or omit the attribute to inherit the global mode.")
	}
	if !model.Version.IsNull() && !model.Version.IsUnknown() && model.Version.ValueInt64() != 2 && model.Version.ValueInt64() != 3 {
		resp.Diagnostics.AddAttributeError(path.Root("version"), "Invalid IGMP version", "Use 2 or 3, or omit the attribute to inherit the global version.")
	}
}

func (r *vlanResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
	var plan vlanModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || plan.VLANID.IsUnknown() || r.device == nil {
		return
	}
	_, err := read(ctx, r.device, plan.VLANID.ValueInt64())
	if err != nil && !errors.Is(err, fastiron.ErrNotFound) {
		resp.Diagnostics.AddError("Cannot read IGMP capability", err.Error())
	}
}

func (r *vlanResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vlanModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := apply(ctx, r.device, plan.VLANID.ValueInt64(), settings{Mode: plan.Mode.ValueString(), Version: plan.Version.ValueInt64()}, true)
	if observed != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, vlanState(plan.VLANID.ValueInt64(), *observed, err != nil))...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot apply VLAN IGMP overrides", err.Error())
	}
}

func (r *vlanResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan vlanModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := apply(ctx, r.device, plan.VLANID.ValueInt64(), settings{Mode: plan.Mode.ValueString(), Version: plan.Version.ValueInt64()}, true)
	if observed != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, vlanState(plan.VLANID.ValueInt64(), *observed, err != nil))...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot update VLAN IGMP overrides", err.Error())
	}
}

func (r *vlanResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vlanModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := read(ctx, r.device, state.VLANID.ValueInt64())
	if errors.Is(err, fastiron.ErrNotFound) {
		if !state.Pending.ValueBool() {
			resp.State.RemoveResource(ctx)
		}
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read VLAN IGMP overrides", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, vlanState(state.VLANID.ValueInt64(), observed.settings, state.Pending.ValueBool()))...)
}

func (r *vlanResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vlanModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := apply(ctx, r.device, state.VLANID.ValueInt64(), settings{}, false)
	if err == nil {
		return
	}
	if observed != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, vlanState(state.VLANID.ValueInt64(), *observed, true))...)
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
	resp.Diagnostics.AddError("Cannot reset VLAN IGMP overrides", err.Error())
}

func (r *vlanResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(strings.TrimPrefix(req.ID, "vlan "), 10, 64)
	if err != nil || req.ID != "vlan "+strconv.FormatInt(id, 10) || validate(id, settings{}) != nil {
		resp.Diagnostics.AddError("Invalid VLAN IGMP identity", "Use vlan <id>, with an identifier between 1 and 4095.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("vlan_id"), id)...)
}

func vlanState(id int64, observed settings, pending bool) vlanModel {
	value := vlanModel{ID: types.StringValue("vlan " + strconv.FormatInt(id, 10)), VLANID: types.Int64Value(id), Mode: types.StringNull(), Version: types.Int64Null(), Pending: types.BoolValue(pending)}
	if observed.Mode != "" {
		value.Mode = types.StringValue(observed.Mode)
	}
	if observed.Version != 0 {
		value.Version = types.Int64Value(observed.Version)
	}
	return value
}
