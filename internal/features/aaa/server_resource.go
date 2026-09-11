package aaa

import (
	"bytes"
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	serverResource struct {
		device *fastiron.Device
		kind   string
	}
	aaaServerResourceModel struct {
		ID                 types.String `tfsdk:"id"`
		Address            types.String `tfsdk:"address"`
		AuthPort           types.Int64  `tfsdk:"auth_port"`
		AcctPort           types.Int64  `tfsdk:"acct_port"`
		Purpose            types.String `tfsdk:"purpose"`
		Secret             types.String `tfsdk:"secret"`
		PersistencePending types.Bool   `tfsdk:"persistence_pending"`
	}
)

func (r *serverResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_aaa_" + r.kind + "_server"
}

func (r *serverResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	port := int64(49)
	if r.kind == "radius" {
		port = 1812
	}
	attrs := map[string]schema.Attribute{
		"id":                  schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"address":             schema.StringAttribute{Required: true, Description: "Canonical IP address or lowercase DNS hostname. Changing it replaces the server.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"auth_port":           schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(port), Description: "Authentication port: UDP for RADIUS, TCP for TACACS."},
		"acct_port":           schema.Int64Attribute{Computed: true, Description: "Null for TACACS."},
		"purpose":             schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("default"), Description: "default, accounting-only, authentication-only, or TACACS authorization-only."},
		"secret":              schema.StringAttribute{Required: r.kind == "tacacs", Optional: r.kind == "radius", Sensitive: true, Description: "Per-server shared key. Stored as sensitive configuration in state; returned device keys are never exposed. Removing a RADIUS key replaces the server. TACACS currently requires a key."},
		"persistence_pending": schema.BoolAttribute{Computed: true, Description: "True when an operation still requires reconciliation or persistence."},
	}
	if r.kind == "radius" {
		attrs["acct_port"] = schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(1813), Description: "RADIUS accounting UDP port."}
	}
	resp.Schema = schema.Schema{Description: "Owns one AAA server, preserving shared retry settings, authentication policy, and other servers. Requires allow_aaa_changes. Import with " + r.kind + "-server host <address>.", Attributes: attrs}
}

func (r *serverResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	r.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (m aaaServerResourceModel) server(kind string) server {
	return server{Kind: kind, Address: m.Address.ValueString(), AuthPort: m.AuthPort.ValueInt64(), AcctPort: m.AcctPort.ValueInt64(), Purpose: m.Purpose.ValueString()}
}

func (m *aaaServerResourceModel) observe(s server) {
	m.ID = types.StringValue(s.Kind + "-server host " + s.Address)
	m.Address = types.StringValue(s.Address)
	m.AuthPort = types.Int64Value(s.AuthPort)
	m.Purpose = types.StringValue(s.Purpose)
	m.AcctPort = types.Int64Null()
	if s.Kind == "radius" {
		m.AcctPort = types.Int64Value(s.AcctPort)
	}
}

func (m aaaServerResourceModel) secret() *string {
	if m.Secret.IsNull() {
		return nil
	}
	s := m.Secret.ValueString()
	return &s
}

func (r *serverResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
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
	if r.kind == "tacacs" {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("acct_port"), types.Int64Null())...)
	}
	var m aaaServerResourceModel
	resp.Diagnostics.Append(resp.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !m.Address.IsUnknown() && !m.AuthPort.IsUnknown() && !m.AcctPort.IsUnknown() && !m.Purpose.IsUnknown() {
		if err := validateServer(m.server(r.kind)); err != nil {
			resp.Diagnostics.AddError("Invalid AAA server", err.Error())
		}
	}
	if !req.State.Raw.IsNull() && !m.Secret.IsUnknown() {
		var prior aaaServerResourceModel
		resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
		if m.Secret.IsNull() && !prior.Secret.IsNull() {
			resp.RequiresReplace.Append(path.Root("secret"))
		}
		applied, diags := req.Private.GetKey(ctx, "configured_secret")
		resp.Diagnostics.Append(diags...)
		// A failed write can leave configured input in state without establishing
		// that the key was applied. Only a successful apply advances this checksum.
		if !bytes.Equal(applied, configuredSecretChecksum(m.Secret)) {
			resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), types.BoolUnknown())...)
		}
	}
}

func (r *serverResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m aaaServerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := applyServer(ctx, r.device, m.server(r.kind), m.secret(), true)
	if observed != nil {
		m.observe(*observed)
		m.PersistencePending = types.BoolValue(err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot create AAA server", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.Private.SetKey(ctx, "configured_secret", configuredSecretChecksum(m.Secret))...)
}

func (r *serverResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m aaaServerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := applyServer(ctx, r.device, m.server(r.kind), m.secret(), true)
	if observed != nil {
		m.observe(*observed)
	}
	m.PersistencePending = types.BoolValue(err != nil)
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
	if err != nil {
		resp.Diagnostics.AddError("Cannot update AAA server", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.Private.SetKey(ctx, "configured_secret", configuredSecretChecksum(m.Secret))...)
}

func (r *serverResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m aaaServerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	servers, err := readServers(ctx, r.device)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read AAA server", err.Error())
		return
	}
	for _, s := range servers {
		if s.Kind == r.kind && s.Address == m.Address.ValueString() {
			m.observe(s)
			if m.PersistencePending.IsNull() {
				m.PersistencePending = types.BoolValue(false)
			}
			resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
			return
		}
	}
	if !m.PersistencePending.ValueBool() {
		resp.State.RemoveResource(ctx)
	}
}

func (r *serverResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m aaaServerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := applyServer(ctx, r.device, m.server(r.kind), nil, false); err != nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
		resp.Diagnostics.AddError("Cannot delete AAA server", err.Error())
	}
}

func (r *serverResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	prefix := r.kind + "-server host "
	address, ok := strings.CutPrefix(req.ID, prefix)
	s := server{Kind: r.kind, Address: address, AuthPort: 49, Purpose: "default"}
	if r.kind == "radius" {
		s.AuthPort, s.AcctPort = 1812, 1813
	}
	if !ok || validateServer(s) != nil {
		resp.Diagnostics.AddError("Invalid AAA server identity", "Use "+prefix+"<address>.")
		return
	}
	m := aaaServerResourceModel{Secret: types.StringNull(), PersistencePending: types.BoolValue(false)}
	m.observe(s)
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

func NewRADIUSResource() *serverResource { return &serverResource{kind: "radius"} }
func NewTACACSResource() *serverResource { return &serverResource{kind: "tacacs"} }
