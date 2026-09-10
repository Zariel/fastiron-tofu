package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type lldpResource struct {
	device       *fastiron.Device
	perInterface bool
}

func (r *lldpResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lldp"
	if r.perInterface {
		resp.TypeName += "_interface"
	}
}

func (r *lldpResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Owns LLDP enable state. Omission and destroy restore enabled=true. Global and per-interface settings are independently owned.", Attributes: map[string]schema.Attribute{
		"id":                  schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"enabled":             schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true), Description: "Enable LLDP in this scope. Defaults to true."},
		"persistence_pending": schema.BoolAttribute{Computed: true, Description: "True when a failed operation still requires reconciliation or persistence."},
	}}
	if r.perInterface {
		resp.Schema.Attributes["interface"] = schema.StringAttribute{Required: true, Description: "Canonical Ethernet interface name.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}}
	}
}

func (r *lldpResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	r.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (r *lldpResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	if !r.perInterface {
		return
	}
	var name types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("interface"), &name)...)
	if resp.Diagnostics.HasError() || name.IsUnknown() || name.IsNull() {
		return
	}
	if err := fastiron.ValidateLLDPInterface(name.ValueString()); err != nil {
		resp.Diagnostics.AddError("Invalid LLDP interface", err.Error())
	}
}

func (r *lldpResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
	if r.device == nil {
		return
	}
	var name types.String
	if r.perInterface {
		resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("interface"), &name)...)
		if name.IsUnknown() || name.IsNull() {
			return
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.device.LLDP(ctx, name.ValueString()); err != nil {
		resp.Diagnostics.AddError("Cannot read LLDP capability", err.Error())
	}
}

func (r *lldpResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var enabled types.Bool
	var name types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("enabled"), &enabled)...)
	if r.perInterface {
		resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("interface"), &name)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := r.device.ApplyLLDP(ctx, name.ValueString(), enabled.ValueBool())
	if observed != nil {
		resp.State.Raw = req.Plan.Raw
		id := "lldp"
		if r.perInterface {
			id += "|" + name.ValueString()
		}
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("enabled"), *observed)...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), err != nil)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot apply LLDP", err.Error())
	}
}

func (r *lldpResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var enabled types.Bool
	var name types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("enabled"), &enabled)...)
	if r.perInterface {
		resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("interface"), &name)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := r.device.ApplyLLDP(ctx, name.ValueString(), enabled.ValueBool())
	if observed != nil {
		resp.State.Raw = req.Plan.Raw
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("enabled"), *observed)...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), err != nil)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot update LLDP", err.Error())
	}
}

func (r *lldpResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var name types.String
	var pending types.Bool
	if r.perInterface {
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("interface"), &name)...)
	}
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("persistence_pending"), &pending)...)
	if resp.Diagnostics.HasError() {
		return
	}
	enabled, err := r.device.LLDP(ctx, name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot read LLDP", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("enabled"), enabled)...)
	if pending.IsNull() {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
	}
}

func (r *lldpResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var name types.String
	if r.perInterface {
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("interface"), &name)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.device.ApplyLLDP(ctx, name.ValueString(), true); err != nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
		resp.Diagnostics.AddError("Cannot reset LLDP", err.Error())
	}
}

func (r *lldpResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if r.perInterface {
		name := strings.TrimPrefix(req.ID, "lldp|")
		if req.ID != "lldp|"+name || fastiron.ValidateLLDPInterface(name) != nil {
			resp.Diagnostics.AddError("Invalid LLDP identity", "Use lldp|ethernet <stack>/<slot>/<port>.")
			return
		}
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("interface"), name)...)
	} else if req.ID != "lldp" {
		resp.Diagnostics.AddError("Invalid LLDP identity", "Use lldp for global LLDP.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
