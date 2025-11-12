// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"github.com/ava-labs/avalanchego/ids"
)

// ValidatorUptime is signed when the ValidationID is known and the validator
// has been up for TotalUptime seconds.
type ValidatorUptime struct {
	ValidationID ids.ID `serialize:"true"`
	TotalUptime  uint64 `serialize:"true"` // in seconds

	bytes []byte
}

func NewValidatorUptime(validationID ids.ID, totalUptime uint64) (*ValidatorUptime, error) {
	msg := &ValidatorUptime{
		ValidationID: validationID,
		TotalUptime:  totalUptime,
	}
	bytes, err := Codec.Marshal(CodecVersion, msg)
	if err != nil {
		return nil, err
	}
	msg.bytes = bytes
	return msg, nil
}

// ParseValidatorUptime converts a slice of bytes into an initialized ValidatorUptime.
func ParseValidatorUptime(b []byte) (*ValidatorUptime, error) {
	var msg ValidatorUptime
	if _, err := Codec.Unmarshal(b, &msg); err != nil {
		return nil, err
	}
	msg.bytes = b
	return &msg, nil
}

// Bytes returns the binary representation of this payload.
func (v *ValidatorUptime) Bytes() []byte {
	return v.bytes
}
