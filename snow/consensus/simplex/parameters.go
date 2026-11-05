// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	ErrInvalidParameters = errors.New("simplex parameters must be valid")

	errDuplicateNodeID = errors.New("duplicate nodeID")
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

	nodeIDs := set.NewSet[ids.NodeID](len(p.InitialValidators))
	for _, validator := range p.InitialValidators {
		if nodeIDs.Contains(validator.NodeID) {
			return fmt.Errorf("%w: initialValidators has %w: %s", ErrInvalidParameters, errDuplicateNodeID, validator.NodeID)
		}
		nodeIDs.Add(validator.NodeID)

		if _, err := bls.PublicKeyFromCompressedBytes(validator.PublicKey); err != nil {
			return fmt.Errorf("%w: initialValidators has invalid public key for %s: %w", ErrInvalidParameters, validator.NodeID, err)
		}
	}
	return nil
}
