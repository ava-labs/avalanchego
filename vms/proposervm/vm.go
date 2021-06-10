package proposervm

// VM is a decorator for a snowman.ChainVM struct, created to handle block headers introduced with snowman++

// Contract
// * After initialization. full ProposerBlocks (proHeader + core block ) are stored in proposervm.VM's db
// on Build/ParseBlock, AFTER calls to core vm's Build/ParseBlock, which we ASSUME
//  would store core block on core VM's db.
// * ProposerVM do not track ProposerBlock state; instead state relate calls (Accept/Reject/Status) are
// forwarded to the core VM. Since block registration HAPPENS BEFORE block status settings,
// proposerVM is guaranteed not to lose the last accepted block
// * ProposerVM can handle both ProposerVM blocks AND generic snowman.Block not wrapped with a ProposerBlocHeader
// This allows all snowman-like VM freedom to select a time after which introduce the congestion control mechanism
// implemented via the proposer block header

import (
	"crypto"
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
	NoProposerBlocks       = time.Unix(1<<63-62135596801, 999999999)
	cdc                    = codec.NewDefaultManager()
	ErrInnerVMNotConnector = errors.New("chainVM wrapped in proposerVM does not implement snowman.Connector")
	ErrCannotSignWithKey   = errors.New("unable to use key to sign proposer blocks")
)

func init() {
	c := linearcodec.NewDefault()

	errs := wrappers.Errs{}
	errs.Add(
		c.RegisterType(&marshallingProposerBLock{}),
		c.RegisterType(&ids.ID{}),
		cdc.RegisterCodec(codecVersion, c),
	)

	if errs.Errored() {
		panic(errs.Err)
	}
}

// clock interface and implementation, to ease up UTs
type clock interface {
	now() time.Time
}

type clockImpl struct{}

func (c clockImpl) now() time.Time {
	return time.Now()
}

type VM struct {
	block.ChainVM
	state *innerState
	windower
	clock
	stakingKey      crypto.Signer
	fromWrappedVM   chan common.Message
	toEngine        chan<- common.Message
	proBlkStartTime time.Time
}

func NewProVM(vm block.ChainVM, proBlkStart time.Time) VM {
	res := VM{
		ChainVM:         vm,
		clock:           clockImpl{},
		stakingKey:      nil,
		fromWrappedVM:   nil,
		toEngine:        nil,
		proBlkStartTime: proBlkStart,
	}
	res.state = newState(&res)
	return res
}

func (vm *VM) handleBlockTiming() {
	msg := <-vm.fromWrappedVM
	vm.toEngine <- msg
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
	vm.state.init(dbManager.Current().Database)

	pKey := *ctx.StakingKey
	signer, ok := pKey.(crypto.Signer)
	if !ok {
		return ErrCannotSignWithKey
	}

	vm.stakingKey = signer

	// TODO: comparison should be with genesis timestamp, not with Now()
	if time.Now().After(vm.proBlkStartTime) {
		// proposerVM intercepts VM events for blocks and times event relay to consensus
		vm.toEngine = toEngine
		vm.fromWrappedVM = make(chan common.Message, len(toEngine))

		// Assuming genesisBytes has not proposerBlockHeader
		if err := vm.ChainVM.Initialize(ctx, dbManager, genesisBytes, upgradeBytes,
			configBytes, vm.fromWrappedVM, fxs); err != nil {
			return err
		}

		// Store genesis
		genesisID, err := vm.ChainVM.LastAccepted()
		if err != nil {
			return err
		}

		if _, err := vm.state.getBlockFromWrappedBlkID(genesisID); err != nil {
			// genesis not stored
			genesisBlk, err := vm.ChainVM.GetBlock(genesisID)
			if err != nil {
				return err
			}

			hdr := NewProHeader(ids.ID{}, 0, 0)
			proGenBlk, _ := NewProBlock(vm, hdr, genesisBlk, nil, false) // not signing block, cannot err

			// Skipping verification for genesis block.
			vm.state.cacheProBlk(&proGenBlk)
			if err := vm.state.commitBlk(&proGenBlk); err != nil {
				return err
			}

			go vm.handleBlockTiming()
		}
	} else if err := vm.ChainVM.Initialize(ctx, dbManager, genesisBytes, upgradeBytes,
		configBytes, toEngine, fxs); err != nil {
		return err
	}

	return nil
}

