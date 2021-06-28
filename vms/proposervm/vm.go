// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

// VM is a decorator for a snowman.ChainVM struct, created to handle block headers introduced with snowman++

// Contract
// * CoreVM MUST build blocks on top of currently preferred block, otherwise Verify() will fail
// * After initialization. full ProposerBlocks (proHeader + core block) are stored in proposervm.VM's db
// on Build/ParseBlock calls, AFTER calls to core vm's Build/ParseBlock, which we ASSUME
//  would store core block on core VM's db.
// * ProposerVM do not track ProposerBlock state; instead state related calls (Accept/Reject/Status) are
// forwarded to the core VM. Since block registration HAPPENS BEFORE block status settings,
// proposerVM is guaranteed not to lose the last accepted block
// * ProposerVM can handle both ProposerVM blocks AND generic snowman.Block not wrapped with a ProposerBlocHeader
// This allows all snowman-like VM freedom to select a time after which introduce the congestion control mechanism
// implemented via the proposer block header

import (
	"crypto"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
	"github.com/ava-labs/avalanchego/vms/proposervm/scheduler"
	"github.com/ava-labs/avalanchego/vms/proposervm/state"
	"github.com/ava-labs/avalanchego/vms/proposervm/tree"

	statelessblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
)

var (
	dbPrefix = []byte("proposervm")

	_ block.ChainVM = &VM{}
)

type VM struct {
	block.ChainVM
	activationTime time.Time

	state.State
	proposer.Windower
	tree.Tree
	scheduler.Scheduler
	timer.Clock

	ctx            *snow.Context
	db             *versiondb.Database
	verifiedBlocks map[ids.ID]*ProposerBlock
	preferred      ids.ID
}

func New(vm block.ChainVM, activationTime time.Time) *VM {
	return &VM{
		ChainVM:        vm,
		activationTime: activationTime,
	}
}

// common.VM interface implementation
func (vm *VM) Initialize(
	ctx *snow.Context,
	dbManager manager.Manager,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	toEngine chan<- common.Message,
	fxs []*common.Fx,
) error {
	rawDB := dbManager.Current().Database
	prefixDB := prefixdb.New(dbPrefix, rawDB)
	vm.db = versiondb.New(prefixDB)
	vm.State = state.New(vm.db)
	vm.Windower = proposer.New(ctx.ValidatorVM, ctx.SubnetID, ctx.ChainID)
	vm.Tree = tree.New()

	scheduler, vmToEngine := scheduler.New(toEngine)
	vm.Scheduler = scheduler

	go ctx.Log.RecoverAndPanic(func() {
		scheduler.Dispatch(timer.MaxTime)
	})

	vm.ctx = ctx
	vm.verifiedBlocks = make(map[ids.ID]*ProposerBlock)

	err := vm.ChainVM.Initialize(
		ctx,
		dbManager,
		genesisBytes,
		upgradeBytes,
		configBytes,
		vmToEngine,
		fxs,
	)
	if err != nil {
		return err
	}

	// TODO: repair the DB if the underlying VM's Accept calls weren't persisted

	prefID, err := vm.LastAccepted()
	switch err {
	case nil:
	case database.ErrNotFound:
		// store genesis
		corePrefID, err := vm.ChainVM.LastAccepted()
		if err != nil {
			return err
		}
		corePref, err := vm.ChainVM.GetBlock(corePrefID)
		if err != nil {
			return err
		}
		slb, err := statelessblock.BuildPreFork(
			vm.preferred,
			corePref.Timestamp(),
			vm.activationTime,
			corePref.Bytes(),
			corePref.ID())
		if err != nil {
			return err
		}

		proGen := &ProposerBlock{
			Block:   slb,
			vm:      vm,
			coreBlk: corePref,
			status:  choices.Processing,
		}
		if err := vm.storeProposerBlock(proGen); err != nil {
			return err
		}

		prefID = proGen.ID()
		if err := vm.SetLastAccepted(prefID); err != nil {
			return err
		}
	default:
		return err
	}

	return vm.SetPreference(prefID)
}

