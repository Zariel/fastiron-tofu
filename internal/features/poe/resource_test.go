package poe

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestPolicyValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		model   model
		invalid bool
	}{
		{name: "omitted defaults", model: model{Interface: types.StringValue("ethernet 1/1/12")}},
		{name: "disabled defaults", model: model{Interface: types.StringValue("ethernet 1/1/12"), Enabled: types.BoolValue(false)}},
		{name: "disabled priority", model: model{Interface: types.StringValue("ethernet 1/1/12"), Enabled: types.BoolValue(false), Priority: types.Int64Value(1)}, invalid: true},
		{name: "exclusive allocation", model: model{Interface: types.StringValue("ethernet 1/1/12"), PowerByClass: types.Int64Value(2), PowerLimit: types.Int64Value(18000)}, invalid: true},
		{name: "unknown enable", model: model{Interface: types.StringValue("ethernet 1/1/12"), Enabled: types.BoolUnknown(), Priority: types.Int64Value(1)}},
		{name: "invalid priority with unknown interface", model: model{Interface: types.StringUnknown(), Priority: types.Int64Value(4)}, invalid: true},
		{name: "capability dependent maximum", model: model{Interface: types.StringValue("ethernet 1/1/12"), PowerLimit: types.Int64Value(95000)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			r := &Resource{}
			var schema resource.SchemaResponse
			r.Schema(ctx, resource.SchemaRequest{}, &schema)
			state := tfsdk.State{Schema: schema.Schema}
			if diagnostics := state.Set(ctx, tc.model); diagnostics.HasError() {
				t.Fatal(diagnostics)
			}

			request := resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: schema.Schema, Raw: state.Raw}}
			var response resource.ValidateConfigResponse
			r.ValidateConfig(ctx, request, &response)
			if response.Diagnostics.HasError() != tc.invalid {
				t.Fatalf("validation diagnostics=%v; invalid=%t", response.Diagnostics, tc.invalid)
			}
		})
	}
}
