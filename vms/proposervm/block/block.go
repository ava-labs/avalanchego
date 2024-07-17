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
)

var (
	_ SignedBlock = (*statelessBlock[statelessUnsignedBlock])(nil)
	_ SignedBlock = (*statelessBlock[statelessUnsignedBlockV0])(nil)

	errUnexpectedSignature              = errors.New("signature provided when none was expected")
	errInvalidCertificate               = errors.New("invalid certificate")
	errInvalidBlockEncodingLength       = errors.New("block encoding length must be greater than zero bytes long")
	errFailedToParseVRFSignature        = errors.New("failed to parse VRF signature")
	errUnexpectedStatelessUnsignedBlock = errors.New("unexpected statelessUnsignedBlock type")
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
	VerifySignature(*bls.PublicKey, []byte, ids.ID, uint32) bool
}

// statelessUnsignedBlockV0 is the "old" version of statelessUnsignedBlock, which doesn't contain the VRF signature.
type statelessUnsignedBlockV0 struct {
	ParentID     ids.ID `serialize:"true"`
	Timestamp    int64  `serialize:"true"`
	PChainHeight uint64 `serialize:"true"`
	Certificate  []byte `serialize:"true"`
	Block        []byte `serialize:"true"`
}

type statelessUnsignedBlock struct {
	ParentID     ids.ID `serialize:"true"`
	Timestamp    int64  `serialize:"true"`
	PChainHeight uint64 `serialize:"true"`
	Certificate  []byte `serialize:"true"`
	Block        []byte `serialize:"true"`
	VRFSig       []byte `serialize:"true"`
}

type unsignedBlockTypes interface {
	statelessUnsignedBlockV0 | statelessUnsignedBlock
}

type statelessBlock[T unsignedBlockTypes] struct {
	StatelessBlock T      `serialize:"true"`
	Signature      []byte `serialize:"true"`

	id        ids.ID
	timestamp time.Time
	cert      *staking.Certificate
	proposer  ids.NodeID
	bytes     []byte
	vrfSig    *bls.Signature
}

func (b *statelessBlock[T]) ParentID() ids.ID {
	switch sb := any(b.StatelessBlock).(type) {
	case statelessUnsignedBlock:
		return sb.ParentID
	case statelessUnsignedBlockV0:
		return sb.ParentID
	default:
		panic(errUnexpectedStatelessUnsignedBlock)
	}
}

func (b *statelessBlock[T]) Block() []byte {
	switch sb := any(b.StatelessBlock).(type) {
	case statelessUnsignedBlock:
		return sb.Block
	case statelessUnsignedBlockV0:
		return sb.Block
	default:
		panic(errUnexpectedStatelessUnsignedBlock)
	}
}

func (b *statelessBlock[T]) VRFSig() []byte {
	switch sb := any(b.StatelessBlock).(type) {
	case statelessUnsignedBlock:
		return sb.VRFSig
	case statelessUnsignedBlockV0:
		return nil
	default:
		panic(errUnexpectedStatelessUnsignedBlock)
	}
}

func (b *statelessBlock[T]) PChainHeight() uint64 {
	switch sb := any(b.StatelessBlock).(type) {
	case statelessUnsignedBlock:
		return sb.PChainHeight
	case statelessUnsignedBlockV0:
		return sb.PChainHeight
	default:
		panic(errUnexpectedStatelessUnsignedBlock)
	}
}

func (b *statelessBlock[T]) innerCertificate() []byte {
	switch sb := any(b.StatelessBlock).(type) {
	case statelessUnsignedBlock:
		return sb.Certificate
	case statelessUnsignedBlockV0:
		return sb.Certificate
	default:
		panic(errUnexpectedStatelessUnsignedBlock)
	}
}

func (b *statelessBlock[T]) innerTimestamp() int64 {
	switch sb := any(b.StatelessBlock).(type) {
	case statelessUnsignedBlock:
		return sb.Timestamp
	case statelessUnsignedBlockV0:
		return sb.Timestamp
	default:
		panic(errUnexpectedStatelessUnsignedBlock)
	}
}

func (b *statelessBlock[T]) Timestamp() time.Time {
	return b.timestamp
}

func (b *statelessBlock[T]) Proposer() ids.NodeID {
	return b.proposer
}

func (b *statelessBlock[T]) Bytes() []byte {
	return b.bytes
}

func (b *statelessBlock[T]) ID() ids.ID {
	return b.id
}

func (b *statelessBlock[T]) VerifySignature(pk *bls.PublicKey, parentVRFSig []byte, chainID ids.ID, networkID uint32) bool {
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

func (b *statelessBlock[T]) initialize(bytes []byte) error {
	var err error

	b.bytes = bytes
	if b.id, err = initializeID(bytes, b.Signature); err != nil {
		return err
	}

	b.timestamp = time.Unix(b.innerTimestamp(), 0)
	if len(b.innerCertificate()) == 0 {
		return nil
	}

	b.cert, err = staking.ParseCertificate(b.innerCertificate())
	if err != nil {
		return fmt.Errorf("%w: %w", errInvalidCertificate, err)
	}

	b.proposer = ids.NodeIDFromCert(b.cert)

	// The following ensures that we would initialize the vrfSig member only when
	// the provided signature is 96 bytes long. That supports both statelessUnsignedBlockV0 & statelessUnsignedBlock
	// variations, as well as optional 32-byte hashes stored in the VRFSig.
	if vrfBytes := b.VRFSig(); len(vrfBytes) == bls.SignatureLen {
		b.vrfSig, err = bls.SignatureFromBytes(vrfBytes)
		if err != nil {
			return fmt.Errorf("%w: %w", errFailedToParseVRFSignature, err)
		}
	}

	return nil
}

func (b *statelessBlock[T]) verify(chainID ids.ID) error {
	if len(b.innerCertificate()) == 0 {
		if len(b.Signature) > 0 {
			return errUnexpectedSignature
		}
		return nil
	}

	header, err := BuildHeader(chainID, b.ParentID(), b.id)
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