// block.ChainVM interface implementation
func (vm *VM) BuildBlock() (snowman.Block, error) {
	sb, err := vm.ChainVM.BuildBlock()
	if err != nil {
		return nil, err
	}

	// TODO: comparison should be with genesis timestamp, not with Now()
	if time.Now().After(vm.proBlkStartTime) {
		proParent, err := vm.state.getBlockFromWrappedBlkID(sb.Parent().ID())
		if err != nil {
			return nil, err
		}

		hdr := NewProHeader(proParent.ID(), vm.now().Unix(), vm.pChainHeight())
		proBlk, err := NewProBlock(vm, hdr, sb, nil, true)
		if err != nil {
			return nil, err
		}

		if err := proBlk.Verify(); err != nil {
			return nil, err
		}

		vm.state.cacheProBlk(&proBlk)
		if err := vm.state.commitBlk(&proBlk); err != nil {
			return nil, err
		}
		return &proBlk, nil
	}
	return sb, nil
}

func (vm *VM) tryParseAsProposerBlock(b []byte) (*marshallingProposerBLock, error) {
	var mPb marshallingProposerBLock
	cdcVer, err := cdc.Unmarshal(b, &mPb)

	if err != nil {
		return nil, fmt.Errorf("couldn't unmarshal proposerBlockHeader: %s", err)
	} else if cdcVer != codecVersion {
		return nil, fmt.Errorf("codecVersion not matching")
	}
	return &mPb, nil
}

func (vm *VM) ParseBlock(b []byte) (snowman.Block, error) {
	mPb, err := vm.tryParseAsProposerBlock(b)
	if err == nil {
		sb, err := vm.ChainVM.ParseBlock(mPb.WrpdBytes)
		if err != nil {
			return nil, err
		}

		proBlk, _ := NewProBlock(vm, mPb.Header, sb, b, false) // not signing block, cannot err

		vm.state.cacheProBlk(&proBlk)
		if err := vm.state.commitBlk(&proBlk); err != nil {
			return nil, err
		}

		return &proBlk, nil
	}

	// try parse it as a core block
	sb, err := vm.ChainVM.ParseBlock(b)
	if err != nil {
		return nil, err
	}
	// no caching of core blocks into ProposerVM
	return sb, nil
}

func (vm *VM) GetBlock(id ids.ID) (snowman.Block, error) {
	if res, err := vm.state.getProBlock(id); err == nil {
		return res, nil
	}

	// check whether block is core one, with no proposerBlockHeader
	if coreBlk, err := vm.ChainVM.GetBlock(id); err == nil {
		return coreBlk, nil
	}

	return nil, ErrProBlkNotFound
}

func (vm *VM) SetPreference(id ids.ID) error {
	if proBlk, err := vm.state.getProBlock(id); err == nil {
		return vm.ChainVM.SetPreference(proBlk.coreBlk.ID())
	}

	// check whether block is core one, with no proposerBlockHeader
	return vm.ChainVM.SetPreference(id)
}

func (vm *VM) LastAccepted() (ids.ID, error) {
	coreID, err := vm.ChainVM.LastAccepted()
	if err != nil {
		return ids.ID{}, err
	}

	if proBlk, err := vm.state.getBlockFromWrappedBlkID(coreID); err == nil {
		return proBlk.ID(), nil
	}

	// no proposerBlock wrapping core block; return coreID
	return coreID, nil
}

// Connector VMs handling
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
