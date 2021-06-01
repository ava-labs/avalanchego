package proposervm

import (
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
)

// VM is a decorator for a snowman.ChainVM struct,
// overriding the relevant methods to handle new block fields in snowman++

type VM struct {
	wrappedVM block.ChainVM
}

func NewProVM(vm block.ChainVM) VM {
	return VM{
		wrappedVM: vm,
	}
}

//////// Common.VM interface implementation
func (vm *VM) Initialize(
	ctx *snow.Context,
	dbManager manager.Manager,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	toEngine chan<- common.Message,
	fxs []*common.Fx,
) error {
	// simply a forwarder
	return vm.wrappedVM.Initialize(ctx, dbManager, genesisBytes, upgradeBytes, configBytes, toEngine, fxs)
}

func (vm *VM) Bootstrapping() error {
	// simply a forwarder
	return vm.wrappedVM.Bootstrapping()
}

func (vm *VM) Bootstrapped() error {
	// simply a forwarder
	return vm.wrappedVM.Bootstrapped()
}

func (vm *VM) Shutdown() error {
	// simply a forwarder
	return vm.wrappedVM.Shutdown()
}

func (vm *VM) CreateHandlers() (map[string]*common.HTTPHandler, error) {
	// simply a forwarder
	return vm.wrappedVM.CreateHandlers()
}

func (vm *VM) HealthCheck() (interface{}, error) {
	return vm.wrappedVM.HealthCheck()
}

//////// block.ChainVM interface implementation
func (vm *VM) BuildProBlock() (ProposerBlock, error) { //NO MORE block.ChainVM interface
	sb, err := vm.wrappedVM.BuildBlock()
	proBlk := NewProBlock(sb) // here new block fields will be handled
	return proBlk, err
}

func (vm *VM) ParseProBlock(b []byte) (ProposerBlock, error) { //NO MORE block.ChainVM interface
	sb, err := vm.wrappedVM.ParseBlock(b)
	proBlk := NewProBlock(sb) // here new block fields will be handled
	return proBlk, err
}

func (vm *VM) GetProBlock(id ids.ID) (ProposerBlock, error) { //NO MORE block.ChainVM interface
	sb, err := vm.wrappedVM.GetBlock(id)
	proBlk := NewProBlock(sb) // here new block fields will be handled
	return proBlk, err
}

func (vm *VM) SetPreference(id ids.ID) error {
	// simply a forwarder
	return vm.wrappedVM.SetPreference(id)
}

func (vm *VM) LastAccepted() (ids.ID, error) {
	// simply a forwarder
	return vm.wrappedVM.LastAccepted()
}

//////// Connector VMs handling
func (vm *VM) Connected(validatorID ids.ShortID) (bool, error) {
	if connector, ok := vm.wrappedVM.(validators.Connector); ok {
		if err := connector.Connected(validatorID); err != nil {
			return ok, err
		}
	}
	return false, nil
}

func (vm *VM) Disconnected(validatorID ids.ShortID) (bool, error) {
	if connector, ok := vm.wrappedVM.(validators.Connector); ok {
		if err := connector.Disconnected(validatorID); err != nil {
			return ok, err
		}
	}
	return false, nil
}
