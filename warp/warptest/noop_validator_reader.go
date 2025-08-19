// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// warptest exposes common functionality for testing the warp package.
package warptest

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"

	"github.com/ava-labs/subnet-evm/plugin/evm/validators/interfaces"

	stateinterfaces "github.com/ava-labs/subnet-evm/plugin/evm/validators/state/interfaces"
)

var _ interfaces.ValidatorReader = (*NoOpValidatorReader)(nil)

type NoOpValidatorReader struct{}

func (NoOpValidatorReader) GetValidatorAndUptime(ids.ID) (stateinterfaces.Validator, time.Duration, time.Time, error) {
	return stateinterfaces.Validator{}, 0, time.Time{}, nil
}
