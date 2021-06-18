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
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
)

var (
	genesisParentID      = ids.Empty
	NoProposerBlocks     = time.Unix(1<<63-62135596801, 999999999)
	ErrCannotSignWithKey = errors.New("unable to use key to sign proposer blocks")
)

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
	stakingCert     tls.Certificate
	fromCoreVM      chan common.Message
	toEngine        chan<- common.Message
	proBlkStartTime time.Time
}

func NewProVM(vm block.ChainVM, proBlkStart time.Time) *VM {
	res := VM{
		ChainVM:         vm,
		clock:           clockImpl{},
		fromCoreVM:      nil,
		toEngine:        nil,
		proBlkStartTime: proBlkStart,
	}
	res.state = newState(&res)
	return &res
}

func (vm *VM) handleBlockTiming() {
	msg := <-vm.fromCoreVM
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

	vm.stakingCert = ctx.StakingCert
	if ctx.ValidatorVM != nil {
		vm.windower.VM = ctx.ValidatorVM
	} else {
		// a nil ctx.ValidatorVM is expected only if we are wrapping P-chain VM itself.
		// Then core VM must implement the validators.VM interface
		if valVM, ok := vm.ChainVM.(validators.VM); ok {
			vm.windower.VM = valVM
		} else {
			return fmt.Errorf("core VM does not implement validators.VM interface")
		}
	}

	// TODO: comparison should be with genesis timestamp, not with Now()
	if vm.now().After(vm.proBlkStartTime) {
		// proposerVM intercepts VM events for blocks and times event relay to consensus
		vm.toEngine = toEngine
		vm.fromCoreVM = make(chan common.Message, len(toEngine))

		// Assuming genesisBytes has not proposerBlockHeader
		if err := vm.ChainVM.Initialize(ctx, dbManager, genesisBytes, upgradeBytes,
			configBytes, vm.fromCoreVM, fxs); err != nil {
			return err
		}

		_, err := vm.state.getProGenesisBlk()
		switch err {
		case ErrGenesisNotFound:
			// rebuild genesis and store it
			coreGenID, err := vm.ChainVM.LastAccepted()
			if err != nil {
				return err
			}
			coreGenBlk, err := vm.ChainVM.GetBlock(coreGenID)
			if err != nil {
				return err
			}
			proGenHdr := NewProHeader(genesisParentID, coreGenBlk.Timestamp().Unix(), 0, x509.Certificate{})
			proGenBlk, _ := NewProBlock(vm, proGenHdr, coreGenBlk, nil, false) // not signing block, cannot err
			// Skipping verification for genesis block.
			if err := vm.state.storeProGenID(proGenBlk.ID()); err != nil {
				return err
			}
			if err := vm.state.storePreference(proGenBlk.ID()); err != nil {
				return err
			}
			if err := vm.state.storeLastAcceptedID(proGenBlk.ID()); err != nil {
				return err
			}
			if err := vm.state.storeProBlk(&proGenBlk); err != nil {
				return err
			}
		case nil: // TODO: do checks on Preference and LastAcceptedID or just keep going?
		default:
			return err
		}

		go vm.handleBlockTiming()
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
	if vm.now().After(vm.proBlkStartTime) {
		proParentID, err := vm.state.getPreferredID()
		if err != nil {
			return nil, err
		}

		h, err := vm.pChainHeight()
		if err != nil {
			return nil, err
		}
		hdr := NewProHeader(proParentID, sb.Timestamp().Unix(), h, *vm.stakingCert.Leaf)
		proBlk, err := NewProBlock(vm, hdr, sb, nil, true)
		if err != nil {
			return nil, err
		}

		if err := proBlk.Verify(); err != nil {
			return nil, err
		}

		if err := vm.state.storeProBlk(&proBlk); err != nil {
			return nil, err
		}
		return &proBlk, nil
	}
	return sb, nil
}

func (vm *VM) ParseBlock(b []byte) (snowman.Block, error) {
	var mPb marshallingProposerBLock
	if err := mPb.unmarshal(b); err == nil {
		sb, err := vm.ChainVM.ParseBlock(mPb.wrpdBytes)
		if err != nil {
			return nil, err
		}

		proBlk, _ := NewProBlock(vm, mPb.ProposerBlockHeader, sb, b, false) // not signing block, cannot err
		if err := vm.state.storeProBlk(&proBlk); err != nil {
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
	currPrefID, err := vm.state.getPreferredID()
	switch err {
	case nil:
		proBlk, err := vm.state.getProBlock(id)
		if err != nil {
			return err
		}
		if err := vm.state.storePreference(id); err != nil {
			return err
		}
		if err := vm.ChainVM.SetPreference(proBlk.coreBlk.ID()); err != nil {
			// attempt restoring previous proposer block reference and return error
			if err := vm.state.storePreference(currPrefID); err != nil {
				// TODO log
				return err
			}
			return err
		}
		return nil
	case ErrPreferredIDNotFound: // pre snowman++ case
		return vm.ChainVM.SetPreference(id)
	default:
		return err
	}
}

func (vm *VM) LastAccepted() (ids.ID, error) {
	res, err := vm.state.getLastAcceptedID()
	switch err {
	case nil:
		return res, nil
	case ErrLastAcceptedIDNotFound: // pre snowman++ case
		return vm.ChainVM.LastAccepted()
	default:
		return res, err
	}
}
