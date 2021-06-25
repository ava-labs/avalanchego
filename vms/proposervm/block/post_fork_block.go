package block

import (
	"crypto/x509"
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

type statelessUnsignedPostForkBlock struct {
	ParentID     ids.ID `serialize:"true"`
	Timestamp    int64  `serialize:"true"`
	PChainHeight uint64 `serialize:"true"`
	Certificate  []byte `serialize:"true"`
	Block        []byte `serialize:"true"`
}

type StatelessPostForkBlock struct {
	StatelessBlock statelessUnsignedPostForkBlock `serialize:"true"`
	Signature      []byte                         `serialize:"true"`

	id        ids.ID
	timestamp time.Time
	forkTime  time.Time
	cert      *x509.Certificate
	proposer  ids.ShortID
	bytes     []byte
}

func (b *StatelessPostForkBlock) ID() ids.ID            { return b.id }
func (b *StatelessPostForkBlock) ParentID() ids.ID      { return b.StatelessBlock.ParentID }
func (b *StatelessPostForkBlock) PChainHeight() uint64  { return b.StatelessBlock.PChainHeight }
func (b *StatelessPostForkBlock) Timestamp() time.Time  { return b.timestamp }
func (b *StatelessPostForkBlock) Block() []byte         { return b.StatelessBlock.Block }
func (b *StatelessPostForkBlock) Proposer() ids.ShortID { return b.proposer }
func (b *StatelessPostForkBlock) Bytes() []byte         { return b.bytes }

func (b *StatelessPostForkBlock) Verify() error {
	unsignedBytes, err := c.Marshal(version, &b.StatelessBlock)
	if err != nil {
		return err
	}
	return b.cert.CheckSignature(b.cert.SignatureAlgorithm, unsignedBytes, b.Signature)
}
