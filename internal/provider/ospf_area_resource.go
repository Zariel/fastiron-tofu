package provider

import (
	"context"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	ospfAreaResource struct{ device *fastiron.Device }
	ospfAreaModel    struct {
		ID                 types.String `tfsdk:"id"`
		AreaID             types.String `tfsdk:"area_id"`
		PersistencePending types.Bool   `tfsdk:"persistence_pending"`
	}
)

func (r *ospfAreaResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_router_ospf_area"
}

func (r *ospfAreaResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Owns one OSPF area in the default VRF. Interface bindings and area options remain independently managed.", Attributes: map[string]schema.Attribute{
		"id":                  schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"area_id":             schema.StringAttribute{Required: true, Description: "Canonical dotted area identifier, such as 0.0.0.0.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"persistence_pending": schema.BoolAttribute{Computed: true, Description: "True when a failed operation still requires reconciliation or persistence."},
	}}
}

func (r *ospfAreaResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	r.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (r *ospfAreaResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m ospfAreaModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || m.AreaID.IsUnknown() || m.AreaID.IsNull() {
		return
	}
	if err := fastiron.ValidateOSPFAreaID(m.AreaID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Invalid OSPF area", err.Error())
	}
}

func (r *ospfAreaResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
	var m ospfAreaModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || m.AreaID.IsUnknown() || m.AreaID.IsNull() || r.device == nil {
		return
	}
	if _, err := r.device.OSPFAreas(ctx); err != nil {
		resp.Diagnostics.AddError("Cannot read OSPF area capability", err.Error())
	}
}

func (r *ospfAreaResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m ospfAreaModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := r.device.ApplyOSPFArea(ctx, m.AreaID.ValueString(), true)
	if observed != nil {
		m.ID = m.AreaID
		m.PersistencePending = types.BoolValue(err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot apply OSPF area", err.Error())
	}
}

func (r *ospfAreaResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m ospfAreaModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := r.device.ApplyOSPFArea(ctx, m.AreaID.ValueString(), true)
	if observed != nil {
		m.PersistencePending = types.BoolValue(err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot reconcile OSPF area", err.Error())
	}
}

func (r *ospfAreaResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m ospfAreaModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	areas, err := r.device.OSPFAreas(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read OSPF area", err.Error())
		return
	}
	if slices.IndexFunc(areas, func(area fastiron.OSPFArea) bool { return area.ID == m.AreaID.ValueString() }) < 0 {
		// Preserve failed-delete state until startup persistence can be retried.
		if !m.PersistencePending.ValueBool() {
			resp.State.RemoveResource(ctx)
		}
		return
	}
	if m.PersistencePending.IsNull() {
		m.PersistencePending = types.BoolValue(false)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

func (r *ospfAreaResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m ospfAreaModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.device.ApplyOSPFArea(ctx, m.AreaID.ValueString(), false); err != nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
		resp.Diagnostics.AddError("Cannot remove OSPF area", err.Error())
	}
}

func (r *ospfAreaResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if err := fastiron.ValidateOSPFAreaID(req.ID); err != nil {
		resp.Diagnostics.AddError("Invalid OSPF area identity", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, ospfAreaModel{ID: types.StringValue(req.ID), AreaID: types.StringValue(req.ID), PersistencePending: types.BoolValue(false)})...)
}
