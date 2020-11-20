package avm

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/avm/internalvm"
	"github.com/ava-labs/avalanchego/vms/avm/rpcapi"
	"github.com/ava-labs/avalanchego/vms/avm/service"
)

type VM struct {
	avm     *internalvm.VM
	rpcapi  *rpcapi.RPCAPI
	service *service.Service
}

func NewVM(creationTxFee uint64, txFee uint64) *VM {
	internalVM := internalvm.NewVM(creationTxFee, txFee)
	return &VM{
		avm:     internalVM,
		rpcapi:  rpcapi.NewRPCAPI(),
		service: service.NewService(internalVM),
	}
}

// Initialize implements the avalanche.DAGVM interface
func (vm *VM) Initialize(
	ctx *snow.Context,
	db database.Database,
	genesisBytes []byte,
	toEngine chan<- common.Message,
	fxs []*common.Fx,
) error {
	return vm.avm.Initialize(ctx, db, genesisBytes, toEngine, fxs)
}

// Bootstrapping is called by the consensus engine when it starts bootstrapping
// this chain
func (vm *VM) Bootstrapping() error {
	return vm.avm.Bootstrapping()
}

// Bootstrapped is called by the consensus engine when it is done bootstrapping
// this chain
func (vm *VM) Bootstrapped() error {
	return vm.avm.Bootstrapped()
}

// Shutdown implements the avalanche.DAGVM interface
func (vm *VM) Shutdown() error {
	return vm.avm.Shutdown()
}

// CreateHandlers implements the avalanche.DAGVM interface
func (vm *VM) CreateHandlers() map[string]*common.HTTPHandler {
	return vm.rpcapi.CreateHandlers(vm.avm, vm.service)
}

// CreateStaticHandlers implements the avalanche.DAGVM interface
func (vm *VM) CreateStaticHandlers() map[string]*common.HTTPHandler {
	return vm.rpcapi.CreateStaticHandlers(vm)
}

// Returns nil if the VM is healthy.
// Periodically called and reported via the node's Health API.
func (vm *VM) Health() (interface{}, error) {
	return vm.avm.Health()
}
