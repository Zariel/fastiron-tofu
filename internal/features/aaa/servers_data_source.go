package aaa

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	ServersDataSource struct{ device *fastiron.Device }
	aaaServersModel   struct {
		Servers map[string]aaaServerModel `tfsdk:"servers"`
	}
	aaaServerModel struct {
		Kind     types.String `tfsdk:"kind"`
		Address  types.String `tfsdk:"address"`
		AuthPort types.Int64  `tfsdk:"auth_port"`
		AcctPort types.Int64  `tfsdk:"acct_port"`
		Purpose  types.String `tfsdk:"purpose"`
	}
)

func (d *ServersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_aaa_servers"
}

func (d *ServersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads configured RADIUS and TACACS servers without exposing secret material.", Attributes: map[string]schema.Attribute{
		"servers": schema.MapNestedAttribute{Computed: true, Description: "Servers keyed by radius|address or tacacs|address.", NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"kind":      schema.StringAttribute{Computed: true, Description: "radius or tacacs."},
			"address":   schema.StringAttribute{Computed: true, Description: "Configured server address."},
			"auth_port": schema.Int64Attribute{Computed: true, Description: "RADIUS authentication UDP port or TACACS TCP port."},
			"acct_port": schema.Int64Attribute{Computed: true, Description: "RADIUS accounting UDP port; null for TACACS."},
			"purpose":   schema.StringAttribute{Computed: true, Description: "default, authentication-only, accounting-only, or TACACS authorization-only."},
		}}},
	}}
}

func (d *ServersDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	d.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (d *ServersDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	servers, err := readServers(ctx, d.device)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read AAA servers", err.Error())
		return
	}
	m := aaaServersModel{Servers: map[string]aaaServerModel{}}
	for _, s := range servers {
		entry := aaaServerModel{Kind: types.StringValue(s.Kind), Address: types.StringValue(s.Address), AuthPort: types.Int64Value(s.AuthPort), AcctPort: types.Int64Null(), Purpose: types.StringValue(s.Purpose)}
		if s.Kind == "radius" {
			entry.AcctPort = types.Int64Value(s.AcctPort)
		}
		m.Servers[s.Kind+"|"+s.Address] = entry
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}
