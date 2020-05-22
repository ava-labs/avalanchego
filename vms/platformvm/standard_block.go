// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/vms/components/core"
)

// DecisionTx is an operation that can be decided without being proposed
type DecisionTx interface {
	initialize(vm *VM) error

	ID() ids.ID

	Bytes() []byte
	// Attempt to verify this transaction with the provided state. The provided
	// database can be modified arbitrarily. If a nil error is returned, it is
	// assumed onAccept is non-nil.
	SemanticVerify(database.Database) (onAccept func(), err error)

	SyntacticVerify() error

	Accept() error

	Reject() error
}

// StandardBlock being accepted results in the transactions contained in the
// block to be accepted and committed to the chain.
type StandardBlock struct {
	SingleDecisionBlock `serialize:"true"`

	Txs []DecisionTx `serialize:"true"`
}

// initialize this block
func (sb *StandardBlock) initialize(vm *VM, bytes []byte) error {
	if err := sb.SingleDecisionBlock.initialize(vm, bytes); err != nil {
		return err
	}
	for _, tx := range sb.Txs {
		if err := tx.initialize(vm); err != nil {
			return err
		}

		if err := tx.SyntacticVerify(); err != nil {
			return err
		}

		status, err := vm.getTxStatus(vm.DB, tx.ID())
		if err != nil {
			return err
		}
		if status == choices.Unknown {
			genTx := &GenericTx{
				Tx: &tx,
			}
			if err := vm.putTx(vm.DB, tx.ID(), genTx); err != nil {
				return nil, err
			}
			if err := vm.putTxStatus(vm.DB, tx.ID(), choices.Processing); err != nil {
				return err
			}
			if err := vm.DB.Commit(); err != nil {
				return err
			}
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
	parentBlock := sb.parentBlock()
	// StandardBlock is not a modifier on a proposal block, so its parent must
	// be a decision.
	parent, ok := parentBlock.(decision)
	if !ok {
		return errInvalidBlockType
	}

	pdb := parent.onAccept()

	sb.onAcceptDB = versiondb.New(pdb)
	funcs := []func(){}
	for _, tx := range sb.Txs {
		onAccept, err := tx.SemanticVerify(sb.onAcceptDB)
		if err != nil {
			return err
		}
		if onAccept != nil {
			funcs = append(funcs, onAccept)
		}
	}

	if numFuncs := len(funcs); numFuncs == 1 {
		sb.onAcceptFunc = funcs[0]
	} else if numFuncs > 1 {
		sb.onAcceptFunc = func() {
			for _, f := range funcs {
				f()
			}
		}
	}

	sb.vm.currentBlocks[sb.ID().Key()] = sb
	sb.parentBlock().addChild(sb)
	return nil
}

func (sb *StandardBlock) Accept() {
	sb.vm.Ctx.Log.Verbo("Accepting block with ID %s", sb.ID())

	sb.CommonBlock.Accept()

	for _, tx := range sb.Txs {
		if err := tx.Accept(); err != nil {
			sb.vm.Ctx.Log.Error("unable to accept tx")
		}
	}
}

func (sb *StandardBlock) Reject() {
	sb.vm.Ctx.Log.Verbo("Rejecting block with ID %s", sb.ID())

	sb.CommonBlock.Reject()

	for _, tx := range sb.Txs {
		if err := tx.Reject(); err != nil {
			sb.vm.Ctx.Log.Error("unable to reject tx")
		}
	}
}

// newStandardBlock returns a new *StandardBlock where the block's parent, a
// decision block, has ID [parentID].
func (vm *VM) newStandardBlock(parentID ids.ID, txs []DecisionTx) (*StandardBlock, error) {
	sb := &StandardBlock{
		SingleDecisionBlock: SingleDecisionBlock{CommonDecisionBlock: CommonDecisionBlock{CommonBlock: CommonBlock{
			Block: core.NewBlock(parentID),
			vm:    vm,
		}}},
		Txs: txs,
	}

	// We serialize this block as a Block so that it can be deserialized into a
	// Block
	blk := Block(sb)
	bytes, err := Codec.Marshal(&blk)
	if err != nil {
		return nil, err
	}
	sb.Block.Initialize(bytes, vm.SnowmanVM)
	return sb, nil
}
