package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	ospfDataSource struct{ device *fastiron.Device }
	ospfModel      struct {
		Areas map[string]ospfAreaStatusModel `tfsdk:"areas"`
	}
	ospfAreaStatusModel struct {
		Interfaces types.Set `tfsdk:"interfaces"`
	}
)

func (d *ospfDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ospf_areas"
}

func (d *ospfDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads default-VRF OSPF areas and interface bindings.", Attributes: map[string]schema.Attribute{
		"areas": schema.MapNestedAttribute{Computed: true, Description: "Areas keyed by canonical dotted area ID.", NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"interfaces": schema.SetAttribute{Computed: true, ElementType: types.StringType, Description: "Canonical interface names bound to the area."},
		}}},
	}}
}

func (d *ospfDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	d.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (d *ospfDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	areas, err := d.device.OSPFAreas(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read OSPF areas", err.Error())
		return
	}
	m := ospfModel{Areas: map[string]ospfAreaStatusModel{}}
	for _, area := range areas {
		interfaces, diags := types.SetValueFrom(ctx, types.StringType, area.Interfaces)
		resp.Diagnostics.Append(diags...)
		m.Areas[area.ID] = ospfAreaStatusModel{Interfaces: interfaces}
	}
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
	}
}
