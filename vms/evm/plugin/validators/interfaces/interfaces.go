// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interfaces

import (
	"context"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/uptime"

	stateinterfaces "github.com/ava-labs/avalanchego/vms/evm/plugin/validators/state/interfaces"
)

type ValidatorReader interface {
	// GetValidatorAndUptime returns the calculated uptime of the validator specified by validationID
	// and the last updated time.
	// GetValidatorAndUptime holds the VM lock while performing the operation and can be called concurrently.
	GetValidatorAndUptime(validationID ids.ID) (stateinterfaces.Validator, time.Duration, time.Time, error)
}

type Manager interface {
	stateinterfaces.StateReader
	// Initialize initializes the validator manager
	// by syncing the validator state with the current validator set
	// and starting the uptime tracking.
	uptime.Manager

	// Lifecycle management
	Initialize(ctx context.Context) error
	// Shutdown stops the uptime tracking and writes the validator state to the database.
	Shutdown() error
	// DispatchSync starts the sync process
	DispatchSync(ctx context.Context, lock sync.Locker)
}
