// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interfaces

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	avalancheuptime "github.com/ava-labs/avalanchego/snow/uptime"
	stateinterfaces "github.com/ava-labs/subnet-evm/plugin/evm/validators/state/interfaces"
)

type ValidatorReader interface {
	// GetValidatorAndUptime returns the calculated uptime of the validator specified by validationID
	// and the last updated time.
	// GetValidatorAndUptime holds the chain context lock while performing the operation and can be called concurrently.
	GetValidatorAndUptime(validationID ids.ID) (stateinterfaces.Validator, time.Duration, time.Time, error)
}

type Manager interface {
	stateinterfaces.State
	avalancheuptime.Manager
	ValidatorReader
	// Initialize initializes the validator manager
	// by syncing the validator state with the current validator set
	// and starting the uptime tracking.
	// Initialize holds the chain context lock while performing the operation.
	Initialize(ctx context.Context) error
	// Shutdown stops the uptime tracking and writes the validator state to the database.
	// Shutdown holds the chain context lock while performing the operation.
	Shutdown() error
	// DispatchSync starts the sync process
	// DispatchSync holds the chain context lock while performing the sync.
	DispatchSync(ctx context.Context)
}
