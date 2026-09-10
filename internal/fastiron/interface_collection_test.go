package fastiron

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

func TestEmptyInterfaceDatabase(t *testing.T) {
	// A RESTCONF restart returned these shapes despite native physical and
	// logical interfaces still existing. They cannot establish resource absence.
	for _, body := range []string{
		`{"openconfig-interfaces:interfaces":{}}`,
		`{"openconfig-interfaces:interfaces":{"interface":[]}}`,
	} {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, body)
		}))
		t.Cleanup(server.Close)
		device, err := New(Config{Host: server.URL, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.URL, InsecureSkipVerify: true, Timeout: time.Second}})
		if err != nil {
			t.Fatal(err)
		}

		for name, read := range map[string]func() error{
			"Ethernet": func() error { _, err := device.Ethernet(context.Background(), "1/1/7"); return err },
			"VE":       func() error { _, err := device.VE(context.Background(), 53); return err },
			"PoE":      func() error { _, err := device.PoEInterfaces(context.Background()); return err },
		} {
			t.Run(name, func(t *testing.T) {
				if err := read(); err == nil || errors.Is(err, ErrNotFound) {
					t.Fatalf("incomplete database reported as confirmed state: %v", err)
				}
			})
		}
	}
}
