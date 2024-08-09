// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

var (
	_ SignedBlock = (*statelessBlock)(nil)

	errUnexpectedSignature        = errors.New("signature provided when none was expected")
	errInvalidCertificate         = errors.New("invalid certificate")
	errInvalidBlockEncodingLength = errors.New("block encoding length must be greater than zero bytes long")
	errFailedToParseVRFSignature  = errors.New("failed to parse VRF signature")
)

type Block interface {
	ID() ids.ID
	ParentID() ids.ID
	Block() []byte
	Bytes() []byte

	// initialize & verify are used by the parser so that all the block types can be treated uniformly
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

	VRFSig() []byte

	// VerifySignature validates the correctness of the VRF signature.
	VerifySignature(pk *bls.PublicKey, parentVRFSig []byte, chainID ids.ID, networkID uint32) bool
}

type statelessUnsignedBlock struct {
	ParentID     ids.ID `v0:"true"`
	Timestamp    int64  `v0:"true"`
	PChainHeight uint64 `v0:"true"`
	Certificate  []byte `v0:"true"`
	Block        []byte `v0:"true"`

	VRFSig []byte `v1:"true"`
}

type statelessBlock struct {
	StatelessBlock statelessUnsignedBlock `v0:"true"`
	Signature      []byte                 `v0:"true"`

	id        ids.ID
	timestamp time.Time
	cert      *staking.Certificate
	proposer  ids.NodeID
	bytes     []byte
	vrfSig    *bls.Signature
}

func (b *statelessBlock) ParentID() ids.ID {
	return b.StatelessBlock.ParentID
}

func (b *statelessBlock) Block() []byte {
	return b.StatelessBlock.Block
}

func (b *statelessBlock) VRFSig() []byte {
	return b.StatelessBlock.VRFSig
}

func (b *statelessBlock) Bytes() []byte {
	return b.bytes
}

func (b *statelessBlock) ID() ids.ID {
	return b.id
}

func (b *statelessBlock) VerifySignature(pk *bls.PublicKey, parentVRFSig []byte, chainID ids.ID, networkID uint32) bool {
	if pk == nil {
		// proposer doesn't have a BLS key.
		if len(parentVRFSig) == 0 {
			// parent block had no VRF Signature.
			// in this case, we verify that the current signature is empty.
			return len(b.VRFSig()) == 0
		}
		// parent block had VRF Signature.
		expectedSignature := NextHashBlockSignature(parentVRFSig)
		return bytes.Equal(expectedSignature, b.VRFSig())
	}

	// proposer does have a BLS key.
	if len(parentVRFSig) == 0 {
		// parent block had no VRF Signature.
		msgHash := calculateBootstrappingBlockSig(chainID, networkID)
		return bls.Verify(pk, b.vrfSig, msgHash[:])
	}
	return bls.Verify(pk, b.vrfSig, parentVRFSig)
}

func (b *statelessBlock) initialize(bytes []byte) error {
	var err error

	b.bytes = bytes
	if b.id, err = initializeID(bytes, b.Signature); err != nil {
		return err
	}

	b.timestamp = time.Unix(b.StatelessBlock.Timestamp, 0)
	if len(b.StatelessBlock.Certificate) == 0 {
		return nil
	}

	b.cert, err = staking.ParseCertificate(b.StatelessBlock.Certificate)
	if err != nil {
		return fmt.Errorf("%w: %w", errInvalidCertificate, err)
	}

	b.proposer = ids.NodeIDFromCert(b.cert)

	// The following ensures that we would initialize the vrfSig member only when
	// the provided signature is 96 bytes long. That supports both v0 & v1
	// variations, as well as optional 32-byte hashes stored in the VRFSig.
	switch len(b.StatelessBlock.VRFSig) {
	case 0:
		// no VRFSig
	case hashing.HashLen:
		// it's a hash
	case bls.SignatureLen:
		b.vrfSig, err = bls.SignatureFromBytes(b.StatelessBlock.VRFSig)
		if err != nil {
			return fmt.Errorf("%w: %w", errFailedToParseVRFSignature, err)
		}
	default:
		// unexpected cases.
		return fmt.Errorf("%w: VRF Signature length is %d", errFailedToParseVRFSignature, len(b.StatelessBlock.VRFSig))
	}

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
