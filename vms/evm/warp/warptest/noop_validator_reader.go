// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// warptest exposes common functionality for testing the warp package.
package warptest

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

var _ ValidatorReader = (*NoOpValidatorReader)(nil)

type Validator struct {
	ValidationID   ids.ID     `json:"validationID"`
	NodeID         ids.NodeID `json:"nodeID"`
	Weight         uint64     `json:"weight"`
	StartTimestamp uint64     `json:"startTimestamp"`
	IsActive       bool       `json:"isActive"`
	IsL1Validator  bool       `json:"isL1Validator"`
}

type ValidatorReader interface {
	// GetValidatorAndUptime returns the calculated uptime of the validator specified by validationID
	// and the last updated time.
	// GetValidatorAndUptime holds the VM lock while performing the operation and can be called concurrently.
	GetValidatorAndUptime(validationID ids.ID) (Validator, time.Duration, time.Time, error)
}

type NoOpValidatorReader struct{}

func (NoOpValidatorReader) GetValidatorAndUptime(ids.ID) (Validator, time.Duration, time.Time, error) {
	return Validator{}, 0, time.Time{}, nil
}
