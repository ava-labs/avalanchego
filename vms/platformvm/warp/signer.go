// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

var (
	_ Signer = (*signer)(nil)

	errWrongSourceChainID = errors.New("wrong SourceChainID")
)

type Signer interface {
	// Returns this node's BLS signature over an unsigned message. If the caller
	// does not have the authority to sign the message, an error will be
	// returned.
	//
	// Assumes the unsigned message is correctly initialized.
	Sign(msg *UnsignedMessage) ([]byte, error)
}

func NewSigner(sk *bls.SecretKey, chainID ids.ID) Signer {
	return &signer{
		sk:      sk,
		chainID: chainID,
	}
}

type signer struct {
	sk      *bls.SecretKey
	chainID ids.ID
}

func (s *signer) Sign(msg *UnsignedMessage) ([]byte, error) {
	if msg.SourceChainID != s.chainID {
		return nil, errWrongSourceChainID
	}

	msgBytes := msg.Bytes()
	sig := bls.Sign(s.sk, msgBytes)
	return bls.SignatureToBytes(sig), nil
}
