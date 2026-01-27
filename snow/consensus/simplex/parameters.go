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

type SimplexValidatorInfo struct {
	NodeID ids.NodeID `json:"nodeID" yaml:"nodeID"`

	// PublicKey is the public key of the validator.
	// It should be in the compressed public key format.
	PublicKey []byte `json:"publicKey" yaml:"publicKey"`
}

type Parameters struct {
	MaxNetworkDelay    time.Duration          `json:"maxNetworkDelay"    yaml:"maxNetworkDelay"`
	MaxRebroadcastWait time.Duration          `json:"maxRebroadcastWait" yaml:"maxRebroadcastWait"`
	InitialValidators  []SimplexValidatorInfo `json:"initialValidators"  yaml:"initialValidators"`
}

var DefaultParameters = Parameters{
	MaxNetworkDelay:    5 * time.Second,
	MaxRebroadcastWait: 5 * time.Second,
}

func (p Parameters) Verify() error {
	if p.MaxNetworkDelay <= 0 {
		return fmt.Errorf("%w: maxNetworkDelay must be positive", ErrInvalidParameters)
	}
	if p.MaxRebroadcastWait <= 0 {
		return fmt.Errorf("%w: maxRebroadcastWait must be positive", ErrInvalidParameters)
	}
	if len(p.InitialValidators) == 0 {
		return fmt.Errorf("%w: initialValidators must be non-empty", ErrInvalidParameters)
	}
	return nil
}
