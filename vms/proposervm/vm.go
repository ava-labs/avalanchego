// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"errors"
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
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/vms/proposervm/option"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
	"github.com/ava-labs/avalanchego/vms/proposervm/scheduler"
	"github.com/ava-labs/avalanchego/vms/proposervm/state"
	"github.com/ava-labs/avalanchego/vms/proposervm/tree"

	statelessblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
)

var (
	dbPrefix           = []byte("proposervm")
	ErrNotValidatorsVM = errors.New("VM could not access validators.VM interface")

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
	verifiedBlocks map[ids.ID]Block
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
	vm.ctx = ctx
	if vm.ctx.ValidatorVM == nil { // happens creating P-chain vm
		valVM, ok := vm.ChainVM.(validators.VM)
		if !ok {
			vm.ctx.Log.Error("could not access validators.VM interface")
			return ErrNotValidatorsVM
		}
		vm.ctx.ValidatorVM = valVM
	}

	rawDB := dbManager.Current().Database
	prefixDB := prefixdb.New(dbPrefix, rawDB)
	vm.db = versiondb.New(prefixDB)
	vm.State = state.New(vm.db)
	vm.Windower = proposer.New(ctx.ValidatorVM, ctx.SubnetID, ctx.ChainID)
	vm.Tree = tree.New()

	scheduler, vmToEngine := scheduler.New(toEngine)
	vm.Scheduler = scheduler

	go ctx.Log.RecoverAndPanic(func() {
		scheduler.Dispatch(time.Now())
	})

	vm.verifiedBlocks = make(map[ids.ID]Block)

	return vm.ChainVM.Initialize(
		ctx,
		dbManager,
		genesisBytes,
		upgradeBytes,
		configBytes,
		vmToEngine,
		fxs,
	)
}

// block.ChainVM interface implementation
func (vm *VM) BuildBlock() (snowman.Block, error) {
	vm.ctx.Log.Debug("Snowman++ build - call at time %v", time.Now().Format("15:04:05"))
	preferredBlock, err := vm.getBlock(vm.preferred)
	if err != nil {
		return nil, err
	}

	innerBlock, err := vm.ChainVM.BuildBlock()
	if err != nil {
		return nil, err
	}

	return preferredBlock.buildChild(innerBlock)
}

func (vm *VM) ParseBlock(b []byte) (snowman.Block, error) {
	if blk, err := vm.parsePostForkBlock(b); err == nil {
		return blk, nil
	}
	if opt, err := vm.parsePostForkOption(b); err == nil {
		return opt, nil
	}
	return vm.parsePreForkBlock(b)
}

func (vm *VM) GetBlock(id ids.ID) (snowman.Block, error) {
	return vm.getBlock(id)
}

func (vm *VM) SetPreference(preferred ids.ID) error {
	if vm.preferred == preferred {
		return nil
	}
	vm.preferred = preferred

	var (
		prefBlk      snowman.Block
		pChainHeight uint64
	)
	if blk, err := vm.getPostForkBlock(preferred); err == nil {
		if err := vm.ChainVM.SetPreference(blk.innerBlk.ID()); err != nil {
			return err
		}

		prefBlk = blk
		pChainHeight = blk.PChainHeight()
	} else if opt, err := vm.getPostForkOption(preferred); err == nil {
		if err := vm.ChainVM.SetPreference(opt.innerBlk.ID()); err != nil {
			return err
		}

		prefBlk = opt
		pChainHeight, err = opt.pChainHeight()
		if err != nil {
			vm.ctx.Log.Error("Snowman++ set preference - could not retrieve current P-Chain height")
			return err
		}
	} else {
		return vm.ChainVM.SetPreference(preferred)
	}

	// reset scheduler
	minDelay, err := vm.Windower.Delay(prefBlk.Height()+1, pChainHeight, vm.ctx.NodeID)
	if err != nil {
		return err
	}

	nextStartTime := prefBlk.Timestamp().Add(minDelay)
	vm.ctx.Log.Debug("Snowman++ set preference - preferred block ID %s,  timestamp %v; next start time scheduled at %v",
		prefBlk.ID(), prefBlk.Timestamp().Format("15:04:05"), nextStartTime.Format("15:04:05"))
	vm.Scheduler.SetStartTime(nextStartTime)
	return nil
}

func (vm *VM) LastAccepted() (ids.ID, error) {
	lastAccepted, err := vm.State.GetLastAccepted()
	if err == database.ErrNotFound {
		return vm.ChainVM.LastAccepted()
	}
	return lastAccepted, err
}

