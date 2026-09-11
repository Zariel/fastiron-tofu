package aaa

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	UsersDataSource struct{ device *fastiron.Device }
	usersModel      struct {
		Users map[string]types.Int64 `tfsdk:"users"`
	}
)

func (d *UsersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_aaa_users"
}

func (d *UsersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads configured local usernames and privilege levels without exposing passwords or password hashes.", Attributes: map[string]schema.Attribute{
		"users": schema.MapAttribute{Computed: true, ElementType: types.Int64Type, Description: "Username to configured privilege level. Native levels include 0 (super user), 4 (port configuration), 5 (read only), 6 (cloud user), and 7 (no syslog access)."},
	}}
}

func (d *UsersDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	d.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (d *UsersDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	users, err := readUsers(ctx, d.device)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read local users", err.Error())
		return
	}
	m := usersModel{Users: map[string]types.Int64{}}
	for _, u := range users {
		m.Users[u.Username] = types.Int64Value(u.Privilege)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}
