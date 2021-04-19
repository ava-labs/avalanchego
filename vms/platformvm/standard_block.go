// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
)

var (
	_ Block    = &StandardBlock{}
	_ decision = &StandardBlock{}
)

// StandardBlock being accepted results in the transactions contained in the
// block to be accepted and committed to the chain.
type StandardBlock struct {
	SingleDecisionBlock `serialize:"true"`

	Txs []*Tx `serialize:"true" json:"txs"`
}

func (sb *StandardBlock) initialize(vm *VM, bytes []byte, status choices.Status, blk Block) error {
	if err := sb.SingleDecisionBlock.initialize(vm, bytes, status, blk); err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}
	for _, tx := range sb.Txs {
		if err := tx.Sign(vm.codec, nil); err != nil {
			return fmt.Errorf("failed to sign block: %w", err)
		}
	}
	return nil
}

// Verify this block performs a valid state transition.
//
// The parent block must be a proposal
//
// This function also sets onAcceptDB database if the verification passes.
func (sb *StandardBlock) Verify() error {
	blkID := sb.ID()

	if err := sb.SingleDecisionBlock.Verify(); err != nil {
		if err := sb.Reject(); err != nil {
			sb.vm.ctx.Log.Error(
				"failed to reject standard block %s due to %s",
				blkID,
				err,
			)
		}
		return err
	}

	parentIntf, err := sb.parent()
	if err != nil {
		return err
	}

	// StandardBlock is not a modifier on a proposal block, so its parent must
	// be a decision.
	parent, ok := parentIntf.(decision)
	if !ok {
		if err := sb.Reject(); err != nil {
			sb.vm.ctx.Log.Error(
				"failed to reject standard block %s due to %s",
				blkID,
				err,
			)
		}
		return errInvalidBlockType
	}

	parentState := parent.onAccept()
	sb.onAcceptState = NewVersionedState(
		parentState,
		parentState.CurrentStakerChainState(),
		parentState.PendingStakerChainState(),
	)

	funcs := make([]func() error, 0, len(sb.Txs))
	for _, tx := range sb.Txs {
		utx, ok := tx.UnsignedTx.(UnsignedDecisionTx)
		if !ok {
			return errWrongTxType
		}
		txID := tx.ID()
		onAccept, err := utx.SemanticVerify(sb.vm, sb.onAcceptState, tx)
		if err != nil {
			sb.vm.droppedTxCache.Put(txID, err.Error()) // cache tx as dropped
			if err := sb.Reject(); err != nil {
				sb.vm.ctx.Log.Error(
					"failed to reject standard block %s due to %s",
					blkID,
					err,
				)
			}
			return err
		}
		sb.onAcceptState.AddTx(tx, Committed)
		if onAccept != nil {
			funcs = append(funcs, onAccept)
		}
	}

	if numFuncs := len(funcs); numFuncs == 1 {
		sb.onAcceptFunc = funcs[0]
	} else if numFuncs > 1 {
		sb.onAcceptFunc = func() error {
			for _, f := range funcs {
				if err := f(); err != nil {
					return fmt.Errorf("failed to execute onAcceptFunc: %w", err)
				}
			}
			return nil
		}
	}

	sb.vm.currentBlocks[blkID] = sb
	parentIntf.addChild(sb)
	return nil
}

func (sb *StandardBlock) Reject() error {
	sb.vm.ctx.Log.Verbo(
		"Rejecting Standard Block %s at height %d with parent %s",
		sb.ID(),
		sb.Height(),
		sb.ParentID(),
	)

	for _, tx := range sb.Txs {
		if err := sb.vm.mempool.IssueTx(tx); err != nil {
			sb.vm.ctx.Log.Debug(
				"failed to reissue tx %q due to: %s",
				tx.ID(),
				err,
			)
		}
	}
	return sb.SingleDecisionBlock.Reject()
}

// newStandardBlock returns a new *StandardBlock where the block's parent, a
// decision block, has ID [parentID].
func (vm *VM) newStandardBlock(parentID ids.ID, height uint64, txs []*Tx) (*StandardBlock, error) {
	sb := &StandardBlock{
		SingleDecisionBlock: SingleDecisionBlock{
			CommonDecisionBlock: CommonDecisionBlock{
				CommonBlock: CommonBlock{
					PrntID: parentID,
					Hght:   height,
				},
			},
		},
		Txs: txs,
	}

	// We serialize this block as a Block so that it can be deserialized into a
	// Block
	blk := Block(sb)
	bytes, err := vm.codec.Marshal(codecVersion, &blk)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal block: %w", err)
	}
	return sb, sb.SingleDecisionBlock.initialize(vm, bytes, choices.Processing, sb)
}
