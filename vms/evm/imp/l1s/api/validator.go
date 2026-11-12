// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"github.com/ava-labs/libevm/common/hexutil"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/evm/imp/l1s/validators"
)

// Uptime is a validation's locally observed uptime.
type Uptime struct {
	// Seconds is the duration the validator has been observed online.
	Seconds hexutil.Uint64 `json:"seconds"`
	// LastUpdated is the Unix timestamp, in seconds, at which Seconds was
	// computed.
	LastUpdated hexutil.Uint64 `json:"lastUpdated"`
}

// Validator is the JSON-RPC service for validator data that only this node
// observes.
type Validator struct {
	uptime *validators.Uptime
}

// NewValidator returns a [Validator] reading from `uptime`.
func NewValidator(uptime *validators.Uptime) *Validator {
	return &Validator{uptime: uptime}
}

// Uptime returns the uptime of `validationID`.
func (v *Validator) Uptime(validationID ids.ID) (*Uptime, error) {
	upDuration, lastUpdated, err := v.uptime.GetUptime(validationID)
	if err != nil {
		return nil, err
	}
	return &Uptime{
		Seconds:     hexutil.Uint64(upDuration.Seconds()),
		LastUpdated: hexutil.Uint64(lastUpdated.Unix()), // #nosec G115 -- Unix timestamps of tracked validations are non-negative
	}, nil
}
