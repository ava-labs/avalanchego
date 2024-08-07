// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************
// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"crypto/x509"
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	_ SignedBlock = (*statelessBlock)(nil)

	errUnexpectedProposer = errors.New("expected no proposer but one was provided")
	errMissingProposer    = errors.New("expected proposer but none was provided")
)

type Block interface {
	ID() ids.ID
	ParentID() ids.ID
	Block() []byte
	Bytes() []byte

	initialize(bytes []byte) error
}

type SignedBlock interface {
	Block

	PChainHeight() uint64
	Timestamp() time.Time
	Proposer() ids.NodeID

	Verify(shouldHaveProposer bool, chainID ids.ID) error
}

type statelessUnsignedBlock struct {
	ParentID     ids.ID `serialize:"true"`
	Timestamp    int64  `serialize:"true"`
	PChainHeight uint64 `serialize:"true"`
	Certificate  []byte `serialize:"true"`
	Block        []byte `serialize:"true"`
}

type statelessBlock struct {
	StatelessBlock statelessUnsignedBlock `serialize:"true"`
	Signature      []byte                 `serialize:"true"`

	id        ids.ID
	timestamp time.Time
	cert      *x509.Certificate
	proposer  ids.NodeID
	bytes     []byte
}

func (b *statelessBlock) ID() ids.ID {
	return b.id
}

func (b *statelessBlock) ParentID() ids.ID {
	return b.StatelessBlock.ParentID
}

func (b *statelessBlock) Block() []byte {
	return b.StatelessBlock.Block
}

func (b *statelessBlock) Bytes() []byte {
	return b.bytes
}

func (b *statelessBlock) initialize(bytes []byte) error {
	b.bytes = bytes

	// The serialized form of the block is the unsignedBytes followed by the
	// signature, which is prefixed by a uint32. So, we need to strip off the
	// signature as well as it's length prefix to get the unsigned bytes.
	lenUnsignedBytes := len(bytes) - wrappers.IntLen - len(b.Signature)
	unsignedBytes := bytes[:lenUnsignedBytes]
	b.id = hashing.ComputeHash256Array(unsignedBytes)

	b.timestamp = time.Unix(b.StatelessBlock.Timestamp, 0)
	if len(b.StatelessBlock.Certificate) == 0 {
		return nil
	}

	cert, err := x509.ParseCertificate(b.StatelessBlock.Certificate)
	if err != nil {
		return err
	}

	if err := staking.VerifyCertificate(cert); err != nil {
		return err
	}

	b.cert = cert

	nodeIDBytes, err := secp256k1.RecoverSecp256PublicKey(cert)
	if err != nil {
		return err
	}
	nodeID, err := ids.ToNodeID(nodeIDBytes)
	if err != nil {
		return err
	}
	b.proposer = nodeID
	return nil
}

func (b *statelessBlock) PChainHeight() uint64 {
	return b.StatelessBlock.PChainHeight
}

func (b *statelessBlock) Timestamp() time.Time {
	return b.timestamp
}

func (b *statelessBlock) Proposer() ids.NodeID {
	return b.proposer
}

func (b *statelessBlock) Verify(shouldHaveProposer bool, chainID ids.ID) error {
	if !shouldHaveProposer {
		if len(b.Signature) > 0 || len(b.StatelessBlock.Certificate) > 0 {
			return errUnexpectedProposer
		}
		return nil
	} else if b.cert == nil {
		return errMissingProposer
	}

	header, err := BuildHeader(chainID, b.StatelessBlock.ParentID, b.id)
	if err != nil {
		return err
	}

	headerBytes := header.Bytes()
	return b.cert.CheckSignature(b.cert.SignatureAlgorithm, headerBytes, b.Signature)
}
