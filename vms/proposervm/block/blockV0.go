// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// statelessUnsignedBlockV0 is the "old" version of statelessUnsignedBlock, which doesn't contain the VRF signature.
type statelessUnsignedBlockV0 struct {
	ParentID     ids.ID `serialize:"true"`
	Timestamp    int64  `serialize:"true"`
	PChainHeight uint64 `serialize:"true"`
	Certificate  []byte `serialize:"true"`
	Block        []byte `serialize:"true"`
}

// statelessBlockV0 is the "old" version of statelessBlock, which doesn't contain the VRF signature.
type statelessBlockV0 struct {
	StatelessBlock statelessUnsignedBlockV0 `serialize:"true"`
	Signature      []byte                   `serialize:"true"`

	id        ids.ID
	timestamp time.Time
	cert      *staking.Certificate
	proposer  ids.NodeID
	bytes     []byte
}

func (b *statelessBlockV0) ID() ids.ID {
	return b.id
}

func (b *statelessBlockV0) ParentID() ids.ID {
	return b.StatelessBlock.ParentID
}

func (b *statelessBlockV0) Block() []byte {
	return b.StatelessBlock.Block
}

func (b *statelessBlockV0) Bytes() []byte {
	return b.bytes
}

func (*statelessBlockV0) VRFSig() []byte {
	return nil
}

func (*statelessBlockV0) VerifySignature(pk *bls.PublicKey, parentVRFSig []byte, _ ids.ID, _ uint32) bool {
	return pk == nil && len(parentVRFSig) == 0
}

func (b *statelessBlockV0) initializeID() error {
	var unsignedBytes []byte
	// The serialized form of the block is the unsignedBytes followed by the
	// signature, which is prefixed by a uint32. So, we need to strip off the
	// signature as well as it's length prefix to get the unsigned bytes.
	lenUnsignedBytes := len(b.bytes) - wrappers.IntLen - len(b.Signature)
	if lenUnsignedBytes <= 0 {
		return errInvalidBlockEncodingLength
	}

	unsignedBytes = b.bytes[:lenUnsignedBytes]
	b.id = hashing.ComputeHash256Array(unsignedBytes)
	return nil
}

func (b *statelessBlockV0) initialize(bytes []byte) error {
	b.bytes = bytes

	if err := b.initializeID(); err != nil {
		return err
	}

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

func (b *statelessBlockV0) verify(chainID ids.ID) error {
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

func (b *statelessBlockV0) PChainHeight() uint64 {
	return b.StatelessBlock.PChainHeight
}

func (b *statelessBlockV0) Timestamp() time.Time {
	return b.timestamp
}

func (b *statelessBlockV0) Proposer() ids.NodeID {
	return b.proposer
}