func (vm *VM) getBlock(id ids.ID) (Block, error) {
	if blk, err := vm.getPostForkBlock(id); err == nil {
		return blk, nil
	}
	if opt, err := vm.getPostForkOption(id); err == nil {
		return opt, nil
	}
	return vm.getPreForkBlock(id)
}

func (vm *VM) getPostForkBlock(blkID ids.ID) (*postForkBlock, error) {
	blkIntf, exists := vm.verifiedBlocks[blkID]
	if exists {
		if blk, ok := blkIntf.(*postForkBlock); ok {
			return blk, nil
		}
		vm.ctx.Log.Debug("object matching requested ID is not a postForkBlock")
		return nil, errUnexpectedBlockType
	}
	statelessBlock, status, err := vm.State.GetBlock(blkID)
	if err != nil {
		return nil, err
	}

	innerBlkBytes := statelessBlock.Block()
	innerBlk, err := vm.ChainVM.ParseBlock(innerBlkBytes)
	if err != nil {
		return nil, err
	}

	return &postForkBlock{
		Block: statelessBlock,
		postForkCommonComponents: postForkCommonComponents{
			vm:       vm,
			innerBlk: innerBlk,
			status:   status,
		},
	}, nil
}

func (vm *VM) getPostForkOption(blkID ids.ID) (*postForkOption, error) {
	optIntf, exists := vm.verifiedBlocks[blkID]
	if exists {
		if opt, ok := optIntf.(*postForkOption); ok {
			return opt, nil
		}
		vm.ctx.Log.Debug("object matching requested ID is not a postForkOption")
		return nil, errUnexpectedBlockType
	}
	option, status, err := vm.State.GetOption(blkID)
	if err != nil {
		return nil, err
	}

	innerBlkBytes := option.Block()
	innerBlk, err := vm.ChainVM.ParseBlock(innerBlkBytes)
	if err != nil {
		return nil, err
	}

	return &postForkOption{
		Option: option,
		postForkCommonComponents: postForkCommonComponents{
			vm:       vm,
			innerBlk: innerBlk,
			status:   status,
		},
	}, nil
}

func (vm *VM) getPreForkBlock(blkID ids.ID) (*preForkBlock, error) {
	blk, err := vm.ChainVM.GetBlock(blkID)
	return &preForkBlock{
		Block: blk,
		vm:    vm,
	}, err
}

func (vm *VM) parsePostForkBlock(b []byte) (*postForkBlock, error) {
	statelessBlock, err := statelessblock.Parse(b)
	if err != nil {
		return nil, err
	}

	// if the block already exists, then make sure the status is set correctly
	blkID := statelessBlock.ID()
	blk, err := vm.getPostForkBlock(blkID)
	if err == nil {
		return blk, nil
	}
	if err != database.ErrNotFound {
		return nil, err
	}

	innerBlkBytes := statelessBlock.Block()
	innerBlk, err := vm.ChainVM.ParseBlock(innerBlkBytes)
	if err != nil {
		return nil, err
	}

	blk = &postForkBlock{
		Block: statelessBlock,
		postForkCommonComponents: postForkCommonComponents{
			vm:       vm,
			innerBlk: innerBlk,
			status:   choices.Processing,
		},
	}
	return blk, vm.storePostForkBlock(blk)
}

func (vm *VM) parsePostForkOption(b []byte) (*postForkOption, error) {
	option, err := option.Parse(b)
	if err != nil {
		return nil, err
	}

	// if the block already exists, then make sure the status is set correctly
	blkID := option.ID()
	opt, err := vm.getPostForkOption(blkID)
	if err == nil {
		return opt, nil
	}
	if err != database.ErrNotFound {
		return nil, err
	}

	innerBlkBytes := option.Block()
	innerBlk, err := vm.ChainVM.ParseBlock(innerBlkBytes)
	if err != nil {
		return nil, err
	}

	opt = &postForkOption{
		Option: option,
		postForkCommonComponents: postForkCommonComponents{
			vm:       vm,
			innerBlk: innerBlk,
			status:   choices.Processing,
		},
	}
	return opt, vm.storePostForkOption(opt)
}

func (vm *VM) parsePreForkBlock(b []byte) (*preForkBlock, error) {
	blk, err := vm.ChainVM.ParseBlock(b)
	return &preForkBlock{
		Block: blk,
		vm:    vm,
	}, err
}

func (vm *VM) storePostForkBlock(blk *postForkBlock) error {
	if err := vm.State.PutBlock(blk.Block, blk.status); err != nil {
		return err
	}
	return vm.db.Commit()
}

func (vm *VM) storePostForkOption(blk *postForkOption) error {
	if err := vm.State.PutOption(blk, blk.status); err != nil {
		return err
	}
	return vm.db.Commit()
}
