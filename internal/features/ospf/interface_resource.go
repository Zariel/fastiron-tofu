package ospf

import (
	"context"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	InterfaceResource  struct{ device *fastiron.Device }
	ospfInterfaceModel struct {
		ID                 types.String `tfsdk:"id"`
		AreaID             types.String `tfsdk:"area_id"`
		Interface          types.String `tfsdk:"interface"`
		PersistencePending types.Bool   `tfsdk:"persistence_pending"`
	}
)

func (r *InterfaceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_router_ospf_interface"
}

func (r *InterfaceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Owns one interface binding to an OSPF area in the default VRF. Additional interface settings remain independently managed.", Attributes: map[string]schema.Attribute{
		"id":                  schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"area_id":             schema.StringAttribute{Required: true, Description: "Canonical dotted area identifier, such as 0.0.0.0.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"interface":           schema.StringAttribute{Required: true, Description: "Canonical Ethernet, LAG, VE, or loopback interface name.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"persistence_pending": schema.BoolAttribute{Computed: true, Description: "True when a failed operation still requires reconciliation or persistence."},
	}}
}

func (r *InterfaceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	r.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (r *InterfaceResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m ospfInterfaceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !m.AreaID.IsUnknown() && !m.AreaID.IsNull() {
		if err := validateAreaID(m.AreaID.ValueString()); err != nil {
			resp.Diagnostics.AddError("Invalid OSPF area", err.Error())
		}
	}
	if !m.Interface.IsUnknown() && !m.Interface.IsNull() {
		if err := validateInterface(m.Interface.ValueString()); err != nil {
			resp.Diagnostics.AddError("Invalid OSPF interface", err.Error())
		}
	}
}

func (r *InterfaceResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
	var m ospfInterfaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || m.AreaID.IsUnknown() || m.AreaID.IsNull() || r.device == nil {
		return
	}
	if _, err := readAreas(ctx, r.device); err != nil {
		resp.Diagnostics.AddError("Cannot read OSPF area capability", err.Error())
	}
}

func (r *InterfaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m ospfInterfaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := applyInterface(ctx, r.device, m.AreaID.ValueString(), m.Interface.ValueString(), true)
	if observed {
		m.ID = types.StringValue(m.AreaID.ValueString() + "|" + m.Interface.ValueString())
		m.PersistencePending = types.BoolValue(err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot apply OSPF binding", err.Error())
	}
}

func (r *InterfaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m ospfInterfaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := applyInterface(ctx, r.device, m.AreaID.ValueString(), m.Interface.ValueString(), true)
	if observed {
		m.PersistencePending = types.BoolValue(err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot reconcile OSPF binding", err.Error())
	}
}

func (r *InterfaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m ospfInterfaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	areas, err := readAreas(ctx, r.device)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read OSPF area", err.Error())
		return
	}
	if slices.IndexFunc(areas, func(area area) bool {
		return area.ID == m.AreaID.ValueString() && slices.Contains(area.Interfaces, m.Interface.ValueString())
	}) < 0 {
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

func (r *InterfaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m ospfInterfaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := applyInterface(ctx, r.device, m.AreaID.ValueString(), m.Interface.ValueString(), false); err != nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
		resp.Diagnostics.AddError("Cannot remove OSPF binding", err.Error())
	}
}

func (r *InterfaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	area, name, ok := strings.Cut(req.ID, "|")
	if !ok || validateAreaID(area) != nil || validateInterface(name) != nil {
		resp.Diagnostics.AddError("Invalid OSPF binding identity", "Use <dotted area ID>|<interface name>.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, ospfInterfaceModel{ID: types.StringValue(req.ID), AreaID: types.StringValue(area), Interface: types.StringValue(name), PersistencePending: types.BoolValue(false)})...)
}
