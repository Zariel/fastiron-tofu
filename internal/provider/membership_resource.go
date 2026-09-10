package provider

import (
	"context"
	"fmt"
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
	membershipResource struct{ device *fastiron.Device }
	membershipModel    struct {
		ID                 types.String `tfsdk:"id"`
		VLANID             types.Int64  `tfsdk:"vlan_id"`
		Interface          types.String `tfsdk:"interface"`
		Tagging            types.String `tfsdk:"tagging"`
		PersistencePending types.Bool   `tfsdk:"persistence_pending"`
	}
)

func (r *membershipResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vlan_membership"
}

func (r *membershipResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Owns one tagged or untagged VLAN-to-interface relationship. Other memberships remain independently managed. Removing an untagged membership restores the default VLAN. Import with vlan <id>|<interface>|<tagging>.", Attributes: map[string]schema.Attribute{
		"id":                  schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"vlan_id":             schema.Int64Attribute{Required: true, Description: "VLAN identifier, 2 through 4094.", PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()}},
		"interface":           schema.StringAttribute{Required: true, Description: "Canonical Ethernet or LAG interface name, such as ethernet 1/1/2 or lag 5.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"tagging":             schema.StringAttribute{Required: true, Description: "tagged or untagged. A different existing untagged VLAN must be removed first.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"persistence_pending": schema.BoolAttribute{Computed: true, Description: "True when a failed operation still requires reconciliation or persistence."},
	}}
}

func (r *membershipResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	r.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (m membershipModel) desired() fastiron.VLANMembership {
	return fastiron.VLANMembership{VLANID: m.VLANID.ValueInt64(), Interface: m.Interface.ValueString(), Tagging: m.Tagging.ValueString()}
}

func (m membershipModel) known() bool {
	return !m.VLANID.IsUnknown() && !m.VLANID.IsNull() && !m.Interface.IsUnknown() && !m.Interface.IsNull() && !m.Tagging.IsUnknown() && !m.Tagging.IsNull()
}

func (r *membershipResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m membershipModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !m.known() {
		return
	}
	if err := fastiron.ValidateVLANMembership(m.desired()); err != nil {
		resp.Diagnostics.AddError("Invalid VLAN membership", err.Error())
	}
}

func (r *membershipResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
	var m membershipModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !m.known() || r.device == nil {
		return
	}
	if _, err := r.device.VLANMembership(ctx, m.desired()); err != nil {
		resp.Diagnostics.AddError("Cannot read VLAN membership capability", err.Error())
	}
}

func (r *membershipResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m membershipModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	exists, err := r.device.ApplyVLANMembership(ctx, m.desired(), true)
	if exists {
		m.ID = types.StringValue(fmt.Sprintf("vlan %d|%s|%s", m.VLANID.ValueInt64(), m.Interface.ValueString(), m.Tagging.ValueString()))
		m.PersistencePending = types.BoolValue(err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot apply VLAN membership", err.Error())
	}
}

func (r *membershipResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m membershipModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	exists, err := r.device.ApplyVLANMembership(ctx, m.desired(), true)
	if exists {
		m.PersistencePending = types.BoolValue(err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot reconcile VLAN membership", err.Error())
	}
}

func (r *membershipResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m membershipModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	exists, err := r.device.VLANMembership(ctx, m.desired())
	if err != nil {
		resp.Diagnostics.AddError("Cannot read VLAN membership", err.Error())
		return
	}
	if !exists {
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

func (r *membershipResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m membershipModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.device.ApplyVLANMembership(ctx, m.desired(), false); err != nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
		resp.Diagnostics.AddError("Cannot remove VLAN membership", err.Error())
	}
}

func (r *membershipResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "|")
	if len(parts) != 3 {
		resp.Diagnostics.AddError("Invalid membership identity", "Use vlan <id>|<interface>|<tagging>, with an Ethernet or LAG interface name.")
		return
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(parts[0], "vlan "), 10, 64)
	v := fastiron.VLANMembership{VLANID: id, Interface: parts[1], Tagging: parts[2]}
	if err != nil || parts[0] != fmt.Sprintf("vlan %d", id) || fastiron.ValidateVLANMembership(v) != nil {
		resp.Diagnostics.AddError("Invalid membership identity", "Use vlan <id>|<interface>|<tagging>, with an Ethernet or LAG interface name.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, membershipModel{ID: types.StringValue(req.ID), VLANID: types.Int64Value(id), Interface: types.StringValue(v.Interface), Tagging: types.StringValue(v.Tagging), PersistencePending: types.BoolValue(false)})...)
}
