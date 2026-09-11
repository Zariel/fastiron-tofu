package stp

import (
	"context"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	VLANResource struct{ device *fastiron.Device }
	stpVLANModel struct {
		ID                 types.String `tfsdk:"id"`
		VLANID             types.Int64  `tfsdk:"vlan_id"`
		Mode               types.String `tfsdk:"mode"`
		Priority           types.Int64  `tfsdk:"priority"`
		PersistencePending types.Bool   `tfsdk:"persistence_pending"`
	}
)

func (r *VLANResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_spanning_tree_vlan"
}

func (r *VLANResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Owns spanning-tree mode and bridge priority on one VLAN. Creation enables spanning tree; destruction disables it.", Attributes: map[string]schema.Attribute{
		"id":                  schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"vlan_id":             schema.Int64Attribute{Required: true, PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()}},
		"mode":                schema.StringAttribute{Required: true, Description: "stp (802.1D) or rstp (802.1w). Mode changes replace this configuration.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"priority":            schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(32768), Description: "Bridge priority, 0–65535."},
		"persistence_pending": schema.BoolAttribute{Computed: true, Description: "True when a failed operation still requires reconciliation or persistence."},
	}}
}

func (r *VLANResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	r.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (m stpVLANModel) desired() vlan {
	return vlan{VLANID: m.VLANID.ValueInt64(), Mode: m.Mode.ValueString(), Priority: m.Priority.ValueInt64()}
}

func (r *VLANResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m stpVLANModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !m.VLANID.IsUnknown() && !m.VLANID.IsNull() {
		if m.VLANID.ValueInt64() < 1 || m.VLANID.ValueInt64() > 4094 {
			resp.Diagnostics.AddError("Invalid VLAN", "vlan_id must be between 1 and 4094.")
		}
	}
	if !m.Mode.IsUnknown() && !m.Mode.IsNull() && m.Mode.ValueString() != "stp" && m.Mode.ValueString() != "rstp" {
		resp.Diagnostics.AddError("Invalid spanning-tree mode", "Use stp or rstp.")
	}
	if !m.Priority.IsUnknown() && !m.Priority.IsNull() && (m.Priority.ValueInt64() < 0 || m.Priority.ValueInt64() > 65535) {
		resp.Diagnostics.AddError("Invalid bridge priority", "Priority must be between 0 and 65535.")
	}
}

func (r *VLANResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
	if r.device != nil {
		if _, err := readVLANs(ctx, r.device); err != nil {
			resp.Diagnostics.AddError("Cannot read spanning-tree VLAN capability", err.Error())
		}
	}
}

func (m *stpVLANModel) observe(v vlan, pending bool) {
	m.VLANID = types.Int64Value(v.VLANID)
	m.Mode = types.StringValue(v.Mode)
	m.Priority = types.Int64Value(v.Priority)
	m.ID = types.StringValue(strconv.FormatInt(v.VLANID, 10))
	m.PersistencePending = types.BoolValue(pending)
}

func (r *VLANResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m stpVLANModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := applyVLAN(ctx, r.device, m.desired(), true)
	if observed != nil {
		m.observe(*observed, err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot apply spanning-tree VLAN", err.Error())
	}
}

func (r *VLANResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m stpVLANModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := applyVLAN(ctx, r.device, m.desired(), true)
	if observed != nil {
		m.observe(*observed, err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot reconcile spanning-tree VLAN", err.Error())
	}
}

func (r *VLANResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m stpVLANModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	vlans, err := readVLANs(ctx, r.device)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read spanning-tree VLANs", err.Error())
		return
	}
	for _, vlan := range vlans {
		if vlan.VLANID == m.VLANID.ValueInt64() {
			m.observe(vlan, m.PersistencePending.ValueBool())
			resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
			return
		}
	}
	// Preserve a failed delete until its save can be retried, even if absent.
	if !m.PersistencePending.ValueBool() {
		resp.State.RemoveResource(ctx)
	}
}

func (r *VLANResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m stpVLANModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := applyVLAN(ctx, r.device, m.desired(), false); err != nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
		resp.Diagnostics.AddError("Cannot delete spanning-tree VLAN", err.Error())
	}
}

func (r *VLANResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil || strconv.FormatInt(id, 10) != req.ID || id < 1 || id > 4094 {
		resp.Diagnostics.AddError("Invalid spanning-tree VLAN identity", "Use the numeric VLAN ID.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, stpVLANModel{ID: types.StringValue(req.ID), VLANID: types.Int64Value(id), Mode: types.StringNull(), Priority: types.Int64Null(), PersistencePending: types.BoolValue(false)})...)
}
