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
	// GetValidatorAndUptime returns the uptime of the validator specified by validationID
	GetValidatorAndUptime(validationID ids.ID) (stateinterfaces.Validator, time.Duration, time.Time, error)
}

type Manager interface {
	stateinterfaces.State
	avalancheuptime.Manager
	ValidatorReader

	// Sync updates the validator set managed
	// by the manager
	Sync(ctx context.Context) error
	// DispatchSync starts the sync process
	DispatchSync(ctx context.Context)
}
