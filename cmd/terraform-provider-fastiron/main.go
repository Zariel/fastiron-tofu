package main

import (
	"context"
	"flag"
	"log"

	framework "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/zariel/fastiron-tofu/internal/provider"
)

var version = "dev"

func main() {
	debug := flag.Bool("debug", false, "Run with debugger attachment support")
	flag.Parse()
	err := providerserver.Serve(context.Background(), func() framework.Provider { return provider.New(version) }, providerserver.ServeOpts{
		Address: "registry.opentofu.org/zariel/fastiron", Debug: *debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
