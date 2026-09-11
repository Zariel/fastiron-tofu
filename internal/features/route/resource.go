package route

import (
	"context"
	"net/netip"
	"strings"

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
	Resource   struct{ device *fastiron.Device }
	routeModel struct {
		ID                 types.String `tfsdk:"id"`
		Prefix             types.String `tfsdk:"prefix"`
		NextHop            types.String `tfsdk:"next_hop"`
		Distance           types.Int64  `tfsdk:"distance"`
		PersistencePending types.Bool   `tfsdk:"persistence_pending"`
	}
)

func (r *Resource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ip_route"
}

func (r *Resource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Owns one IPv4 prefix and next-hop relationship in the default VRF. Other next hops remain independently managed.", Attributes: map[string]schema.Attribute{
		"id":                  schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"prefix":              schema.StringAttribute{Required: true, Description: "Canonical IPv4 destination network in CIDR notation.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"next_hop":            schema.StringAttribute{Required: true, Description: "Canonical IPv4 gateway address.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"distance":            schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(1), Description: "Administrative distance, 1–255. Changes replace this route relationship.", PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()}},
		"persistence_pending": schema.BoolAttribute{Computed: true, Description: "True when a failed operation still requires reconciliation or persistence."},
	}}
}

func (r *Resource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	r.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (m routeModel) desired() route {
	prefix, _ := netip.ParsePrefix(m.Prefix.ValueString())
	nextHop, _ := netip.ParseAddr(m.NextHop.ValueString())
	return route{Prefix: prefix, NextHop: nextHop, Distance: m.Distance.ValueInt64()}
}

func (r *Resource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m routeModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !m.Prefix.IsUnknown() && !m.Prefix.IsNull() {
		prefix, err := netip.ParsePrefix(m.Prefix.ValueString())
		if err != nil || !prefix.Addr().Is4() || prefix.Masked().String() != m.Prefix.ValueString() || prefix.Addr().IsMulticast() {
			resp.Diagnostics.AddError("Invalid route prefix", "Use a canonical IPv4 network in CIDR notation.")
		}
	}
	if !m.NextHop.IsUnknown() && !m.NextHop.IsNull() {
		ip, err := netip.ParseAddr(m.NextHop.ValueString())
		if err != nil || !ip.Is4() || ip.String() != m.NextHop.ValueString() || ip.IsUnspecified() || ip.IsMulticast() {
			resp.Diagnostics.AddError("Invalid next hop", "Use a canonical unicast IPv4 gateway address.")
		}
	}
	if !m.Distance.IsUnknown() && !m.Distance.IsNull() && (m.Distance.ValueInt64() < 1 || m.Distance.ValueInt64() > 255) {
		resp.Diagnostics.AddError("Invalid distance", "Administrative distance must be between 1 and 255.")
	}
}

func (r *Resource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
	if r.device != nil {
		if _, err := readRoutes(ctx, r.device); err != nil {
			resp.Diagnostics.AddError("Cannot read static route capability", err.Error())
		}
	}
}

func (m *routeModel) observe(route route, pending bool) {
	m.Prefix = types.StringValue(route.Prefix.String())
	m.NextHop = types.StringValue(route.NextHop.String())
	m.Distance = types.Int64Value(route.Distance)
	m.ID = types.StringValue(route.Prefix.String() + "|" + route.NextHop.String())
	m.PersistencePending = types.BoolValue(pending)
}

func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m routeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := applyRoute(ctx, r.device, m.desired(), true)
	if observed != nil {
		m.observe(*observed, err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot apply static route", err.Error())
	}
}

func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m routeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := applyRoute(ctx, r.device, m.desired(), true)
	if observed != nil {
		m.observe(*observed, err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot reconcile static route", err.Error())
	}
}

func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m routeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	routes, err := readRoutes(ctx, r.device)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read static routes", err.Error())
		return
	}
	for _, route := range routes {
		if route.Prefix == m.desired().Prefix && route.NextHop == m.desired().NextHop {
			m.observe(route, m.PersistencePending.ValueBool())
			resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
			return
		}
	}
	// Preserve a failed delete until its save can be retried, even if absent.
	if !m.PersistencePending.ValueBool() {
		resp.State.RemoveResource(ctx)
	}
}

func (r *Resource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m routeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := applyRoute(ctx, r.device, m.desired(), false); err != nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
		resp.Diagnostics.AddError("Cannot delete static route", err.Error())
	}
}

func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	prefix, nextHop, ok := strings.Cut(req.ID, "|")
	p, prefixErr := netip.ParsePrefix(prefix)
	ip, ipErr := netip.ParseAddr(nextHop)
	v := route{Prefix: p, NextHop: ip, Distance: 1}
	if !ok || prefixErr != nil || ipErr != nil || p.String() != prefix || ip.String() != nextHop || validateRoute(v) != nil {
		resp.Diagnostics.AddError("Invalid route identity", "Use <IPv4 prefix>|<IPv4 next hop>.")
		return
	}
	var m routeModel
	m.observe(v, false)
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}
