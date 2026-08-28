// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package validators owns the SAE-side validator-uptime lifecycle:
// it wraps an `*uptimetracker.UptimeTracker` and runs the periodic
// `Sync` goroutine that kicks in on `snow.NormalOp`.
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
// `*uptimetracker.UptimeTracker.Sync`.
const syncFrequency = 1 * time.Minute

// dbPrefix scopes the on-disk state used by the underlying
// `*uptimetracker.UptimeTracker`.
var dbPrefix = []byte("validators")

// An Uptime owns the validator-uptime lifecycle for `*subnetevm.VM`.
// Construct via [New]; close via [Uptime.Shutdown].
//
// Concurrency: [Uptime.Dispatch] is one-shot (guarded by a `sync.Once`).
// `Connect`/`Disconnect`/`GetUptime` are safe to call from any goroutine
// once construction returns; the underlying tracker provides its own
// serialisation.
type Uptime struct {
	tracker *uptimetracker.UptimeTracker
	log     logging.Logger

	// once guards the one-shot transition into NormalOp that kicks
	// off the first `tracker.Sync` and the periodic-sync goroutine.
	once   sync.Once
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New constructs an [Uptime] over a fresh `*uptimetracker.UptimeTracker`.
// `db` is scoped to a `validators` sub-prefix so callers can pass the
// raw VM database. `clock` drives the tracker (production: unfaked;
// tests: `*mockable.Clock.Set`).
func New(
	validatorState avagovalidators.State,
	subnetID ids.ID,
	db avadb.Database,
	clock *mockable.Clock,
	log logging.Logger,
) (*Uptime, error) {
	tracker, err := uptimetracker.New(
		validatorState,
		subnetID,
		prefixdb.New(dbPrefix, db),
		clock,
	)
	if err != nil {
		return nil, fmt.Errorf("uptimetracker.New: %w", err)
	}
	return &Uptime{tracker: tracker, log: log}, nil
}

// GetUptime forwards to the tracker, satisfying the warp and
// validators-API `UptimeSource` interfaces.
func (m *Uptime) GetUptime(validationID ids.ID) (time.Duration, time.Time, error) {
	return m.tracker.GetUptime(validationID)
}

// Connect notifies the underlying tracker that `nodeID` is connected.
// Mirrors `*subnetevm.VM.Connected` semantics: must be called BEFORE
// the embedded `*p2p.Network.Connected`.
func (m *Uptime) Connect(nodeID ids.NodeID) error {
	if err := m.tracker.Connect(nodeID); err != nil {
		return fmt.Errorf("uptimeTracker.Connect(%s): %w", nodeID, err)
	}
	return nil
}

// Disconnect notifies the underlying tracker that `nodeID` is
// disconnected. Mirrors `*subnetevm.VM.Disconnected`.
func (m *Uptime) Disconnect(nodeID ids.NodeID) error {
	if err := m.tracker.Disconnect(nodeID); err != nil {
		return fmt.Errorf("uptimeTracker.Disconnect(%s): %w", nodeID, err)
	}
	return nil
}

// Dispatch performs the one-shot work that needs to happen when the tracker
// is ready to start tracking validators (typically on snow.NormalOp): an
// initial `tracker.Sync`, followed by spawning a goroutine that re-syncs
// every `syncFrequency`. The goroutine is cancelled by [Uptime.Shutdown].
//
// Subsequent calls are no-ops (the underlying `sync.Once` only fires
// once).
func (m *Uptime) Dispatch() error {
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
func (m *Uptime) Shutdown() error {
	if m.cancel != nil {
		m.cancel()
		m.wg.Wait()
	}
	if err := m.tracker.Shutdown(); err != nil {
		return fmt.Errorf("uptimeTracker.Shutdown: %w", err)
	}
	return nil
}
