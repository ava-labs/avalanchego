// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package validators owns the SAE-side validator-uptime lifecycle:
// it wraps an `*uptimetracker.UptimeTracker` and runs the periodic
// `Sync` goroutine that kicks in on `snow.NormalOp`.
// `*subnetevm.VM` holds a single `*Manager` instead of a fistful of
// fields, and its lifecycle methods (`Connected`, `Disconnected`,
// `SetState`, `Shutdown`) forward into this package.
package validators

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/evm/uptimetracker"

	avadb "github.com/ava-labs/avalanchego/database"
	avagovalidators "github.com/ava-labs/avalanchego/snow/validators"
)

// syncFrequency is the period between background calls to
// `*uptimetracker.UptimeTracker.Sync`. Mirrors `graft/subnet-evm/plugin/evm`.
const syncFrequency = 1 * time.Minute

// dbPrefix scopes the on-disk state used by the underlying
// `*uptimetracker.UptimeTracker`.
var dbPrefix = []byte("validators")

// Manager owns the validator-uptime lifecycle for `*subnetevm.VM`.
// Construct via [New]; close via [Manager.Shutdown].
//
// Concurrency: `OnNormalOp` is one-shot (guarded by a `sync.Once`).
// `Connect`/`Disconnect`/`GetUptime` are safe to call from any goroutine
// once construction returns; the underlying tracker provides its own
// serialisation.
type Manager struct {
	tracker *uptimetracker.UptimeTracker
	log     logging.Logger

	// once guards the one-shot transition into NormalOp that kicks
	// off the first `tracker.Sync` and the periodic-sync goroutine.
	once   sync.Once
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New constructs a `Manager` over a fresh `*uptimetracker.UptimeTracker`.
// `db` is scoped to a `validators` sub-prefix so callers can pass the
// raw VM database. `clock` drives the tracker (production: unfaked;
// tests: `*mockable.Clock.Set`).
func New(
	validatorState avagovalidators.State,
	subnetID ids.ID,
	db avadb.Database,
	clock *mockable.Clock,
	log logging.Logger,
) (*Manager, error) {
	tracker, err := uptimetracker.New(
		validatorState,
		subnetID,
		prefixdb.New(dbPrefix, db),
		clock,
	)
	if err != nil {
		return nil, fmt.Errorf("uptimetracker.New: %w", err)
	}
	return &Manager{tracker: tracker, log: log}, nil
}

// Tracker returns the underlying `*uptimetracker.UptimeTracker`.
// Exposed primarily so `*subnetevm.VM` can pass it to the warp
// verifier and the validators-API service (both of which depend on
// their own `UptimeSource` interface that the tracker satisfies
// structurally).
func (m *Manager) Tracker() *uptimetracker.UptimeTracker {
	return m.tracker
}

// GetUptime forwards to the tracker. Provided so tests can call
// `vm.validators.GetUptime` without reaching through `Tracker()`, and
// so `*Manager` itself satisfies the warp/api `UptimeSource`
// interfaces structurally.
func (m *Manager) GetUptime(validationID ids.ID) (time.Duration, time.Time, error) {
	return m.tracker.GetUptime(validationID)
}

// Connect notifies the underlying tracker that `nodeID` is connected.
// Mirrors `*subnetevm.VM.Connected` semantics: must be called BEFORE
// the embedded `*p2p.Network.Connected`.
func (m *Manager) Connect(nodeID ids.NodeID) error {
	if err := m.tracker.Connect(nodeID); err != nil {
		return fmt.Errorf("uptimeTracker.Connect(%s): %w", nodeID, err)
	}
	return nil
}

// Disconnect notifies the underlying tracker that `nodeID` is
// disconnected. Mirrors `*subnetevm.VM.Disconnected`.
func (m *Manager) Disconnect(nodeID ids.NodeID) error {
	if err := m.tracker.Disconnect(nodeID); err != nil {
		return fmt.Errorf("uptimeTracker.Disconnect(%s): %w", nodeID, err)
	}
	return nil
}

// Dispatch performs the one-shot work that needs to happen when[tracker]
// is ready to start tracking validator (typicall on snow.NormalOp):
// - an initial `tracker.Sync`,
// followed by spawning a goroutine that re-syncs every `syncFrequency`.
// The goroutine is cancelled by [Manager.Shutdown].
//
// Subsequent calls are no-ops (the underlying `sync.Once` only fires
// once), matching the legacy `graft/subnet-evm/plugin/evm` behaviour.
func (m *Manager) Dispatch() error {
	var firstSyncErr error
	m.once.Do(func() {
		syncCtx, cancel := context.WithCancel(context.Background())
		m.cancel = cancel

		if err := m.tracker.Sync(syncCtx); err != nil {
			cancel()
			firstSyncErr = fmt.Errorf("initial uptimeTracker.Sync: %w", err)
			return
		}

		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			ticker := time.NewTicker(syncFrequency)
			defer ticker.Stop()

			for {
				select {
				case <-syncCtx.Done():
					return
				case <-ticker.C:
					if err := m.tracker.Sync(syncCtx); err != nil {
						m.log.Error("uptimeTracker.Sync failed", zap.Error(err))
					}
				}
			}
		}()
	})
	return firstSyncErr
}

// Shutdown stops the periodic-sync goroutine (if running) and shuts
// down the underlying tracker. Safe to call even if `Dispatch` was
// never invoked.
func (m *Manager) Shutdown() error {
	if m.cancel != nil {
		m.cancel()
		m.wg.Wait()
	}
	if err := m.tracker.Shutdown(); err != nil {
		return fmt.Errorf("uptimeTracker.Shutdown: %w", err)
	}
	return nil
}
