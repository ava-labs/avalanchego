// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"crypto/x509"
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

type Block interface {
	ID() ids.ID

	ParentID() ids.ID
	PChainHeight() uint64
	Timestamp() time.Time
	Block() []byte
	Proposer() ids.ShortID

	Bytes() []byte

	Verify() error
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
	proposer  ids.ShortID
	bytes     []byte
}

func (b *statelessBlock) ID() ids.ID            { return b.id }
func (b *statelessBlock) ParentID() ids.ID      { return b.StatelessBlock.ParentID }
func (b *statelessBlock) PChainHeight() uint64  { return b.StatelessBlock.PChainHeight }
func (b *statelessBlock) Timestamp() time.Time  { return b.timestamp }
func (b *statelessBlock) Block() []byte         { return b.StatelessBlock.Block }
func (b *statelessBlock) Proposer() ids.ShortID { return b.proposer }
func (b *statelessBlock) Bytes() []byte         { return b.bytes }

func (b *statelessBlock) Verify() error {
	unsignedBytes, err := c.Marshal(version, &b.StatelessBlock)
	if err != nil {
		return err
	}
	return b.cert.CheckSignature(b.cert.SignatureAlgorithm, unsignedBytes, b.Signature)
}
