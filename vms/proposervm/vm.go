package proposervm

// VM is a decorator for a snowman.ChainVM struct,
// overriding the relevant methods to handle block headers introduced with snowman++
// Design guidelines:
// Calls to wrapped VM can be expensive (e.g. sent over gRPC); be frugal

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	codecVersion = 1987
)

var (
	cdc                    = codec.NewDefaultManager()
	ErrInnerVMNotConnector = errors.New("chainVM wrapped in proposerVM does not implement snowman.Connector")
)

func init() {
	c := linearcodec.NewDefault()

	errs := wrappers.Errs{}
	errs.Add(
		c.RegisterType(&marshallingProposerBLock{}),
		cdc.RegisterCodec(codecVersion, c),
	)

	if errs.Errored() {
		panic(errs.Err)
	}
}

type clock interface {
	now() time.Time
}

type clockImpl struct{}

func (c clockImpl) now() time.Time {
	return time.Now()
}

type VM struct {
	block.ChainVM
	knownProBlocks map[ids.ID]*ProposerBlock
	wrpdToProID    map[ids.ID]ids.ID
	clk            clock
	fromWrappedVM  chan common.Message
	toEngine       chan<- common.Message
}

func NewProVM(vm block.ChainVM) VM {
	return VM{
		ChainVM:        vm,
		knownProBlocks: make(map[ids.ID]*ProposerBlock),
		wrpdToProID:    make(map[ids.ID]ids.ID),
		clk:            clockImpl{},
		fromWrappedVM:  nil,
		toEngine:       nil,
	}
}

func (vm *VM) handleBlockTiming() {
	msg := <-vm.fromWrappedVM
	vm.toEngine <- msg
}

//////// common.VM interface implementation
func (vm *VM) Initialize(
	ctx *snow.Context,
	dbManager manager.Manager,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	toEngine chan<- common.Message,
	fxs []*common.Fx,
) error {
	// proposerVM intercepts VM events for blocks and times event relay to consensus
	vm.toEngine = toEngine
	vm.fromWrappedVM = make(chan common.Message, len(toEngine))

	if err := vm.ChainVM.Initialize(ctx, dbManager, genesisBytes, upgradeBytes, configBytes, vm.fromWrappedVM, fxs); err != nil {
		return err
	}

	// Store genesis
	genesisID, err := vm.ChainVM.LastAccepted()
	if err != nil {
		return err
	}

	genesisBlk, err := vm.ChainVM.GetBlock(genesisID)
	if err != nil {
		return err
	}

	hdr := NewProHeader(ids.ID{}, 0, 0)
	proGenBlk := NewProBlock(vm, hdr, genesisBlk, nil)
	if err := vm.addProBlk(&proGenBlk); err != nil {
		return err
	}

	go vm.handleBlockTiming()
	return nil
}

//////// block.ChainVM interface implementation
func (vm *VM) addProBlk(blk *ProposerBlock) error { // exported for UTs
	// TODO: handle update/create
	vm.knownProBlocks[blk.ID()] = blk
	vm.wrpdToProID[blk.Block.ID()] = blk.ID()
	return nil
}

func (vm *VM) BuildBlock() (snowman.Block, error) {
	sb, err := vm.ChainVM.BuildBlock()
	if err != nil {
		return nil, err
	}
	prntID, ok := vm.wrpdToProID[sb.Parent().ID()]
	if !ok {
		return nil, ErrProBlkNotFound
	}
	hdr := NewProHeader(prntID, vm.clk.now().Unix(), vm.knownProBlocks[prntID].Height()+1)
	proBlk := NewProBlock(vm, hdr, sb, nil)

	if err := proBlk.Verify(); err != nil {
		return nil, err
	}

	// Skipping verification for genesis block.
	// Should we instead check that genesis state is accepted && skip verification for accepted blocks?
	if err := vm.addProBlk(&proBlk); err != nil {
		return nil, err
	}
	return &proBlk, nil
}

func (vm *VM) ParseBlock(b []byte) (snowman.Block, error) {
	var mPb marshallingProposerBLock
	cdcVer, err := cdc.Unmarshal(b, &mPb)

	if err != nil {
		return nil, fmt.Errorf("couldn't unmarshal proposerBlockHeader: %s", err)
	} else if cdcVer != codecVersion {
		return nil, fmt.Errorf("codecVersion not matching")
	}

	sb, err := vm.ChainVM.ParseBlock(mPb.WrpdBytes)
	if err != nil {
		return nil, err
	}

	proBlk := NewProBlock(vm, mPb.Header, sb, b)

	if err := proBlk.Verify(); err != nil {
		return nil, err
	}

	if err := vm.addProBlk(&proBlk); err != nil {
		return nil, err
	}

	return &proBlk, nil
}

func (vm *VM) GetBlock(id ids.ID) (snowman.Block, error) {
	if proBlk, ok := vm.knownProBlocks[id]; ok {
		return proBlk, nil
	}
	return nil, ErrProBlkNotFound
}

func (vm *VM) SetPreference(id ids.ID) error {
	if _, ok := vm.knownProBlocks[id]; !ok {
		return ErrProBlkNotFound // ASSERT THAT wrpdToProID has not key for value id??
	}

	err := vm.ChainVM.SetPreference(vm.knownProBlocks[id].Block.ID())
	return err
}

func (vm *VM) LastAccepted() (ids.ID, error) {
	wrpdID, err := vm.ChainVM.LastAccepted()
	if err != nil {
		return ids.ID{}, err
	}

	proID, ok := vm.wrpdToProID[wrpdID]
	if !ok {
		return ids.ID{}, ErrProBlkNotFound // Is this possible at all??
	}
	return proID, nil
}

//////// Connector VMs handling
func (vm *VM) Connected(validatorID ids.ShortID) (bool, error) {
	if connector, ok := vm.ChainVM.(validators.Connector); ok {
		if err := connector.Connected(validatorID); err != nil {
			return ok, err
		}
	}
	return false, ErrInnerVMNotConnector
}

func (vm *VM) Disconnected(validatorID ids.ShortID) (bool, error) {
	if connector, ok := vm.ChainVM.(validators.Connector); ok {
		if err := connector.Disconnected(validatorID); err != nil {
			return ok, err
		}
	}
	return false, ErrInnerVMNotConnector
}
