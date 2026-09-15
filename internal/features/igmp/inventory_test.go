package igmp

import (
	"maps"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/config/configtest"
)

func TestInventory(t *testing.T) {
	configuration := `ver 09.0.10k
ip multicast active
ip multicast version 3
vlan 4095 name DEFAULT-VLAN by port
vlan 53 name MEDIA by port
 multicast passive
 multicast tracking
vlan 54 by port
 multicast disable-igmp-snoop
 multicast version 2
interface ethernet 1/1/1
 port-name independent
vlan 55 by port
 multicast version 3
end`
	got, err := parseInventory(configtest.Parse(t, configuration))
	if err != nil {
		t.Fatal(err)
	}
	want := map[int64]settings{
		4095: {},
		53:   {Mode: "passive"},
		54:   {Mode: "disabled", Version: 2},
		55:   {Version: 3},
	}
	if !maps.Equal(got, want) {
		t.Fatalf("inventory=%+v", got)
	}
}

func TestInventoryInvalid(t *testing.T) {
	for name, configuration := range map[string]string{
		"missing identity":  "ver 09.0.10k\nvlan\nend",
		"invalid identity":  "ver 09.0.10k\nvlan media by port\nend",
		"out of range":      "ver 09.0.10k\nvlan 4096 by port\nend",
		"duplicate VLAN":    "ver 09.0.10k\nvlan 53 by port\nvlan 53 by port\nend",
		"conflicting modes": "ver 09.0.10k\nvlan 53 by port\n multicast active\n multicast passive\nend",
		"invalid version":   "ver 09.0.10k\nvlan 53 by port\n multicast version 1\nend",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseInventory(configtest.Parse(t, configuration)); err == nil {
				t.Fatal("accepted invalid inventory")
			}
		})
	}
}
