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

//////// clock interface and implementation, to ease up UTs
type clock interface {
	now() time.Time
}

type clockImpl struct{}

func (c clockImpl) now() time.Time {
	return time.Now()
}

type VM struct {
	block.ChainVM
	state         *innerState
	clk           clock
	fromWrappedVM chan common.Message
	toEngine      chan<- common.Message
}

func NewProVM(vm block.ChainVM) VM {
	res := VM{
		ChainVM:       vm,
		clk:           clockImpl{},
		fromWrappedVM: nil,
		toEngine:      nil,
	}
	res.state = newState(&res)
	return res
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
	vm.state.init(dbManager.Current().Database) // TODO: keep VM state synced with wrappedVM's one

	// proposerVM intercepts VM events for blocks and times event relay to consensus
	vm.toEngine = toEngine
	vm.fromWrappedVM = make(chan common.Message, len(toEngine))

	// Assuming genesisBytes has not proposerBlockHeader
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

	// Skipping verification for genesis block.
	// Should we instead check that genesis state is accepted && skip verification for accepted blocks?
	vm.state.cacheProBlk(&proGenBlk)
	if err := vm.state.storeBlk(&proGenBlk); err != nil {
		// TODO: remove blk from cache??
		return err
	}

	go vm.handleBlockTiming()
	return nil
}

//////// block.ChainVM interface implementation
func (vm *VM) BuildBlock() (snowman.Block, error) {
	sb, err := vm.ChainVM.BuildBlock()
	if err != nil {
		return nil, err
	}

	proParent, err := vm.state.getBlockFromWrappedBlkID(sb.Parent().ID())
	if err != nil {
		return nil, err
	}

	hdr := NewProHeader(proParent.ID(), vm.clk.now().Unix(), proParent.Height()+1)
	proBlk := NewProBlock(vm, hdr, sb, nil)

	if err := proBlk.Verify(); err != nil {
		return nil, err
	}

	vm.state.cacheProBlk(&proBlk)
	if err := vm.state.storeBlk(&proBlk); err != nil {
		// TODO: remove blk from cache??
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

	vm.state.cacheProBlk(&proBlk)
	if err := vm.state.storeBlk(&proBlk); err != nil {
		// TODO: remove blk from cache??
		return nil, err
	}

	return &proBlk, nil
}

func (vm *VM) GetBlock(id ids.ID) (snowman.Block, error) {
	return vm.state.getBlock(id)
}

func (vm *VM) SetPreference(id ids.ID) error {
	proBlk, err := vm.state.getBlock(id)
	if err != nil {
		// TODO: log error
		return ErrProBlkNotFound
	}

	err = vm.ChainVM.SetPreference(proBlk.Block.ID())
	return err
}

func (vm *VM) LastAccepted() (ids.ID, error) {
	wrpdID, err := vm.ChainVM.LastAccepted()
	if err != nil {
		return ids.ID{}, err
	}

	proBlk, err := vm.state.getBlockFromWrappedBlkID(wrpdID)
	if err != nil {
		return ids.ID{}, ErrProBlkNotFound // Is this possible at all??
	}

	return proBlk.ID(), nil
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
