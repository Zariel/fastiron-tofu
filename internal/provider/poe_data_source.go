package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	poeDataSource      struct{ device *fastiron.Device }
	poeInterfacesModel struct {
		Interfaces map[string]poeStatusModel `tfsdk:"interfaces"`
	}
	poeStatusModel struct {
		Enabled    types.Bool    `tfsdk:"enabled"`
		PowerClass types.Int64   `tfsdk:"power_class"`
		PowerUsed  types.Float64 `tfsdk:"power_used_milliwatts"`
	}
)

func (d *poeDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_poe_interfaces"
}

func (d *poeDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads configured PoE enable state and reported power measurements for interfaces that expose PoE.", Attributes: map[string]schema.Attribute{
		"interfaces": schema.MapNestedAttribute{Computed: true, Description: "PoE interface names mapped to configuration and reported operational measurements.", NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"enabled":               schema.BoolAttribute{Computed: true, Description: "Configured PoE enable state."},
			"power_class":           schema.Int64Attribute{Computed: true, Description: "Reported powered-device class; null when the switch omits it."},
			"power_used_milliwatts": schema.Float64Attribute{Computed: true, Description: "Reported power consumption in milliwatts; null when unavailable."},
		}}},
	}}
}

func (d *poeDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	d.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (d *poeDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	ports, err := d.device.PoEInterfaces(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read PoE interfaces", err.Error())
		return
	}
	state := poeInterfacesModel{Interfaces: map[string]poeStatusModel{}}
	for _, port := range ports {
		if _, duplicate := state.Interfaces[port.Name]; duplicate {
			resp.Diagnostics.AddError("Invalid PoE interface collection", "The switch returned duplicate interface identities.")
			return
		}
		state.Interfaces[port.Name] = poeStatusModel{Enabled: types.BoolValue(port.Enabled), PowerClass: types.Int64PointerValue(port.PowerClass), PowerUsed: types.Float64PointerValue(port.PowerUsedMilliwatts)}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}
