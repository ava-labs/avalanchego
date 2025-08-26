// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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
	_ SignedBlock = (*statelessGraniteBlock)(nil)

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
	PChainEpoch() PChainEpoch
	Timestamp() time.Time

	// Proposer returns the ID of the node that proposed this block. If no node
	// signed this block, [ids.EmptyNodeID] will be returned.
	Proposer() ids.NodeID
}

type statelessUnsignedBlock struct {
	ParentID     ids.ID `serialize:"true" json:"parentID"`
	Timestamp    int64  `serialize:"true" json:"timestamp"`
	PChainHeight uint64 `serialize:"true" json:"pChainHeight"`
	Certificate  []byte `serialize:"true" json:"certificate"`
	Block        []byte `serialize:"true" json:"block"`
}

type statelessUnsignedGraniteBlock struct {
	PChainEpoch  PChainEpoch `serialize:"true" json:"pChainEpoch"`
	ParentID     ids.ID      `serialize:"true" json:"parentID"`
	Timestamp    int64       `serialize:"true" json:"timestamp"`
	PChainHeight uint64      `serialize:"true" json:"pChainHeight"`
	Certificate  []byte      `serialize:"true" json:"certificate"`
	Block        []byte      `serialize:"true" json:"block"`
}

type PChainEpoch struct {
	Height    uint64
	Number    uint64
	StartTime time.Time
}

type statelessBlock struct {
	StatelessBlock statelessUnsignedBlock `serialize:"true" json:"statelessBlock"`
	Signature      []byte                 `serialize:"true" json:"signature"`

	id        ids.ID
	timestamp time.Time
	cert      *staking.Certificate
	proposer  ids.NodeID
	bytes     []byte
}

type statelessGraniteBlock struct {
	StatelessBlock statelessUnsignedGraniteBlock `serialize:"true" json:"statelessBlock"`
	Signature      []byte                        `serialize:"true" json:"signature"`

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
	return initializeBlock(b, bytes)
}

func (b *statelessBlock) verify(chainID ids.ID) error {
	return verifyBlock(b, chainID)
}

func (b *statelessBlock) PChainHeight() uint64 {
	return b.StatelessBlock.PChainHeight
}

func (*statelessBlock) PChainEpoch() PChainEpoch {
	return PChainEpoch{}
}

func (b *statelessBlock) Timestamp() time.Time {
	return b.timestamp
}

func (b *statelessBlock) Proposer() ids.NodeID {
	return b.proposer
}

func (b *statelessGraniteBlock) ID() ids.ID {
	return b.id
}

func (b *statelessGraniteBlock) ParentID() ids.ID {
	return b.StatelessBlock.ParentID
}

func (b *statelessGraniteBlock) Block() []byte {
	return b.StatelessBlock.Block
}

func (b *statelessGraniteBlock) PChainHeight() uint64 {
	return b.StatelessBlock.PChainHeight
}

func (b *statelessGraniteBlock) PChainEpoch() PChainEpoch {
	return b.StatelessBlock.PChainEpoch
}

func (b *statelessGraniteBlock) Bytes() []byte {
	return b.bytes
}

func (b *statelessGraniteBlock) Proposer() ids.NodeID {
	return b.proposer
}

func (b *statelessGraniteBlock) Timestamp() time.Time {
	return b.timestamp
}

func (b *statelessGraniteBlock) initialize(bytes []byte) error {
	return initializeBlock(b, bytes)
}

func (b *statelessGraniteBlock) verify(chainID ids.ID) error {
	return verifyBlock(b, chainID)
}

// initializeBlock handles the common initialization logic for both block types
func initializeBlock(block SignedBlock, bytes []byte) error {
	switch b := block.(type) {
	case *statelessBlock:
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

	case *statelessGraniteBlock:
		b.bytes = bytes

		// The serialized form of the block is the unsignedBytes followed by the
		// signature, which is prefixed by a uint32. So, we need to strip off the
		// signature as well as it's length prefix to get the unsigned bytes.
		lenUnsignedBytes := len(bytes) - wrappers.IntLen - len(b.Signature)
		unsignedBytes := bytes[:lenUnsignedBytes]
		b.id = hashing.ComputeHash256Array(unsignedBytes)

		b.timestamp = time.UnixMilli(b.StatelessBlock.Timestamp)
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

	default:
		return fmt.Errorf("unsupported block type: %T", block)
	}
}

// verifyBlock handles the common verification logic for both block types
func verifyBlock(block SignedBlock, chainID ids.ID) error {
	switch b := block.(type) {
	case *statelessBlock:
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

	case *statelessGraniteBlock:
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

	default:
		return fmt.Errorf("unsupported block type: %T", block)
	}
}
