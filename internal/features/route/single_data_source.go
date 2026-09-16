package route

import (
	"context"
	"net/netip"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type singleDataSource struct{ device *fastiron.Device }

type singleQueryModel struct {
	ID       types.String `tfsdk:"id"`
	Prefix   types.String `tfsdk:"prefix"`
	NextHop  types.String `tfsdk:"next_hop"`
	Distance types.Int64  `tfsdk:"distance"`
}

func NewSingleDataSource() *singleDataSource { return &singleDataSource{} }

var _ datasource.DataSourceWithConfigure = (*singleDataSource)(nil)

func (d *singleDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_static_route"
}

func (d *singleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads one native default-VRF IPv4 gateway route without writing or saving. Requires RESTCONF and SSH access; an absent native route is an error.", Attributes: map[string]schema.Attribute{
		"id":       schema.StringAttribute{Computed: true, Description: "Canonical identity: <prefix>|<next hop>."},
		"prefix":   schema.StringAttribute{Required: true, Description: "Canonical IPv4 destination network in CIDR notation."},
		"next_hop": schema.StringAttribute{Required: true, Description: "Canonical IPv4 gateway address."},
		"distance": schema.Int64Attribute{Computed: true, Description: "Native administrative distance; does not indicate forwarding-table presence."},
	}}
}

func (d *singleDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	device, ok := req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
		return
	}
	d.device = device
}

func (d *singleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state singleQueryModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	prefix, prefixErr := netip.ParsePrefix(state.Prefix.ValueString())
	gateway, gatewayErr := netip.ParseAddr(state.NextHop.ValueString())
	selected := route{Prefix: prefix, NextHop: gateway, Distance: 1}
	if prefixErr != nil || gatewayErr != nil || prefix.String() != state.Prefix.ValueString() || gateway.String() != state.NextHop.ValueString() || validateRoute(selected) != nil {
		resp.Diagnostics.AddError("Invalid route identity", "Use a canonical IPv4 network prefix and unicast IPv4 next_hop.")
		return
	}
	routes, _, err := readRoutes(ctx, d.device)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read static route", err.Error())
		return
	}
	for _, current := range routes {
		if current.Prefix != prefix || current.NextHop != gateway {
			continue
		}
		state.ID = types.StringValue(prefix.String() + "|" + gateway.String())
		state.Distance = types.Int64Value(current.Distance)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
		return
	}
	resp.Diagnostics.AddError("Static route not found", "The selected prefix and gateway are absent from native running configuration.")
}
