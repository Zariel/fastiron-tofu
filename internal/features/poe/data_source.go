package poe

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	DataSource         struct{ device *fastiron.Device }
	poeInterfacesModel struct {
		Interfaces map[string]poeStatusModel `tfsdk:"interfaces"`
	}
	poeStatusModel struct {
		Priority       types.Int64   `tfsdk:"priority"`
		PowerByClass   types.Int64   `tfsdk:"power_by_class"`
		PowerLimit     types.Int64   `tfsdk:"power_limit_milliwatts"`
		Enabled        types.Bool    `tfsdk:"enabled"`
		PowerClass     types.Int64   `tfsdk:"power_class"`
		PowerUsed      types.Float64 `tfsdk:"power_used_milliwatts"`
		PowerAllocated types.Float64 `tfsdk:"power_allocated_milliwatts"`
	}
)

func (d *DataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_poe_interfaces"
}

func (d *DataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads configured PoE policy and reported power measurements for interfaces that expose PoE.", Attributes: map[string]schema.Attribute{
		"interfaces": schema.MapNestedAttribute{Computed: true, Description: "PoE interface names mapped to configuration and reported operational measurements.", NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"enabled":                    schema.BoolAttribute{Computed: true, Description: "Configured PoE enable state."},
			"priority":                   schema.Int64Attribute{Computed: true, Description: "Configured power priority: 1 (highest) to 3 (lowest)."},
			"power_by_class":             schema.Int64Attribute{Computed: true, Description: "Configured allocation class, 0 to 4; zero when using an explicit power limit. Distinct from the reported powered-device class."},
			"power_limit_milliwatts":     schema.Int64Attribute{Computed: true, Description: "Configured power limit in milliwatts; zero for class-based allocation."},
			"power_class":                schema.Int64Attribute{Computed: true, Description: "Reported powered-device class; null when the switch omits it."},
			"power_allocated_milliwatts": schema.Float64Attribute{Computed: true, Description: "Reported allocated power in milliwatts; null when unavailable. This measurement can lag configuration changes."},
			"power_used_milliwatts":      schema.Float64Attribute{Computed: true, Description: "Reported power consumption in milliwatts; null when unavailable."},
		}}},
	}}
}

func (d *DataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	d.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (d *DataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	ports, err := readPorts(ctx, d.device)
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
		state.Interfaces[port.Name] = poeStatusModel{Priority: types.Int64Value(port.Priority), PowerByClass: types.Int64Value(port.PowerByClass), PowerLimit: types.Int64Value(port.PowerLimitMilliwatts), Enabled: types.BoolValue(port.Enabled), PowerClass: types.Int64PointerValue(port.PowerClass), PowerUsed: types.Float64PointerValue(port.PowerUsedMilliwatts), PowerAllocated: types.Float64PointerValue(port.PowerAllocatedMilliwatts)}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}
