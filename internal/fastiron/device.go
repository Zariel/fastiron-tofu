package fastiron

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/zariel/fastiron-tofu/internal/config"

	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
	"github.com/zariel/fastiron-tofu/internal/transport/ssh"
)

type Config struct {
	AllowAAAChanges              bool
	Host, Transport, Persistence string
	RESTCONF                     *restconf.Config
	SSH                          *ssh.Config
}

type Device struct {
	rest          *restconf.Client
	cli           *ssh.Client
	config        Config
	gate          chan struct{}
	discoveryGate chan struct{}
	capabilities  *Capabilities
}

var hosts sync.Map

func New(cfg Config) (*Device, error) {
	if cfg.Transport != "auto" && cfg.Transport != "restconf" && cfg.Transport != "ssh" {
		return nil, errors.New("transport must be auto, restconf, or ssh")
	}
	if cfg.Persistence != "after_each_write" && cfg.Persistence != "manual" && cfg.Persistence != "never" {
		return nil, errors.New("persistence_mode must be after_each_write, manual, or never")
	}
	d := &Device{config: cfg, discoveryGate: make(chan struct{}, 1)}
	var err error
	if cfg.RESTCONF != nil {
		d.rest, err = restconf.New(*cfg.RESTCONF)
		if err != nil {
			return nil, err
		}
	}
	if cfg.SSH != nil {
		d.cli, err = ssh.New(*cfg.SSH)
		if err != nil {
			return nil, err
		}
	}
	if cfg.Transport == "restconf" && d.rest == nil {
		return nil, errors.New("RESTCONF transport is disabled")
	}
	if cfg.Transport == "ssh" && d.cli == nil {
		return nil, errors.New("SSH transport is disabled")
	}
	if d.rest == nil && d.cli == nil {
		return nil, errors.New("at least one transport must be enabled")
	}
	// Provider aliases in this process share the lock. DNS aliases and separate
	// processes remain the operator's responsibility; OpenTofu owns state locking.
	key := strings.ToLower(strings.TrimSuffix(cfg.Host, "."))
	gate, _ := hosts.LoadOrStore(key, make(chan struct{}, 1))
	d.gate = gate.(chan struct{})
	return d, nil
}

// Lock serializes a complete mutation workflow for this device.
func (d *Device) Lock(ctx context.Context) (func(), error) {
	select {
	case d.gate <- struct{}{}:
		return func() { <-d.gate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type Capabilities struct{ Firmware, BootImage string }

var (
	firmwarePattern = regexp.MustCompile(`(?m)\bSW:\s+Version\s+([0-9]+\.[0-9]+\.[0-9]+[a-z0-9]*)(?:T[0-9]+)?\b`)
	imagePattern    = regexp.MustCompile(`(?m)\blabeled as\s+([A-Z]{3}[0-9]+[a-z0-9]*)\b`)
)

func parseVersion(output string) (Capabilities, error) {
	m := firmwarePattern.FindStringSubmatch(output)
	if len(m) != 2 {
		return Capabilities{}, errors.New("cannot identify active FastIron firmware from show version")
	}
	c := Capabilities{Firmware: m[1]}
	if image := imagePattern.FindStringSubmatch(output); len(image) == 2 {
		c.BootImage = image[1]
	}
	return c, nil
}

func (d *Device) Discover(ctx context.Context) (Capabilities, error) {
	select {
	case d.discoveryGate <- struct{}{}:
		defer func() { <-d.discoveryGate }()
	case <-ctx.Done():
		return Capabilities{}, ctx.Err()
	}
	// Firmware metadata is sampled once per configured provider. Resource reads
	// still obtain current configuration for every refresh and mutation.
	if d.capabilities != nil {
		return *d.capabilities, nil
	}
	if d.cli == nil {
		return Capabilities{}, errors.New("SSH is required to verify active firmware; RESTCONF firmware discovery is not yet verified")
	}
	out, err := d.cli.Run(ctx, true, "show version")
	if err != nil {
		return Capabilities{}, err
	}
	c, err := parseVersion(out[0])
	if err != nil {
		return c, err
	}
	d.capabilities = &c
	return c, nil
}

func (d *Device) Save(ctx context.Context) error {
	if d.config.Persistence == "never" {
		return errors.New("configuration saves are disabled by persistence_mode = never")
	}
	unlock, err := d.Lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	if _, err = d.Discover(ctx); err != nil {
		return err
	}
	return d.save(ctx)
}

func (d *Device) save(ctx context.Context) error {
	if d.cli == nil {
		return errors.New("SSH is required for configuration persistence")
	}
	out, err := d.cli.Run(ctx, true, "write memory")
	if err != nil {
		return err
	}
	if !strings.Contains(out[0], "Write startup-config done.") && !strings.Contains(out[0], "write memory completed. No new config is added.") {
		return errors.New("FastIron did not confirm startup configuration was saved")
	}
	// Verify persisted content independently of the save command's success text.
	out, err = d.cli.Run(ctx, true, "show running-config", "show configuration")
	if err != nil {
		return err
	}
	running, err := NormalizeConfiguration(out[0])
	if err != nil {
		return err
	}
	startup, err := NormalizeConfiguration(out[1])
	if err != nil {
		return err
	}
	if running != startup {
		return errors.New("startup configuration does not match running configuration after save")
	}
	return nil
}

// NormalizeConfiguration validates complete native output and removes display separators.
func NormalizeConfiguration(output string) (string, error) {
	document, err := config.Parse(output)
	if err != nil {
		return "", err
	}
	return document.String(), nil
}

// RESTCONFEnabled reports whether RESTCONF is available under the selected transport.
func (d *Device) RESTCONFEnabled() bool {
	return d.config.Transport != "ssh" && d.rest != nil
}

// DoREST uses the device's configured RESTCONF connection and transport policy.
func (d *Device) DoREST(ctx context.Context, method, endpoint string, body, response any) error {
	if !d.RESTCONFEnabled() {
		return errors.New("RESTCONF transport is unavailable")
	}
	return d.rest.Do(ctx, method, endpoint, body, response)
}

// Persist saves a verified mutation when automatic persistence is configured.
// The caller holds the device lock across mutation, verification and persistence.
func (d *Device) Persist(ctx context.Context) error {
	if d.config.Persistence == "after_each_write" {
		return d.save(ctx)
	}
	return nil
}

// RunningConfig reads a complete native configuration, preserving its formatting.
func (d *Device) RunningConfig(ctx context.Context) (string, error) {
	if d.cli == nil {
		return "", errors.New("SSH is required to read native configuration")
	}
	output, err := d.cli.Run(ctx, true, "show running-config")
	if err != nil {
		return "", err
	}
	if _, err := NormalizeConfiguration(output[0]); err != nil {
		return "", err
	}
	return output[0], nil
}

// RESTCONFTimeout bounds reconciliation of asynchronous RESTCONF state.
func (d *Device) RESTCONFTimeout() time.Duration {
	if d.config.RESTCONF == nil {
		return 0
	}
	return d.config.RESTCONF.Timeout
}

func (d *Device) CheckAAAChanges() error {
	if !d.config.AllowAAAChanges {
		return errors.New("AAA writes require allow_aaa_changes = true")
	}
	return nil
}

// IsTransportAccount identifies accounts used by either configured connection.
func (d *Device) IsTransportAccount(username string) bool {
	return d.config.RESTCONF != nil && strings.EqualFold(username, d.config.RESTCONF.Username) || d.config.SSH != nil && strings.EqualFold(username, d.config.SSH.Username)
}

var ErrNotFound = errors.New("FastIron object not found")
