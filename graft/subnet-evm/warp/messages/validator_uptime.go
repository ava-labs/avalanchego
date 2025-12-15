// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package messages

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

// ValidatorUptime is signed when the ValidationID is known and the validator
// has been up for TotalUptime seconds.
type ValidatorUptime struct {
	ValidationID ids.ID `serialize:"true"`
	TotalUptime  uint64 `serialize:"true"` // in seconds

	bytes []byte
}

// NewValidatorUptime creates a new *ValidatorUptime and initializes it.
func NewValidatorUptime(validationID ids.ID, totalUptime uint64) (*ValidatorUptime, error) {
	bhp := &ValidatorUptime{
		ValidationID: validationID,
		TotalUptime:  totalUptime,
	}
	return bhp, initialize(bhp)
}

// ParseValidatorUptime converts a slice of bytes into an initialized ValidatorUptime.
func ParseValidatorUptime(b []byte) (*ValidatorUptime, error) {
	payloadIntf, err := Parse(b)
	if err != nil {
		return nil, err
	}
	payload, ok := payloadIntf.(*ValidatorUptime)
	if !ok {
		return nil, fmt.Errorf("%w: %T", errWrongType, payloadIntf)
	}
	return payload, nil
}

// Bytes returns the binary representation of this payload. It assumes that the
// payload is initialized from either NewValidatorUptime or Parse.
func (b *ValidatorUptime) Bytes() []byte {
	return b.bytes
}

func (b *ValidatorUptime) initialize(bytes []byte) {
	b.bytes = bytes
}
