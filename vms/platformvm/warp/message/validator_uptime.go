// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

// ValidatorUptime is signed when the ValidationID is known and the validator
// has been up for TotalUptime seconds.
type ValidatorUptime struct {
	payload

	ValidationID ids.ID `serialize:"true"`
	TotalUptime  uint64 `serialize:"true"` // in seconds
}

// ParseValidatorUptime converts a slice of bytes into an initialized ValidatorUptime.
func ParseValidatorUptime(b []byte) (*ValidatorUptime, error) {
	payloadIntf, err := Parse(b)
	if err != nil {
		return nil, err
	}
	payload, ok := payloadIntf.(*ValidatorUptime)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrWrongType, payloadIntf)
	}
	return payload, nil
}
