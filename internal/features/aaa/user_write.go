package aaa

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode"

	nativeconfig "github.com/zariel/fastiron-tofu/internal/config"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

func validateUser(u account) error {
	if len(u.Username) < 1 || len(u.Username) > 48 {
		return errors.New("username must contain 1–48 non-whitespace ASCII characters")
	}
	for _, c := range u.Username {
		if c < 33 || c > 126 {
			return errors.New("username must contain 1–48 non-whitespace ASCII characters")
		}
	}
	if !slices.Contains([]int64{0, 4, 5, 6, 7}, u.Privilege) {
		return errors.New("user privilege must be 0, 4, 5, 6, or 7")
	}
	return nil
}

type nativeUser struct {
	user        account
	hasPassword bool
}

func userConfiguration(output, name string) (*nativeUser, []string, error) {
	document, err := nativeconfig.Parse(output)
	if err != nil {
		return nil, nil, err
	}
	var current *nativeUser
	var neighbors []string
	for _, command := range document.Commands {
		line := command.Text
		if command.Parent != -1 {
			continue
		}
		line = strings.TrimSpace(line)
		f := command.Fields
		if len(f) == 0 {
			continue
		}
		if len(f) >= 3 && f[0] == "no" && f[1] == "username" && f[2] == name {
			return nil, nil, errors.New("native user has access restrictions outside RESTCONF ownership")
		}
		if f[0] != "username" && !(len(f) >= 2 && f[0] == "no" && f[1] == "username") {
			continue
		}
		if len(f) < 2 || f[0] != "username" || f[1] != name {
			neighbors = append(neighbors, line)
			continue
		}
		if current != nil {
			return nil, nil, errors.New("native user has additional configuration outside RESTCONF ownership")
		}
		current = &nativeUser{user: account{Username: name}}
		seen := map[string]bool{}
		for i := 2; i < len(f); i++ {
			field := f[i]
			if seen[field] || i+1 == len(f) {
				return nil, nil, errors.New("native user has incomplete or duplicate options")
			}
			seen[field] = true
			i++
			switch field {
			case "privilege":
				level, err := strconv.ParseInt(f[i], 10, 64)
				if err != nil {
					return nil, nil, errors.New("native user has invalid privilege")
				}
				current.user.Privilege = level
			case "password":
				current.hasPassword = true
			default:
				return nil, nil, errors.New("native user has settings outside RESTCONF ownership")
			}
		}
		if err := validateUser(current.user); err != nil {
			return nil, nil, errors.New("native user has invalid identity or privilege")
		}
	}
	return current, neighbors, nil
}

func applyUser(ctx context.Context, d *fastiron.Device, u account, password string, present bool) (*account, error) {
	if err := d.CheckAAAChanges(); err != nil {
		return nil, err
	}
	if err := validateUser(u); err != nil {
		return nil, err
	}
	if d.IsTransportAccount(u.Username) {
		return nil, errors.New("cannot modify a provider transport account; use a separate administrative account")
	}
	if present && (len(password) < 1 || len(password) > 48 || strings.IndexFunc(password, unicode.IsControl) >= 0) {
		return nil, errors.New("user password must contain 1–48 bytes without control characters")
	}
	return fastiron.Reconcile(ctx, d, func(update *fastiron.Update) (*account, error) {
		users, err := readUsers(ctx, d)
		if err != nil {
			return nil, err
		}
		output, err := d.RunningConfig(ctx)
		if err != nil {
			return nil, err
		}
		native, neighbors, err := userConfiguration(output, u.Username)
		if err != nil {
			return nil, err
		}
		var current *account
		for _, user := range users {
			if user.Username == u.Username {
				current = &user
			}
		}
		if (current == nil) != (native == nil) || current != nil && *current != native.user {
			return current, errors.New("native and RESTCONF user configuration disagree; retry after synchronization")
		}
		if present || current != nil {
			document, err := nativeconfig.Parse(output)
			if err != nil {
				return current, err
			}
			for _, command := range document.Commands {
				if command.Parent == -1 && command.Text == "service local-user-protection" {
					return current, errors.New("local-user protection requires an authenticated user-update operation not currently supported")
				}
			}
			endpoint := "/system/aaa/authentication/users"
			method := http.MethodDelete
			var body any
			if present {
				method = http.MethodPatch
				// Keep password and privilege in one complete native user update. Never
				// replay the password hash returned by discovery as a plaintext password.
				body = map[string]any{"users": map[string]any{"user": []any{map[string]any{"username": u.Username, "config": map[string]any{"username": u.Username, "password": password, "icx-openconfig-aaa-aug:privilege": u.Privilege}}}}}
			} else {
				endpoint = path.Join(endpoint, "user="+url.PathEscape(u.Username))
			}
			writeErr := update.REST(method, endpoint, body)
			observed, readErr := readUsers(ctx, d)
			if readErr != nil {
				return current, errors.Join(writeErr, readErr)
			}
			current = nil
			for _, user := range observed {
				if user.Username == u.Username {
					current = &user
				}
			}
			output, nativeErr := d.RunningConfig(ctx)
			if nativeErr != nil {
				return current, errors.Join(writeErr, nativeErr)
			}
			native, after, parseErr := userConfiguration(output, u.Username)
			if parseErr != nil {
				return current, errors.Join(writeErr, parseErr)
			}
			if !slices.Equal(neighbors, after) {
				return current, errors.Join(writeErr, errors.New("user operation changed unrelated native accounts"))
			}
			if present && (current == nil || *current != u || native == nil || native.user != u || !native.hasPassword) || !present && (current != nil || native != nil) {
				return current, errors.Join(writeErr, errors.New("user configuration did not converge"))
			}
			// Account metadata cannot resolve an ambiguous password write failure.
			if writeErr != nil {
				return current, writeErr
			}
		}
		return current, nil
	})
}
