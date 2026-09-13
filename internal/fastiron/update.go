package fastiron

import (
	"context"
	"errors"
	"net/http"

	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

// Update holds the device lock and retains mutation and verification errors for
// one configuration workflow. It is only valid inside Device.Update's callback.
// Feature code owns readback, convergence, and configuration ownership checks.
type Update struct {
	device *Device
	ctx    context.Context
	err    error
	active bool
}

// Update serializes configuration reconciliation and saves only when every
// mutation, verification, and the workflow itself succeeds. A successful no-op
// workflow still saves, allowing retries after an earlier persistence failure.
func (d *Device) Update(ctx context.Context, reconcile func(*Update) error) error {
	unlock, err := d.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := d.Discover(ctx); err != nil {
		return err
	}

	update := &Update{device: d, ctx: ctx, active: true}
	defer func() { update.active = false }()
	err = reconcile(update)
	if update.err != nil && !errors.Is(err, update.err) {
		err = errors.Join(update.err, err)
	}
	if err != nil {
		return err
	}
	if d.config.Persistence == "after_each_write" {
		return d.save(ctx)
	}
	return nil
}

// Reconcile preserves the observed result even when mutation or persistence fails.
func Reconcile[T any](ctx context.Context, device *Device, reconcile func(*Update) (T, error)) (T, error) {
	var observed T
	err := device.Update(ctx, func(update *Update) error {
		var err error
		observed, err = reconcile(update)
		return err
	})
	return observed, err
}

// REST retains mutation failures even when a caller discards the returned error.
// Readback belongs to the reconciliation callback: FastIron may apply a failed
// request, so callers should observe actual state before returning the failure.
func (u *Update) REST(method, endpoint string, body any) error {
	return u.rest(method, endpoint, body, false)
}

// DeleteIfPresent accepts a missing REST object. The reconciliation callback must
// still verify native absence and preservation; a 404 alone proves neither.
func (u *Update) DeleteIfPresent(endpoint string) error {
	return u.rest(http.MethodDelete, endpoint, nil, true)
}

func (u *Update) rest(method, endpoint string, body any, allowMissing bool) error {
	if !u.active {
		return errors.New("configuration update is no longer active")
	}
	if u.err != nil {
		return u.err
	}
	if method != http.MethodPatch && method != http.MethodPut && method != http.MethodPost && method != http.MethodDelete {
		u.err = errors.New("configuration mutation requires PATCH, PUT, POST, or DELETE")
		return u.err
	}
	u.err = u.device.doREST(u.ctx, method, endpoint, body, nil)
	if allowMissing && errors.Is(u.err, restconf.ErrNotFound) {
		u.err = nil
	}
	return u.err
}
