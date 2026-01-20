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
	NodeID    ids.NodeID `json:"nodeID"    yaml:"nodeID"`
	PublicKey []byte     `json:"publicKey" yaml:"publicKey"`
}

type Parameters struct {
	MaxProposalWait    time.Duration          `json:"maxProposalWait"    yaml:"maxProposalWait"`
	MaxRebroadcastWait time.Duration          `json:"maxRebroadcastWait" yaml:"maxRebroadcastWait"`
	InitialValidators  []SimplexValidatorInfo `json:"initialValidators"  yaml:"initialValidators"`
}

var DefaultParameters = Parameters{
	MaxProposalWait:    5 * time.Second,
	MaxRebroadcastWait: 5 * time.Second,
}

func (p Parameters) Verify() error {
	if p.MaxProposalWait <= 0 {
		return fmt.Errorf("%w: maxProposalWait must be positive", ErrInvalidParameters)
	}
	if p.MaxRebroadcastWait <= 0 {
		return fmt.Errorf("%w: maxRebroadcastWait must be positive", ErrInvalidParameters)
	}
	if len(p.InitialValidators) == 0 {
		return fmt.Errorf("%w: initialValidators must be non-empty", ErrInvalidParameters)
	}
	return nil
}
