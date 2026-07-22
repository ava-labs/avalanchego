// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

var ErrInvalidParameters = errors.New("simplex parameters must be valid")

// MinWaitDuration is the minimum allowed value for both
// [Parameters.MaxNetworkDelay] and [Parameters.MaxRebroadcastWait]. The engine
// ticks at a tenth of the smaller of the two values, so this bound keeps the
// tick interval from being set too fast.
const MinWaitDuration = 500 * time.Millisecond

type ValidatorInfo struct {
	NodeID ids.NodeID `json:"nodeID" yaml:"nodeID"`

	// PublicKey is the public key of the validator.
	// It should be in the compressed public key format.
	PublicKey []byte `json:"publicKey" yaml:"publicKey"`
}

type Parameters struct {
	MaxNetworkDelay    time.Duration   `json:"maxNetworkDelay"    yaml:"maxNetworkDelay"`
	MaxRebroadcastWait time.Duration   `json:"maxRebroadcastWait" yaml:"maxRebroadcastWait"`
	InitialValidators  []ValidatorInfo `json:"initialValidators"  yaml:"initialValidators"`
}

var DefaultParameters = Parameters{
	MaxNetworkDelay:    5 * time.Second,
	MaxRebroadcastWait: 5 * time.Second,
}

func (p Parameters) Verify() error {
	if p.MaxNetworkDelay < MinWaitDuration {
		return fmt.Errorf("%w: maxNetworkDelay must be at least %s", ErrInvalidParameters, MinWaitDuration)
	}
	if p.MaxRebroadcastWait < MinWaitDuration {
		return fmt.Errorf("%w: maxRebroadcastWait must be at least %s", ErrInvalidParameters, MinWaitDuration)
	}
	// TODO: we need to validate InitialValidators contains only unique nodes with valid keys.
	// See: https://github.com/ava-labs/avalanchego/issues/5023
	if len(p.InitialValidators) == 0 {
		return fmt.Errorf("%w: initialValidators must be non-empty", ErrInvalidParameters)
	}
	return nil
}
