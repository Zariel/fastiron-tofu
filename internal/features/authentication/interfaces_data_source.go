package authentication

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type InterfacesDataSource struct{ device *fastiron.Device }

func (d *InterfacesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_authentication_interfaces"
}

func (d *InterfacesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads configured FlexAuth Ethernet interfaces from native configuration, resolving stale RESTCONF port-control lists. Reports configuration, not client authentication success.", Attributes: map[string]schema.Attribute{
		"interfaces": schema.MapNestedAttribute{Computed: true, Description: "Configured interface settings keyed by canonical Ethernet name.", NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"dot1x_enabled":              schema.BoolAttribute{Computed: true, Description: "Whether dot1x is configured on this port; global initialization is separate."},
			"mac_authentication_enabled": schema.BoolAttribute{Computed: true, Description: "Whether MAC authentication is configured on this port; global initialization is separate."},
			"port_control":               schema.StringAttribute{Computed: true, Description: "auto, force-authorized or force-unauthorized. Native omission means the default force-authorized mode."},
		}}},
	}}
}

func (d *InterfacesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	d.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (d *InterfacesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	interfaces, err := readInterfaces(ctx, d.device)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read authentication interfaces", err.Error())
		return
	}
	type interfaceModel struct {
		Dot1XEnabled bool   `tfsdk:"dot1x_enabled"`
		MACEnabled   bool   `tfsdk:"mac_authentication_enabled"`
		PortControl  string `tfsdk:"port_control"`
	}
	model := struct {
		Interfaces map[string]interfaceModel `tfsdk:"interfaces"`
	}{Interfaces: map[string]interfaceModel{}}
	for name, p := range interfaces {
		model.Interfaces[name] = interfaceModel{p.Dot1XEnabled, p.MACEnabled, p.PortControl}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}