// block.ChainVM interface implementation
func (vm *VM) BuildBlock() (snowman.Block, error) {
	sb, err := vm.ChainVM.BuildBlock()
	if err != nil {
		return nil, err
	}

	var slb statelessblock.Block
	if sb.Parent().Timestamp().Before(vm.activationTime) {
		slb, err = statelessblock.BuildPreFork(
			vm.preferred,
			sb.Timestamp(), // assuming in-house built blocks are verified, hence safe to call Timestamp
			vm.activationTime,
			sb.Bytes(),
			sb.ID())
		if err != nil {
			return nil, err
		}
	} else {
		h, err := vm.ctx.ValidatorVM.GetCurrentHeight()
		if err != nil {
			return nil, err
		}

		slb, err = statelessblock.Build(
			vm.preferred,
			sb.Timestamp(),
			vm.activationTime,
			h,
			vm.ctx.StakingCert.Leaf,
			sb.Bytes(),
			vm.ctx.StakingCert.PrivateKey.(crypto.Signer),
		)
		if err != nil {
			return nil, err
		}
	}

	blk := &ProposerBlock{
		Block:   slb,
		vm:      vm,
		coreBlk: sb,
		status:  choices.Processing,
	}
	return blk, vm.storeProposerBlock(blk)
}

func (vm *VM) ParseBlock(b []byte) (snowman.Block, error) {
	// Invariant: always return a proposerBlk
	blk, err := vm.parseProposerBlock(b)
	if err == nil {
		return blk, nil
	}

	sb, err := vm.ChainVM.ParseBlock(b)
	if err != nil {
		return nil, err
	}

	slb, err := statelessblock.BuildPreFork(
		vm.preferred,
		sb.Timestamp(), // TODO: not safe calling Timestamp here. What to do? Update during verify? Use parent one?
		vm.activationTime,
		sb.Bytes(),
		sb.ID())
	if err != nil {
		return nil, err
	}

	blk = &ProposerBlock{
		Block:   slb,
		vm:      vm,
		coreBlk: sb,
		status:  choices.Processing,
	}
	return blk, nil
}

func (vm *VM) GetBlock(id ids.ID) (snowman.Block, error) {
	if blk, err := vm.getProposerBlock(id); err == nil {
		return blk, nil
	}

	sb, err := vm.ChainVM.GetBlock(id)
	if err != nil {
		return nil, err
	}
	slb, err := statelessblock.BuildPreFork(
		vm.preferred,
		sb.Timestamp(), // TODO: not necessarily safe calling Timestamp here. What to do? Update during verify? Use parent one?
		vm.activationTime,
		sb.Bytes(),
		sb.ID())
	if err != nil {
		return nil, err
	}

	blk := &ProposerBlock{
		Block:   slb,
		vm:      vm,
		coreBlk: sb,
		status:  choices.Processing,
	}
	if err := vm.storeProposerBlock(blk); err != nil {
		return nil, err
	}

	return blk, nil
}

func (vm *VM) SetPreference(preferred ids.ID) error {
	if vm.preferred == preferred {
		return nil
	}
	vm.preferred = preferred

	blk, err := vm.getProposerBlock(preferred)
	if err != nil {
		return err
	}

	if err := vm.ChainVM.SetPreference(blk.coreBlk.ID()); err != nil {
		return err
	}

	// TODO: reset the scheduler
	return nil
}

func (vm *VM) LastAccepted() (ids.ID, error) {
	return vm.State.GetLastAccepted()
}

func (vm *VM) getProposerBlock(blkID ids.ID) (*ProposerBlock, error) {
	blk, exists := vm.verifiedBlocks[blkID]
	if exists {
		return blk, nil
	}
	slb, status, err := vm.State.GetBlock(blkID)
	if err != nil {
		return nil, err
	}

	coreBlk, err := vm.ChainVM.ParseBlock(slb.Block())
	if err != nil {
		return nil, err
	}

	return &ProposerBlock{
		Block:   slb,
		vm:      vm,
		coreBlk: coreBlk,
		status:  status,
	}, nil
}

func (vm *VM) parseProposerBlock(b []byte) (*ProposerBlock, error) {
	slb, err := statelessblock.Parse(b)
	if err != nil {
		return nil, err
	}
	// if the block already exists, then make sure the status is set correctly
	blk, err := vm.getProposerBlock(slb.ID())
	if err == nil {
		return blk, nil
	}
	if err != database.ErrNotFound {
		return nil, err
	}

	coreBlk, err := vm.ChainVM.ParseBlock(slb.Block())
	if err != nil {
		return nil, err
	}

	blk = &ProposerBlock{
		Block:   slb,
		vm:      vm,
		coreBlk: coreBlk,
		status:  choices.Processing,
	}
	if err := vm.storeProposerBlock(blk); err != nil {
		return nil, err
	}
	return blk, nil
}

func (vm *VM) storeProposerBlock(blk *ProposerBlock) error {
	if err := vm.State.PutBlock(blk.Block, blk.status); err != nil {
		return err
	}
	return vm.db.Commit()
}
