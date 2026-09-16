package ospf

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type singleDataSource struct{ device *fastiron.Device }

type singleQueryModel struct {
	ID         types.String `tfsdk:"id"`
	AreaID     types.String `tfsdk:"area_id"`
	Interfaces types.Set    `tfsdk:"interfaces"`
}

func NewSingleDataSource() *singleDataSource { return &singleDataSource{} }

func (d *singleDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ospf_area"
}

func (d *singleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads one native default-VRF OSPF area and its interface bindings without writing or saving. Requires RESTCONF and SSH access; an absent native area is an error.", Attributes: map[string]schema.Attribute{
		"id":         schema.StringAttribute{Computed: true, Description: "Canonical dotted area identity."},
		"area_id":    schema.StringAttribute{Required: true, Description: "Canonical dotted area identifier, such as 0.0.0.0."},
		"interfaces": schema.SetAttribute{Computed: true, ElementType: types.StringType, Description: "Canonical interface names bound to the area; does not describe neighbor adjacencies."},
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
	if err := validateAreaID(state.AreaID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Invalid OSPF area", err.Error())
		return
	}
	areas, err := readAreas(ctx, d.device)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read OSPF area", err.Error())
		return
	}
	for _, area := range areas {
		if area.ID != state.AreaID.ValueString() {
			continue
		}
		interfaces, diags := types.SetValueFrom(ctx, types.StringType, area.Interfaces)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		state.ID = state.AreaID
		state.Interfaces = interfaces
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
		return
	}
	resp.Diagnostics.AddError("OSPF area not found", "The selected area is absent from native running configuration.")
}
