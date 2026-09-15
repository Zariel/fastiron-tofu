package acl

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode"

	nativeconfig "github.com/zariel/fastiron-tofu/internal/config"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/interfaceid"
)

type accessGroupKey struct{ Interface, Family, Direction string }

type accessGroupView struct {
	ACL       string
	RESTACLs  []string
	Unowned   []string
	Available map[[2]string]bool
}

func (k accessGroupKey) isVLAN() bool {
	value, ok := strings.CutPrefix(k.Interface, "vlan ")
	if !ok {
		return false
	}
	id, err := strconv.ParseInt(value, 10, 64)
	return err == nil && id >= 1 && id <= 4094 && strconv.FormatInt(id, 10) == value
}

func (k accessGroupKey) restType() string {
	return map[string]string{"ip": "ACL_IPV4", "ipv6": "ACL_IPV6", "mac": "ACL_L2"}[k.Family]
}

func (k accessGroupKey) restDirection() string {
	if k.Direction == "out" {
		return "egress"
	}
	return "ingress"
}

func (k accessGroupKey) validate() error {
	ethernet := strings.HasPrefix(k.Interface, "ethernet ") && interfaceid.EthernetPort(strings.TrimPrefix(k.Interface, "ethernet "))
	if !ethernet && !interfaceid.LAG(k.Interface) && !k.isVLAN() {
		return errors.New("ACL bindings require a canonical Ethernet, LAG or VLAN interface name")
	}
	if k.restType() == "" {
		return errors.New("unsupported ACL family")
	}
	if k.Direction != "in" && k.Direction != "out" {
		return errors.New("direction must be in or out")
	}
	if k.Family == "mac" && k.Direction != "in" {
		return errors.New("MAC ACLs support ingress bindings only")
	}
	return nil
}

func validateACLName(name string) error {
	if name == "" || name == "." || name == ".." || strings.Contains(name, "/") || strings.IndexFunc(name, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return errors.New("ACL name must be a nonempty native token without whitespace, control characters, slashes or dot-path components")
	}
	return nil
}

func (k accessGroupKey) endpoint(name string) string {
	direction := k.restDirection()
	return path.Join("/acl/interfaces/interface", url.PathEscape(k.Interface), direction+"-acl-sets", direction+"-acl-set", url.PathEscape(name), k.restType())
}

func (k accessGroupKey) payload(name string) map[string]any {
	item := map[string]any{"set-name": name, "type": k.restType(), "config": map[string]string{"set-name": name, "type": k.restType()}}
	entry := map[string]any{
		"id": k.Interface, "config": map[string]string{"id": k.Interface},
		k.restDirection() + "-acl-sets": map[string]any{k.restDirection() + "-acl-set": []any{item}},
	}
	return map[string]any{"interfaces": map[string]any{"interface": []any{entry}}}
}

func nativeAccessGroup(document *nativeconfig.Document, k accessGroupKey) (accessGroupView, error) {
	view := accessGroupView{Available: map[[2]string]bool{}}
	active := false
	vlan := k.isVLAN()
	for _, command := range document.Commands {
		line := command.Text
		fields := command.Fields
		if len(fields) == 0 || strings.TrimSpace(line) == "!" {
			continue
		}
		if command.Parent == -1 {
			active = line == "interface "+k.Interface
			if vlan {
				active = line == k.Interface || strings.HasPrefix(line, k.Interface+" ")
			}
			// VLAN headers carry separately owned names. Default interface headers
			// can appear or disappear solely because a binding exists.
			if active && vlan {
				view.Unowned = append(view.Unowned, line)
			}
			if active {
				continue
			}
			family, name := nativeACLHeader(fields)
			if family != "" {
				view.Available[[2]string{family, name}] = true
			}
		}
		if !active || len(fields) < 2 || fields[0] != k.Family || fields[1] != "access-group" {
			view.Unowned = append(view.Unowned, line)
			continue
		}
		if len(fields) < 4 {
			return view, errors.New("malformed native ACL binding")
		}
		if fields[3] != k.Direction {
			view.Unowned = append(view.Unowned, line)
			continue
		}
		if len(fields) != 4 {
			return view, errors.New("native ACL binding contains settings outside supported RESTCONF ownership")
		}
		if view.ACL != "" {
			return view, errors.New("multiple native ACLs occupy the same interface, family and direction")
		}
		if err := validateACLName(fields[2]); err != nil {
			return view, err
		}
		view.ACL = fields[2]
	}
	return view, nil
}

func nativeACLHeader(fields []string) (string, string) {
	if len(fields) == 4 && fields[0] == "ip" && fields[1] == "access-list" && (fields[2] == "standard" || fields[2] == "extended") {
		return "ip", fields[3]
	}
	if len(fields) == 3 && (fields[0] == "ipv6" || fields[0] == "mac") && fields[1] == "access-list" {
		return fields[0], fields[2]
	}
	return "", ""
}

