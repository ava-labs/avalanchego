// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

type Validator struct {
	UncompressedPublicKeyBytes [96]byte `serialize:"true"`
	Weight                     uint64   `serialize:"true"`
}

// ValidatorSetState is sent by the P-chain.
//
// This message, reports the validator set of a given blockchain ID
// at a specific P-Chain height.
type ValidatorSetState struct {
	payload

	BlockchainID     ids.ID `serialize:"true" json:"blockchainID"`
	PChainHeight     uint64 `serialize:"true" json:"pChainHeight"`
	PChainTimestamp  uint64 `serialize:"true" json:"pChainTimestamp"`
	ValidatorSetHash ids.ID `serialize:"true" json:"validatorSetHash"`
}

// NewValidatorSetState creates a new initialized ValidatorSetState.
func NewValidatorSetState(
	blockchainID ids.ID,
	pChainHeight uint64,
	pChainTimestamp uint64,
	validatorSetHash ids.ID,
) (*ValidatorSetState, error) {
	msg := &ValidatorSetState{
		BlockchainID:     blockchainID,
		PChainHeight:     pChainHeight,
		PChainTimestamp:  pChainTimestamp,
		ValidatorSetHash: validatorSetHash,
	}
	return msg, Initialize(msg)
}

// ParseValidatorSetState parses bytes into an initialized ValidatorSetState.
func ParseValidatorSetState(b []byte) (*ValidatorSetState, error) {
	payloadIntf, err := Parse(b)
	if err != nil {
		return nil, err
	}
	payload, ok := payloadIntf.(*ValidatorSetState)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrWrongType, payloadIntf)
	}
	return payload, nil
}
