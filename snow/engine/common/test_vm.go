// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/ids"

	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/snow"
)

var (
	errInitialize           = errors.New("unexpectedly called Initialize")
	errBootstrapping        = errors.New("unexpectedly called Bootstrapping")
	errBootstrapped         = errors.New("unexpectedly called Bootstrapped")
	errShutdown             = errors.New("unexpectedly called Shutdown")
	errCreateHandlers       = errors.New("unexpectedly called CreateHandlers")
	errCreateStaticHandlers = errors.New("unexpectedly called CreateStaticHandlers")
	errHealthCheck          = errors.New("unexpectedly called HealthCheck")
	errConnected            = errors.New("unexpectedly called Connected")
	errDisconnected         = errors.New("unexpectedly called Disconnected")
	errVersion              = errors.New("unexpectedly called Version")

	_ VM = &TestVM{}
)

// TestVM is a test vm
type TestVM struct {
	T *testing.T

	CantInitialize, CantBootstrapping, CantBootstrapped,
	CantShutdown, CantCreateHandlers, CantCreateStaticHandlers,
	CantHealthCheck, CantConnected, CantDisconnected, CantVersion bool

	InitializeF                              func(*snow.Context, manager.Manager, []byte, []byte, []byte, chan<- Message, []*Fx) error
	BootstrappingF, BootstrappedF, ShutdownF func() error
	CreateHandlersF                          func() (map[string]*HTTPHandler, error)
	CreateStaticHandlersF                    func() (map[string]*HTTPHandler, error)
	ConnectedF                               func(ids.ShortID) error
	DisconnectedF                            func(ids.ShortID) error
	HealthCheckF                             func() (interface{}, error)
	VersionF                                 func() (string, error)
}

func (vm *TestVM) Default(cant bool) {
	vm.CantInitialize = cant
	vm.CantBootstrapping = cant
	vm.CantBootstrapped = cant
	vm.CantShutdown = cant
	vm.CantCreateHandlers = cant
	vm.CantCreateStaticHandlers = cant
	vm.CantHealthCheck = cant
}

func (vm *TestVM) Initialize(ctx *snow.Context, db manager.Manager, genesisBytes []byte, upgradeBytes []byte, configBytes []byte, msgChan chan<- Message, fxs []*Fx) error {
	if vm.InitializeF != nil {
		return vm.InitializeF(ctx, db, genesisBytes, upgradeBytes, configBytes, msgChan, fxs)
	}
	if vm.CantInitialize && vm.T != nil {
		vm.T.Fatal(errInitialize)
	}
	return errInitialize
}

func (vm *TestVM) Bootstrapping() error {
	if vm.BootstrappingF != nil {
		return vm.BootstrappingF()
	}
	if vm.CantBootstrapping {
		if vm.T != nil {
			vm.T.Fatal(errBootstrapping)
		}
		return errBootstrapping
	}
	return nil
}

func (vm *TestVM) Bootstrapped() error {
	if vm.BootstrappedF != nil {
		return vm.BootstrappedF()
	}
	if vm.CantBootstrapped {
		if vm.T != nil {
			vm.T.Fatal(errBootstrapped)
		}
		return errBootstrapped
	}
	return nil
}

func (vm *TestVM) Shutdown() error {
	if vm.ShutdownF != nil {
		return vm.ShutdownF()
	}
	if vm.CantShutdown {
		if vm.T != nil {
			vm.T.Fatal(errShutdown)
		}
		return errShutdown
	}
	return nil
}

func (vm *TestVM) CreateHandlers() (map[string]*HTTPHandler, error) {
	if vm.CreateHandlersF != nil {
		return vm.CreateHandlersF()
	}
	if vm.CantCreateHandlers && vm.T != nil {
		vm.T.Fatal(errCreateHandlers)
	}
	return nil, nil
}

func (vm *TestVM) CreateStaticHandlers() (map[string]*HTTPHandler, error) {
	if vm.CreateStaticHandlersF != nil {
		return vm.CreateStaticHandlersF()
	}
	if vm.CantCreateStaticHandlers && vm.T != nil {
		vm.T.Fatal(errCreateStaticHandlers)
	}
	return nil, nil
}

func (vm *TestVM) HealthCheck() (interface{}, error) {
	if vm.HealthCheckF != nil {
		return vm.HealthCheckF()
	}
	if vm.CantHealthCheck && vm.T != nil {
		vm.T.Fatal(errHealthCheck)
	}
	return nil, errHealthCheck
}

func (vm *TestVM) Connected(id ids.ShortID) error {
	if vm.ConnectedF != nil {
		return vm.ConnectedF(id)
	}
	if vm.CantConnected && vm.T != nil {
		vm.T.Fatal(errConnected)
	}
	return nil
}

func (vm *TestVM) Disconnected(id ids.ShortID) error {
	if vm.DisconnectedF != nil {
		return vm.DisconnectedF(id)
	}
	if vm.CantDisconnected && vm.T != nil {
		vm.T.Fatal(errDisconnected)
	}
	return nil
}

func (vm *TestVM) Version() (string, error) {
	if vm.VersionF != nil {
		return vm.VersionF()
	}
	if vm.CantVersion && vm.T != nil {
		vm.T.Fatal(errVersion)
	}
	return "", nil
}
