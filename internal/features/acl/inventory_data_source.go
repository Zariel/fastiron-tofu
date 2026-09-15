package acl

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type inventoryDataSource struct{ device *fastiron.Device }

func NewInventoryDataSource() *inventoryDataSource { return &inventoryDataSource{} }

func (d *inventoryDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_acls"
}

func (d *inventoryDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists ACL identities from native running configuration without taking ownership. Includes empty ACLs and ACLs with rule options outside provider ownership.",
		Attributes: map[string]schema.Attribute{
			"acls": schema.ListNestedAttribute{
				Computed: true, Description: "ACLs sorted by kind, then name. Names can occur in more than one kind.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":   schema.StringAttribute{Computed: true, Description: "Native ACL identity, matching the corresponding resource's import ID."},
					"kind": schema.StringAttribute{Computed: true, Description: "ipv4_standard, ipv4_extended, ipv6 or mac."},
					"name": schema.StringAttribute{Computed: true, Description: "Configured ACL name."},
				}},
			},
		},
	}
}

func (d *inventoryDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	d.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (d *inventoryDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	if _, err := d.device.Discover(ctx); err != nil {
		resp.Diagnostics.AddError("Cannot read ACL inventory", err.Error())
		return
	}
	document, err := d.device.RunningConfig(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read ACL inventory", err.Error())
		return
	}

	identities, err := nativeInventory(document)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read ACL inventory", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, struct {
		ACLs []aclIdentity `tfsdk:"acls"`
	}{ACLs: identities})...)
}
