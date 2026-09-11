package interfaceid

import (
	"errors"
	"regexp"
)

var (
	portPattern = regexp.MustCompile(`^[1-9][0-9]*/[1-9][0-9]*/[1-9][0-9]*$`)
	lagPattern  = regexp.MustCompile(`^lag [1-9][0-9]*$`)
)

// EthernetPort recognizes a canonical stack/slot/port identity.
func EthernetPort(port string) bool { return portPattern.MatchString(port) }

// LAG recognizes a canonical aggregate interface name.
func LAG(name string) bool { return lagPattern.MatchString(name) }

// ValidatePortName checks the description accepted by physical and virtual interfaces.
func ValidatePortName(name string) error {
	if len(name) > 64 {
		return errors.New("port_name must contain at most 64 bytes")
	}
	for _, r := range name {
		if r < 32 || r == 127 {
			return errors.New("port_name cannot contain control characters")
		}
	}
	return nil
}
