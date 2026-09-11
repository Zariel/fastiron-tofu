package address

import (
	"context"
	"errors"
	"net/netip"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	tfresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	resource struct {
		device *fastiron.Device
		ipv6   bool
	}
	addressModel struct {
		ID                 types.String `tfsdk:"id"`
		Interface          types.String `tfsdk:"interface"`
		Address            types.String `tfsdk:"address"`
		PersistencePending types.Bool   `tfsdk:"persistence_pending"`
	}
)

func (r *resource) family() string {
	if r.ipv6 {
		return "ipv6"
	}
	return "ipv4"
}

func (r *resource) Metadata(_ context.Context, req tfresource.MetadataRequest, resp *tfresource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_interface_" + r.family() + "_address"
}

func (r *resource) Schema(_ context.Context, _ tfresource.SchemaRequest, resp *tfresource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Owns one interface IP address and prefix. Address or prefix changes replace this relationship; other addresses remain independently managed. Supports VE and management interfaces.", Attributes: map[string]schema.Attribute{
		"id":                  schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"interface":           schema.StringAttribute{Required: true, Description: "Canonical VE or management interface name.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"address":             schema.StringAttribute{Required: true, Description: "Canonical " + r.family() + " address and prefix length in CIDR syntax, preserving the host address.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"persistence_pending": schema.BoolAttribute{Computed: true, Description: "True when a failed operation still requires reconciliation or persistence."},
	}}
}

func (r *resource) Configure(_ context.Context, req tfresource.ConfigureRequest, resp *tfresource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	r.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (m addressModel) desired() address {
	p, _ := netip.ParsePrefix(m.Address.ValueString())
	return address{Interface: m.Interface.ValueString(), Address: p}
}

func (r *resource) ValidateConfig(ctx context.Context, req tfresource.ValidateConfigRequest, resp *tfresource.ValidateConfigResponse) {
	var m addressModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !m.Interface.IsUnknown() && !m.Interface.IsNull() {
		if err := ValidateInterface(m.Interface.ValueString()); err != nil {
			resp.Diagnostics.AddError("Invalid address interface", err.Error())
		}
	}
	if !m.Address.IsUnknown() && !m.Address.IsNull() {
		p, err := netip.ParsePrefix(m.Address.ValueString())
		if err != nil || p.String() != m.Address.ValueString() || p.Addr().Is6() != r.ipv6 || p.Addr().Is4In6() {
			resp.Diagnostics.AddError("Invalid interface address", "Use a canonical "+r.family()+" CIDR address.")
		}
	}
	if !m.Address.IsUnknown() && !m.Address.IsNull() && !m.Interface.IsUnknown() && !m.Interface.IsNull() {
		if err := validateAddress(m.desired()); err != nil {
			resp.Diagnostics.AddError("Invalid interface address", err.Error())
		}
	}
}

func (r *resource) ModifyPlan(ctx context.Context, req tfresource.ModifyPlanRequest, resp *tfresource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
	var m addressModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || r.device == nil || m.Interface.IsUnknown() || m.Interface.IsNull() {
		return
	}
	if _, err := readAddresses(ctx, r.device, m.Interface.ValueString(), r.ipv6); err != nil && !errors.Is(err, fastiron.ErrNotFound) {
		resp.Diagnostics.AddError("Cannot read address capability", err.Error())
	}
}

func (r *resource) Create(ctx context.Context, req tfresource.CreateRequest, resp *tfresource.CreateResponse) {
	var m addressModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := applyAddress(ctx, r.device, m.desired(), true)
	if observed != nil {
		m.Address = types.StringValue(observed.String())
		m.ID = types.StringValue(m.Interface.ValueString() + "|" + r.family() + "|" + observed.String())
		m.PersistencePending = types.BoolValue(err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot apply interface address", err.Error())
	}
}

func (r *resource) Update(ctx context.Context, req tfresource.UpdateRequest, resp *tfresource.UpdateResponse) {
	var m addressModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := applyAddress(ctx, r.device, m.desired(), true)
	if observed != nil {
		m.Address = types.StringValue(observed.String())
		m.ID = types.StringValue(m.Interface.ValueString() + "|" + r.family() + "|" + observed.String())
		m.PersistencePending = types.BoolValue(err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot reconcile interface address", err.Error())
	}
}

func (r *resource) Read(ctx context.Context, req tfresource.ReadRequest, resp *tfresource.ReadResponse) {
	var m addressModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	addresses, err := readAddresses(ctx, r.device, m.Interface.ValueString(), r.ipv6)
	if err != nil && !errors.Is(err, fastiron.ErrNotFound) {
		resp.Diagnostics.AddError("Cannot read interface addresses", err.Error())
		return
	}
	for _, address := range addresses {
		if address.Addr() == m.desired().Address.Addr() {
			m.Address = types.StringValue(address.String())
			m.ID = types.StringValue(m.Interface.ValueString() + "|" + r.family() + "|" + address.String())
			if m.PersistencePending.IsNull() {
				m.PersistencePending = types.BoolValue(false)
			}
			resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
			return
		}
	}
	// A failed delete must retain its persistence retry even when already absent.
	if !m.PersistencePending.ValueBool() {
		resp.State.RemoveResource(ctx)
	}
}

func (r *resource) Delete(ctx context.Context, req tfresource.DeleteRequest, resp *tfresource.DeleteResponse) {
	var m addressModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := applyAddress(ctx, r.device, m.desired(), false); err != nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
		resp.Diagnostics.AddError("Cannot delete interface address", err.Error())
	}
}

func (r *resource) ImportState(ctx context.Context, req tfresource.ImportStateRequest, resp *tfresource.ImportStateResponse) {
	parts := strings.Split(req.ID, "|")
	if len(parts) != 3 {
		resp.Diagnostics.AddError("Invalid address identity", "Use ve <id>|"+r.family()+"|<CIDR>.")
		return
	}
	p, err := netip.ParsePrefix(parts[2])
	if err != nil || p.String() != parts[2] || p.Addr().Is6() != r.ipv6 || parts[1] != r.family() || validateAddress(address{Interface: parts[0], Address: p}) != nil {
		resp.Diagnostics.AddError("Invalid address identity", "Use ve <id>|"+r.family()+"|<CIDR>.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, addressModel{ID: types.StringValue(req.ID), Interface: types.StringValue(parts[0]), Address: types.StringValue(parts[2]), PersistencePending: types.BoolValue(false)})...)
}

func NewIPv4Resource() *resource { return &resource{} }
func NewIPv6Resource() *resource { return &resource{ipv6: true} }
