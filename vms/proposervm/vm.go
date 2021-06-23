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
	"crypto/tls"
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
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
	"github.com/ava-labs/avalanchego/vms/proposervm/state"
	"github.com/ava-labs/avalanchego/vms/proposervm/tree"

	statelessblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
)

var (
	NoProposerBlocks = time.Unix(1<<63-62135596801, 999999999)
	dbPrefix         = []byte("proposervm")
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
	state.State
	proposer.Windower
	tree.Tree

	db *versiondb.Database

	clock

	// node identity attributes
	stakingCert tls.Certificate
	nodeID      ids.ShortID

	scheduler *scheduler

	proBlkActivationTime time.Time

	preferred ids.ID
}

func NewProVM(vm block.ChainVM, proBlkStart time.Time) *VM {
	return &VM{
		ChainVM:              vm,
		clock:                clockImpl{},
		proBlkActivationTime: proBlkStart,
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

	vm.stakingCert = ctx.StakingCert

	var err error
	if vm.nodeID, err = ids.ToShortID(hashing.PubkeyBytesToAddress(vm.stakingCert.Leaf.Raw)); err != nil {
		return err
	}

	vm.scheduler = &scheduler{}
	if err := vm.scheduler.initialize(vm, toEngine); err != nil {
		return err
	}

	err = vm.ChainVM.Initialize(
		ctx,
		dbManager,
		genesisBytes,
		upgradeBytes,
		configBytes,
		vm.scheduler.coreVMChannel(),
		fxs,
	)
	if err != nil {
		return err
	}

	preferred, err := vm.LastAccepted()
	if err != nil {
		return err
	}
	vm.preferred = preferred

	vm.scheduler.rescheduleBlkTicker()
	go vm.scheduler.handleBlockTiming()

	return nil
}

// block.ChainVM interface implementation
func (vm *VM) BuildBlock() (snowman.Block, error) {
	sb, err := vm.ChainVM.BuildBlock()
	if err != nil {
		return nil, err
	}

	h, err := vm.PChainHeight()
	if err != nil {
		return nil, err
	}

	slb, err := statelessblock.Build(
		vm.preferred,
		sb.Timestamp(),
		h,
		vm.stakingCert.Leaf,
		sb.Bytes(),
		vm.stakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		return nil, err
	}

	proBlk := ProposerBlock{
		Block:   slb,
		vm:      vm,
		coreBlk: sb,
		status:  choices.Processing,
	}

	// TODO: Why is verify called here?
	if err := proBlk.Verify(); err != nil {
		return nil, err
	}

	if err := vm.PutBlock(slb, proBlk.Status()); err != nil {
		return nil, err
	}

	return &proBlk, nil
}

func (vm *VM) ParseBlock(b []byte) (snowman.Block, error) {
	block, err := vm.parseProposerBlock(b)
	if err == nil {
		return block, nil
	}
	return vm.ChainVM.ParseBlock(b)
}

func (vm *VM) parseProposerBlock(b []byte) (*ProposerBlock, error) {
	slb, err := statelessblock.Parse(b)
	if err != nil {
		return nil, err
	}

	coreBlk, err := vm.ChainVM.ParseBlock(slb.Block())
	if err != nil {
		return nil, err
	}

	block := &ProposerBlock{
		Block:   slb,
		vm:      vm,
		coreBlk: coreBlk,
		status:  choices.Processing,
	}

	_, status, err := vm.State.GetBlock(slb.ID())
	if err == nil {
		block.status = status
		return block, nil
	}
	if err != database.ErrNotFound {
		return nil, err
	}

	if err := vm.State.PutBlock(slb, choices.Processing); err != nil {
		return nil, err
	}
	return block, nil
}

func (vm *VM) GetBlock(id ids.ID) (snowman.Block, error) {
	if slb, status, err := vm.State.GetBlock(id); err == nil {
		coreBlk, err := vm.ChainVM.ParseBlock(slb.Block())
		if err != nil {
			return nil, err
		}

		res := &ProposerBlock{
			Block:   slb,
			vm:      vm,
			coreBlk: coreBlk,
			status:  status,
		}

		return res, nil
	}

	// check whether block is core one, with no proposerBlockHeader
	if coreBlk, err := vm.ChainVM.GetBlock(id); err == nil {
		return coreBlk, nil
	}
	return nil, ErrProBlkNotFound
}

func (vm *VM) SetPreference(preferred ids.ID) error {
	if slb, _, err := vm.State.GetBlock(preferred); err == nil {
		vm.preferred = preferred
		coreBlk, err := vm.ChainVM.ParseBlock(slb.Block())
		if err != nil {
			return err
		} // TODO: update block status in DB as well

		return vm.ChainVM.SetPreference(coreBlk.ID())
	}

	// check whether block is core one, with no proposerBlockHeader
	return vm.ChainVM.SetPreference(preferred)
}

func (vm *VM) LastAccepted() (ids.ID, error) {
	res, err := vm.State.GetLastAccepted()
	if err == nil {
		return res, nil
	}
	if err != database.ErrNotFound {
		return ids.ID{}, err
	}
	// pre snowman++ case
	return vm.ChainVM.LastAccepted()
}
