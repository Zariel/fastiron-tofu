package acl

import (
	"slices"
	"strings"
)

func referencesACL(lines []string, family ipFamily, name string) bool {
	var pim ipFamily
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		command := strings.Join(fields, " ")
		if line[0] != ' ' && line[0] != '\t' {
			pim = 0
			switch {
			case command == "router pim" || strings.HasPrefix(command, "router pim vrf "):
				pim = ipv4ACL
			case command == "ipv6 router pim" || strings.HasPrefix(command, "ipv6 router pim vrf "):
				pim = ipv6ACL
			}
		}
		// PIM commands omit their address family; same-named ACLs in the
		// other family must not prevent deletion.
		if pim == family && pimReference(fields, name) {
			return true
		}
		if ipReference(command, fields, family, name) {
			return true
		}
	}
	return false
}

func pimReference(fields []string, name string) bool {
	if len(fields) < 2 || fields[len(fields)-1] != name {
		return false
	}
	switch fields[0] {
	case "jp-policy":
		return len(fields) == 2 || len(fields) == 3
	case "rp-address", "anycast-rp":
		return len(fields) == 3
	case "accept-register":
		return len(fields) == 2
	case "ssm-enable":
		return len(fields) == 3 && fields[1] == "range"
	case "slow-path-forwarding":
		return len(fields) == 3 && fields[1] == "filter"
	}
	return false
}

func ipReference(command string, fields []string, family ipFamily, name string) bool {
	ip := "ip"
	prefixes := []string{
		"ip access-group ", "ip access-class ", "access-class ",
		"ssh access-group ", "telnet access-group ", "web access-group ",
		"ip igmp access-group ", "ip multicast-boundary ", "ip pim neighbor-filter ",
	}
	if family == ipv6ACL {
		ip = "ipv6"
		prefixes = []string{
			"ipv6 access-group ", "ipv6 access-class ", "ipv6 traffic-filter ",
			"ipv6 mld access-group ", "ipv6 multicast-boundary ", "ipv6 pim neighbor-filter ",
		}
	}
	for _, prefix := range prefixes {
		if command == prefix+name || strings.HasPrefix(command, prefix+name+" ") {
			return true
		}
	}
	return len(fields) >= 4 && fields[0] == "match" && fields[1] == ip && fields[2] == "address" && fields[3] != "prefix-list" && slices.Contains(fields[3:], name)
}
