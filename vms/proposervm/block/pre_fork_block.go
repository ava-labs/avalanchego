package block

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
)

var zeroPChainHeight uint64 = 0

type statelessUnsignedPreForkBlock struct {
	Block []byte `serialize:"true"`
}

type StatelessPreForkBlock struct {
	StatelessBlock statelessUnsignedPreForkBlock `serialize:"true"`

	id        ids.ID
	parentID  ids.ID
	timestamp time.Time
	forkTime  time.Time
	bytes     []byte
}

func (b *StatelessPreForkBlock) ID() ids.ID            { return b.id }
func (b *StatelessPreForkBlock) ParentID() ids.ID      { return b.parentID }
func (b *StatelessPreForkBlock) PChainHeight() uint64  { return zeroPChainHeight }
func (b *StatelessPreForkBlock) Timestamp() time.Time  { return b.timestamp }
func (b *StatelessPreForkBlock) Block() []byte         { return b.StatelessBlock.Block }
func (b *StatelessPreForkBlock) Proposer() ids.ShortID { return proposer.None }
func (b *StatelessPreForkBlock) Bytes() []byte         { return b.bytes }

func (b *StatelessPreForkBlock) Verify() error {
	return nil // unmarshalling of coreBlock bytes is performed on Parse/Build. No need to repeat here
}
