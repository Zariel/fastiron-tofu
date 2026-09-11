package aaa

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	PolicyResource struct{ device *fastiron.Device }
	aaaModel       struct {
		ID                 types.String `tfsdk:"id"`
		LoginMethods       types.List   `tfsdk:"login_methods"`
		Dot1XDefault       types.String `tfsdk:"dot1x_default"`
		CoAEnabled         types.Bool   `tfsdk:"coa_enabled"`
		CoAIgnore          types.Set    `tfsdk:"coa_ignore"`
		PersistencePending types.Bool   `tfsdk:"persistence_pending"`
	}
)

func (r *PolicyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_aaa"
}

func (r *PolicyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Owns login authentication methods, dot1x default authentication and CoA policy. Requires allow_aaa_changes. Destroy restores local login, removes the dot1x policy and disables CoA with no ignored actions. Import with aaa.", Attributes: map[string]schema.Attribute{
		"id":                  schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"login_methods":       schema.ListAttribute{Required: true, ElementType: types.StringType, Description: "One to three distinct local, radius or tacacs+ methods in authentication attempt order."},
		"dot1x_default":       schema.StringAttribute{Optional: true, Description: "radius or none. Omission removes the native policy; explicit none authenticates without checking client credentials."},
		"coa_enabled":         schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), Description: "Enable RADIUS Change of Authorization. Defaults to false."},
		"coa_ignore":          schema.SetAttribute{Optional: true, Computed: true, ElementType: types.StringType, Default: setdefault.StaticValue(types.SetValueMust(types.StringType, nil)), Description: "Ignored CoA actions: disable-port, dm-request, flip-port, modify-acl, reauth-host. Defaults to empty."},
		"persistence_pending": schema.BoolAttribute{Computed: true, Description: "True when an operation requires reconciliation or persistence."},
	}}
}

func (r *PolicyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	r.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (m aaaModel) policy(ctx context.Context) (policy, diag.Diagnostics) {
	p := policy{Dot1XDefault: m.Dot1XDefault.ValueString(), CoAEnabled: m.CoAEnabled.ValueBool()}
	diags := m.LoginMethods.ElementsAs(ctx, &p.LoginMethods, false)
	if !m.Dot1XDefault.IsNull() && m.Dot1XDefault.ValueString() == "" {
		diags.AddError("Invalid dot1x policy", "dot1x_default must be radius or none; omit it to remove the native policy.")
	}

	diags.Append(m.CoAIgnore.ElementsAs(ctx, &p.CoAIgnore, false)...)
	return p, diags
}

func (m *aaaModel) observe(ctx context.Context, p policy) diag.Diagnostics {
	m.ID = types.StringValue("aaa")
	m.Dot1XDefault = types.StringNull()
	if p.Dot1XDefault != "" {
		m.Dot1XDefault = types.StringValue(p.Dot1XDefault)
	}
	m.CoAEnabled = types.BoolValue(p.CoAEnabled)
	var diags, more diag.Diagnostics
	m.LoginMethods, diags = types.ListValueFrom(ctx, types.StringType, p.LoginMethods)
	ignored := p.CoAIgnore
	if ignored == nil {
		ignored = []string{}
	}
	m.CoAIgnore, more = types.SetValueFrom(ctx, types.StringType, ignored)
	diags.Append(more...)
	return diags
}

func (r *PolicyResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if r.device != nil {
		if err := r.device.CheckAAAChanges(); err != nil {
			resp.Diagnostics.AddError("AAA changes disabled", err.Error())
			return
		}
	}
	if req.Plan.Raw.IsNull() {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
	var m aaaModel
	resp.Diagnostics.Append(resp.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || m.LoginMethods.IsUnknown() || m.Dot1XDefault.IsUnknown() || m.CoAEnabled.IsUnknown() || m.CoAIgnore.IsUnknown() {
		return
	}
	for _, value := range m.LoginMethods.Elements() {
		if value.IsUnknown() {
			return
		}
	}
	for _, value := range m.CoAIgnore.Elements() {
		if value.IsUnknown() {
			return
		}
	}
	p, diags := m.policy(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := validatePolicy(p); err != nil {
		resp.Diagnostics.AddError("Invalid AAA policy", err.Error())
	}
}

func (r *PolicyResource) apply(ctx context.Context, m aaaModel) (*aaaModel, diag.Diagnostics) {
	p, diags := m.policy(ctx)
	if diags.HasError() {
		return nil, diags
	}
	observed, err := applyPolicy(ctx, r.device, p)
	if err != nil {
		diags.AddError("Cannot apply AAA policy", err.Error())
	}
	if observed == nil {
		return nil, diags
	}
	diags.Append(m.observe(ctx, *observed)...)
	m.PersistencePending = types.BoolValue(err != nil)
	return &m, diags
}

func (r *PolicyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m aaaModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, diags := r.apply(ctx, m)
	resp.Diagnostics.Append(diags...)
	if observed != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, observed)...)
	}
}

func (r *PolicyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m aaaModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, diags := r.apply(ctx, m)
	resp.Diagnostics.Append(diags...)
	if observed != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, observed)...)
	}
}

func (r *PolicyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m aaaModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := readConfiguration(ctx, r.device)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read AAA configuration", err.Error())
		return
	}
	resp.Diagnostics.Append(m.observe(ctx, *p)...)
	if m.PersistencePending.IsNull() {
		m.PersistencePending = types.BoolValue(false)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

func (r *PolicyResource) Delete(ctx context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	if _, err := applyPolicy(ctx, r.device, policy{LoginMethods: []string{"local"}}); err != nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
		resp.Diagnostics.AddError("Cannot reset AAA policy", err.Error())
	}
}

func (r *PolicyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != "aaa" {
		resp.Diagnostics.AddError("Invalid AAA identity", "Use aaa.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), "aaa")...)
}
