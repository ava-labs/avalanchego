package builder

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateful"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ buildingStrategy = &apricotStrategy{}

type apricotStrategy struct {
	b           *blockBuilder
	parentBlkID ids.ID
	height      uint64
	txes        []*txs.Tx
}

func (a *apricotStrategy) build() (snowman.Block, error) {
	blkVersion := uint16(stateless.ApricotVersion)
	switch a.txes[0].Unsigned.(type) {
	case txs.StakerTx,
		*txs.RewardValidatorTx,
		*txs.AdvanceTimeTx:
		return stateful.NewProposalBlock(
			blkVersion,
			uint64(0),
			a.b.blkVerifier,
			a.b.txExecutorBackend,
			a.parentBlkID,
			a.height,
			a.txes[0],
		)
	case *txs.CreateChainTx,
		*txs.CreateSubnetTx,
		*txs.ImportTx,
		*txs.ExportTx:
		return stateful.NewStandardBlock(
			blkVersion,
			uint64(0),
			a.b.blkVerifier,
			a.b.txExecutorBackend,
			a.parentBlkID,
			a.height,
			a.txes,
		)
	default:
		return nil, fmt.Errorf("unhandled tx type, could not include into a block")
	}
}
