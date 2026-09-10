package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	routesDataSource struct{ device *fastiron.Device }
	routesModel      struct {
		Routes map[string]routeStatusModel `tfsdk:"routes"`
	}
	routeStatusModel struct {
		Prefix   types.String `tfsdk:"prefix"`
		NextHop  types.String `tfsdk:"next_hop"`
		Distance types.Int64  `tfsdk:"distance"`
	}
)

func (d *routesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_static_routes"
}

func (d *routesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads default-VRF IPv4 static routes with IP gateways.", Attributes: map[string]schema.Attribute{
		"routes": schema.MapNestedAttribute{Computed: true, Description: "Routes keyed by <prefix>|<next hop>.", NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"prefix":   schema.StringAttribute{Computed: true},
			"next_hop": schema.StringAttribute{Computed: true},
			"distance": schema.Int64Attribute{Computed: true, Description: "Administrative distance."},
		}}},
	}}
}

func (d *routesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	d.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (d *routesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	routes, err := d.device.StaticRoutes(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read static routes", err.Error())
		return
	}
	m := routesModel{Routes: map[string]routeStatusModel{}}
	for _, route := range routes {
		m.Routes[route.Prefix.String()+"|"+route.NextHop.String()] = routeStatusModel{Prefix: types.StringValue(route.Prefix.String()), NextHop: types.StringValue(route.NextHop.String()), Distance: types.Int64Value(route.Distance)}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}
