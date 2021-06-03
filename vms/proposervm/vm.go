package proposervm

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/codec/reflectcodec"
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
	cdc               codec.Manager
	ErrProBlkNotFound error
)

func init() {
	ErrProBlkNotFound = errors.New("snowmanBlock not found")

	cdc = codec.NewDefaultManager()
	c := linearcodec.NewDefault()

	errs := wrappers.Errs{}
	errs.Add(
		c.RegisterType(&ProposerBlockHeader{}),
		cdc.RegisterCodec(codecVersion, c),
	)

	if errs.Errored() {
		panic(errs.Err)
	}
}

// VM is a decorator for a snowman.ChainVM struct,
// overriding the relevant methods to handle new block fields in snowman++

type VM struct {
	block.ChainVM
	knownProBlocks map[ids.ID]*ProposerBlock
	wrpdToProID    map[ids.ID]ids.ID
}

func NewProVM(vm block.ChainVM) VM {
	return VM{
		ChainVM:        vm,
		knownProBlocks: make(map[ids.ID]*ProposerBlock),
		wrpdToProID:    make(map[ids.ID]ids.ID),
	}
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
	if err := vm.ChainVM.Initialize(ctx, dbManager, genesisBytes, upgradeBytes, configBytes, toEngine, fxs); err != nil {
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

	hdr := NewProHeader(ids.ID{}, 0)
	proGenBlk := NewProBlock(vm, hdr, genesisBlk)
	vm.AddProBlk(&proGenBlk)
	return nil
}

//////// block.ChainVM interface implementation
func (vm *VM) AddProBlk(blk *ProposerBlock) { // exported for UTs
	// TODO: handle update/create
	vm.knownProBlocks[blk.ID()] = blk
	vm.wrpdToProID[blk.Block.ID()] = blk.ID()
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
	hdr := NewProHeader(prntID, time.Now().Unix())
	proBlk := NewProBlock(vm, hdr, sb)
	vm.AddProBlk(&proBlk)
	return &proBlk, nil
}

func (vm *VM) ParseBlock(b []byte) (snowman.Block, error) {
	var hdr ProposerBlockHeader
	cdcVer, err := cdc.Unmarshal(b, &hdr)

	if err != reflectcodec.ErrExtraSpace {
		return nil, fmt.Errorf("couldn't unmarshal proposerBlockHeader: %s", err)
	} else if cdcVer != codecVersion {
		return nil, fmt.Errorf("codecVersion not matching")
	}

	// UGLY way to recover current header length
	dummyB, _ := cdc.Marshal(codecVersion, &hdr)

	sb, err := vm.ChainVM.ParseBlock(b[len(dummyB):])
	if err != nil {
		return nil, err
	}
	proBlk := NewProBlock(vm, hdr, sb)
	vm.AddProBlk(&proBlk)
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
	return false, nil
}

func (vm *VM) Disconnected(validatorID ids.ShortID) (bool, error) {
	if connector, ok := vm.ChainVM.(validators.Connector); ok {
		if err := connector.Disconnected(validatorID); err != nil {
			return ok, err
		}
	}
	return false, nil
}
