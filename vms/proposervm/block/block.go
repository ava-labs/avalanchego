// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	_ SignedBlock = (*statelessBlock)(nil)

	errUnexpectedSignature = errors.New("signature provided when none was expected")
	errInvalidCertificate  = errors.New("invalid certificate")
)

type Block interface {
	ID() ids.ID
	ParentID() ids.ID
	Block() []byte
	Bytes() []byte

	initialize(bytes []byte) error
	verify(chainID ids.ID) error
}

type SignedBlock interface {
	Block

	PChainHeight() uint64
	Timestamp() time.Time

	// Proposer returns the ID of the node that proposed this block. If no node
	// signed this block, [ids.EmptyNodeID] will be returned.
	Proposer() ids.NodeID
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
	cert      *staking.Certificate
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

	var err error
	b.cert, err = staking.ParseCertificate(b.StatelessBlock.Certificate)
	if err != nil {
		return fmt.Errorf("%w: %w", errInvalidCertificate, err)
	}

	b.proposer = ids.NodeIDFromCert(b.cert)
	return nil
}

func (b *statelessBlock) verify(chainID ids.ID) error {
	if len(b.StatelessBlock.Certificate) == 0 {
		if len(b.Signature) > 0 {
			return errUnexpectedSignature
		}
		return nil
	}

	header, err := BuildHeader(chainID, b.StatelessBlock.ParentID, b.id)
	if err != nil {
		return err
	}

	headerBytes := header.Bytes()
	return staking.CheckSignature(
		b.cert,
		headerBytes,
		b.Signature,
	)
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
