// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"crypto/x509"
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	_ SignedBlock = (*statelessCertSignedBlock)(nil)
	_ SignedBlock = (*statelessBlsSignedBlock)(nil)

	errUnexpectedProposer           = errors.New("expected no proposer but one was provided")
	errMissingProposer              = errors.New("expected proposer but none was provided")
	errBlsBlocksNotFullyImplemented = errors.New("NOT YET IMPLEMENTED")
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

// NOTE ABENEGIA: statelessUnsignedBlock not reused for bls signed blocks
// since certificate is not needed
type statelessUnsignedBlock struct {
	ParentID     ids.ID `serialize:"true"`
	Timestamp    int64  `serialize:"true"`
	PChainHeight uint64 `serialize:"true"`
	Certificate  []byte `serialize:"true"`
	Block        []byte `serialize:"true"`
}

type statelessCertSignedBlock struct {
	StatelessBlock statelessUnsignedBlock `serialize:"true"`
	Signature      []byte                 `serialize:"true"`

	id        ids.ID
	timestamp time.Time
	cert      *x509.Certificate
	proposer  ids.NodeID
	bytes     []byte
}

func (b *statelessCertSignedBlock) ID() ids.ID {
	return b.id
}

func (b *statelessCertSignedBlock) ParentID() ids.ID {
	return b.StatelessBlock.ParentID
}

func (b *statelessCertSignedBlock) Block() []byte {
	return b.StatelessBlock.Block
}

func (b *statelessCertSignedBlock) Bytes() []byte {
	return b.bytes
}

func (b *statelessCertSignedBlock) initialize(bytes []byte) error {
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
	b.proposer = ids.NodeIDFromCert(cert)
	return nil
}

func (b *statelessCertSignedBlock) PChainHeight() uint64 {
	return b.StatelessBlock.PChainHeight
}

func (b *statelessCertSignedBlock) Timestamp() time.Time {
	return b.timestamp
}

func (b *statelessCertSignedBlock) Proposer() ids.NodeID {
	return b.proposer
}

func (b *statelessCertSignedBlock) Verify(shouldHaveProposer bool, chainID ids.ID) error {
	if !shouldHaveProposer {
		if len(b.Signature) > 0 || len(b.StatelessBlock.Certificate) > 0 {
			return errUnexpectedProposer
		}
		return nil
	} else if b.cert == nil {
		return errMissingProposer
	}

	header, err := buildHeader(chainID, b.StatelessBlock.ParentID, b.id)
	if err != nil {
		return err
	}

	headerBytes := header.Bytes()
	return b.cert.CheckSignature(b.cert.SignatureAlgorithm, headerBytes, b.Signature)
}

type statelessBlsSignedBlock struct {
	BlockParentID     ids.ID     `serialize:"true"`
	BlockTimestamp    int64      `serialize:"true"`
	BlockPChainHeight uint64     `serialize:"true"`
	BlockProposer     ids.NodeID `serialize:"true"`
	InnerBlockBytes   []byte     `serialize:"true"`
	Signature         []byte     `serialize:"true"`

	id        ids.ID
	timestamp time.Time
	// cert      *x509.Certificate // TODO ABENEGIA: maybe BLS key?
	bytes []byte
}

func (b *statelessBlsSignedBlock) ID() ids.ID {
	return b.id
}

func (b *statelessBlsSignedBlock) ParentID() ids.ID {
	return b.BlockParentID
}

func (b *statelessBlsSignedBlock) Block() []byte {
	return b.InnerBlockBytes
}

func (b *statelessBlsSignedBlock) Bytes() []byte {
	return b.bytes
}

func (b *statelessBlsSignedBlock) initialize(_ []byte) error {
	b.timestamp = time.Unix(b.BlockTimestamp, 0)
	return errBlsBlocksNotFullyImplemented
}

func (b *statelessBlsSignedBlock) PChainHeight() uint64 {
	return b.BlockPChainHeight
}

func (b *statelessBlsSignedBlock) Timestamp() time.Time {
	return b.timestamp
}

func (b *statelessBlsSignedBlock) Proposer() ids.NodeID {
	return b.BlockProposer
}

func (*statelessBlsSignedBlock) Verify(_ bool, _ ids.ID) error {
	return errBlsBlocksNotFullyImplemented
}