func (k accessGroupKey) checkParents(view accessGroupView, name string) error {
	if !view.Available[[2]string{k.Family, name}] {
		return fmt.Errorf("create the %s ACL %q before binding it", k.Family, name)
	}
	if k.isVLAN() {
		for _, line := range view.Unowned {
			if line == k.Interface || strings.HasPrefix(line, k.Interface+" ") {
				return nil
			}
		}
		return fmt.Errorf("ACL binding interface %s: %w", k.Interface, fastiron.ErrNotFound)
	}
	if !interfaceid.LAG(k.Interface) {
		return nil
	}
	// The REST interface inventory can retain a deleted LAG temporarily.
	id := strings.TrimPrefix(k.Interface, "lag ")
	for _, line := range view.Unowned {
		if strings.HasPrefix(line, "lag ") && strings.HasSuffix(line, " id "+id) {
			return nil
		}
	}
	return fmt.Errorf("ACL binding interface %s: %w", k.Interface, fastiron.ErrNotFound)
}

func restAccessGroup(ctx context.Context, d *fastiron.Device, k accessGroupKey) ([]string, error) {
	type entry struct {
		Name   string `json:"set-name"`
		Type   string `json:"type"`
		Config *struct {
			Name string `json:"set-name"`
			Type string `json:"type"`
		} `json:"config"`
	}
	type bindingInterface struct {
		ID     string `json:"id"`
		Config *struct {
			ID string `json:"id"`
		} `json:"config"`
		Ingress struct {
			Entries []entry `json:"ingress-acl-set"`
		} `json:"ingress-acl-sets"`
		Egress struct {
			Entries []entry `json:"egress-acl-set"`
		} `json:"egress-acl-sets"`
	}
	var response struct {
		Interfaces *struct {
			Interface []bindingInterface `json:"interface"`
		} `json:"openconfig-acl:interfaces"`
	}
	if err := d.ReadREST(ctx, "/acl/interfaces", &response); err != nil {
		return nil, err
	}
	if response.Interfaces == nil {
		return nil, errors.New("RESTCONF ACL interface collection is missing its container")
	}
	var names []string
	seen := map[string]bool{}
	for _, i := range response.Interfaces.Interface {
		if i.ID == "" || seen[i.ID] || i.Config == nil || i.Config.ID != i.ID {
			return nil, errors.New("RESTCONF ACL interface collection has incomplete or duplicate identities")
		}
		seen[i.ID] = true
		if i.ID != k.Interface {
			continue
		}
		entries := i.Ingress.Entries
		if k.Direction == "out" {
			entries = i.Egress.Entries
		}
		for _, e := range entries {
			if e.Name == "" || e.Type == "" || e.Config == nil || e.Config.Name != e.Name || e.Config.Type != e.Type {
				return nil, errors.New("RESTCONF ACL binding has incomplete or conflicting identities")
			}
			if strings.TrimPrefix(e.Type, "openconfig-acl:") != k.restType() {
				continue
			}
			if err := validateACLName(e.Name); err != nil {
				return nil, err
			}
			if slices.Contains(names, e.Name) {
				return nil, errors.New("RESTCONF ACL binding contains a duplicate ACL")
			}
			names = append(names, e.Name)
		}
	}
	slices.Sort(names)
	return names, nil
}

func accessGroupConfiguration(ctx context.Context, d *fastiron.Device, k accessGroupKey) (accessGroupView, error) {
	document, err := d.RunningConfig(ctx)
	if err != nil {
		return accessGroupView{}, err
	}

	view, err := nativeAccessGroup(document, k)
	if err != nil {
		return view, err
	}
	view.RESTACLs, err = restAccessGroup(ctx, d, k)
	return view, err
}

func checkAccessGroupInterface(ctx context.Context, d *fastiron.Device, k accessGroupKey) error {
	// VLAN parent checks use the native VLAN stanza in checkParents.
	if k.isVLAN() {
		return nil
	}
	type identity struct {
		Name string `json:"name"`
	}
	type entry struct {
		Name   string    `json:"name"`
		Config *identity `json:"config"`
	}
	var response struct {
		Interfaces *struct {
			Interface []entry `json:"interface"`
		} `json:"openconfig-interfaces:interfaces"`
	}
	if err := d.ReadREST(ctx, "/interfaces", &response); err != nil {
		return err
	}
	if response.Interfaces == nil || len(response.Interfaces.Interface) == 0 {
		return errors.New("RESTCONF interface collection is missing or empty")
	}
	for _, i := range response.Interfaces.Interface {
		if i.Name != k.Interface {
			continue
		}
		if i.Config == nil || i.Config.Name != i.Name {
			return errors.New("RESTCONF interface identity is incomplete")
		}
		return nil
	}
	return fmt.Errorf("ACL binding interface %s: %w", k.Interface, fastiron.ErrNotFound)
}
