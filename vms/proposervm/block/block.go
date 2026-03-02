// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
	errZeroEpoch           = errors.New("epoch must be provided after granite")
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
	PChainEpoch() Epoch
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
	StatelessBlock statelessUnsignedBlock `serialize:"true" json:"statelessBlock"`
	Epoch          Epoch                  `serialize:"true" json:"epoch"`
}

type Epoch struct {
	PChainHeight uint64 `serialize:"true" json:"pChainHeight"`
	Number       uint64 `serialize:"true" json:"number"`
	StartTime    int64  `serialize:"true" json:"startTime"`
}

type statelessBlockMetadata struct {
	id        ids.ID
	timestamp time.Time
	cert      *staking.Certificate
	proposer  ids.NodeID
	bytes     []byte
}

func (m *statelessBlockMetadata) initialize(
	b *statelessUnsignedBlock,
	sig []byte,
	bytes []byte,
) error {
	m.bytes = bytes

	// The serialized form of the block is the unsignedBytes followed by the
	// signature, which is prefixed by a uint32. So, we need to strip off the
	// signature as well as it's length prefix to get the unsigned bytes.
	lenUnsignedBytes := len(bytes) - wrappers.IntLen - len(sig)
	unsignedBytes := bytes[:lenUnsignedBytes]
	m.id = hashing.ComputeHash256Array(unsignedBytes)

	m.timestamp = time.Unix(b.Timestamp, 0)
	if len(b.Certificate) == 0 {
		return nil
	}

	var err error
	m.cert, err = staking.ParseCertificate(b.Certificate)
	if err != nil {
		return fmt.Errorf("%w: %w", errInvalidCertificate, err)
	}

	m.proposer = ids.NodeIDFromCert(m.cert)
	return nil
}

func (m *statelessBlockMetadata) verify(
	b *statelessUnsignedBlock,
	sig []byte,
	chainID ids.ID,
) error {
	if len(b.Certificate) == 0 {
		if len(sig) > 0 {
			return errUnexpectedSignature
		}
		return nil
	}

	header, err := BuildHeader(chainID, b.ParentID, m.id)
	if err != nil {
		return err
	}

	headerBytes := header.Bytes()
	return staking.CheckSignature(
		m.cert,
		headerBytes,
		sig,
	)
}

func (m *statelessBlockMetadata) ID() ids.ID {
	return m.id
}

func (m *statelessBlockMetadata) Timestamp() time.Time {
	return m.timestamp
}

func (m *statelessBlockMetadata) Proposer() ids.NodeID {
	return m.proposer
}

func (m *statelessBlockMetadata) Bytes() []byte {
	return m.bytes
}

type statelessBlock struct {
	statelessBlockMetadata

	StatelessBlock statelessUnsignedBlock `serialize:"true" json:"statelessBlock"`
	Signature      []byte                 `serialize:"true" json:"signature"`
}

type statelessGraniteBlock struct {
	statelessBlockMetadata

	StatelessGraniteBlock statelessUnsignedGraniteBlock `serialize:"true" json:"statelessGraniteBlock"`
	Signature             []byte                        `serialize:"true" json:"signature"`
}

func (b *statelessBlock) ParentID() ids.ID {
	return b.StatelessBlock.ParentID
}

func (b *statelessBlock) Block() []byte {
	return b.StatelessBlock.Block
}

func (b *statelessBlock) initialize(bytes []byte) error {
	return b.statelessBlockMetadata.initialize(&b.StatelessBlock, b.Signature, bytes)
}

func (b *statelessBlock) verify(chainID ids.ID) error {
	return b.statelessBlockMetadata.verify(&b.StatelessBlock, b.Signature, chainID)
}

func (b *statelessBlock) PChainHeight() uint64 {
	return b.StatelessBlock.PChainHeight
}

func (*statelessBlock) PChainEpoch() Epoch {
	return Epoch{}
}

func (b *statelessGraniteBlock) ParentID() ids.ID {
	return b.StatelessGraniteBlock.StatelessBlock.ParentID
}

func (b *statelessGraniteBlock) Block() []byte {
	return b.StatelessGraniteBlock.StatelessBlock.Block
}

func (b *statelessGraniteBlock) PChainHeight() uint64 {
	return b.StatelessGraniteBlock.StatelessBlock.PChainHeight
}

func (b *statelessGraniteBlock) PChainEpoch() Epoch {
	return b.StatelessGraniteBlock.Epoch
}

func (b *statelessGraniteBlock) initialize(bytes []byte) error {
	return b.statelessBlockMetadata.initialize(&b.StatelessGraniteBlock.StatelessBlock, b.Signature, bytes)
}

func (b *statelessGraniteBlock) verify(chainID ids.ID) error {
	if b.StatelessGraniteBlock.Epoch == (Epoch{}) {
		return errZeroEpoch
	}
	return b.statelessBlockMetadata.verify(&b.StatelessGraniteBlock.StatelessBlock, b.Signature, chainID)
}
