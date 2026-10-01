// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	ErrInvalidParameters = errors.New("simplex parameters must be valid")

	errMaxNetworkDelayNotPositive    = errors.New("maxNetworkDelay must be positive")
	errMaxRebroadcastWaitNotPositive = errors.New("maxRebroadcastWait must be positive")
	errInitialValidatorsEmpty        = errors.New("initialValidators must be non-empty")
)

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

// Verify returns nil if the parameters are valid.
//
// If any condition is violated, the returned error is the [errors.Join] of
// one error per violated condition, each wrapping [ErrInvalidParameters],
// rather than only the first violation.
func (p Parameters) Verify() error {
	var errs []error
	if p.MaxNetworkDelay <= 0 {
		errs = append(errs, fmt.Errorf("%w: %w", ErrInvalidParameters, errMaxNetworkDelayNotPositive))
	}
	if p.MaxRebroadcastWait <= 0 {
		errs = append(errs, fmt.Errorf("%w: %w", ErrInvalidParameters, errMaxRebroadcastWaitNotPositive))
	}
	// TODO: we need to validate InitialValidators contains only unique nodes with valid keys.
	// See: https://github.com/ava-labs/avalanchego/issues/5023
	if len(p.InitialValidators) == 0 {
		errs = append(errs, fmt.Errorf("%w: %w", ErrInvalidParameters, errInitialValidatorsEmpty))
	}
	return errors.Join(errs...)
}
