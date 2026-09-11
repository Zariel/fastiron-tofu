package interfaceid

import "regexp"

var (
	portPattern = regexp.MustCompile(`^[1-9][0-9]*/[1-9][0-9]*/[1-9][0-9]*$`)
	lagPattern  = regexp.MustCompile(`^lag [1-9][0-9]*$`)
)

// EthernetPort recognizes a canonical stack/slot/port identity.
func EthernetPort(port string) bool { return portPattern.MatchString(port) }

// LAG recognizes a canonical aggregate interface name.
func LAG(name string) bool { return lagPattern.MatchString(name) }
