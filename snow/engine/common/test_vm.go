// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow"
)

var (
	errInitialize = errors.New("unexpectedly called Initialize")
)

// TestVM is a test vm
type TestVM struct {
	T *testing.T

	CantInitialize, CantBootstrapping, CantBootstrapped, CantShutdown, CantCreateHandlers, CantCreateStaticHandlers bool

	InitializeF                              func(*snow.Context, database.Database, []byte, chan<- Message, []*Fx) error
	BootstrappingF, BootstrappedF, ShutdownF func() error
	CreateHandlersF                          func() map[string]*HTTPHandler
	CreateStaticHandlersF                    func() map[string]*HTTPHandler
}

// Default ...
func (vm *TestVM) Default(cant bool) {
	vm.CantInitialize = cant
	vm.CantBootstrapping = cant
	vm.CantBootstrapped = cant
	vm.CantShutdown = cant
	vm.CantCreateHandlers = cant
	vm.CantCreateStaticHandlers = cant
}

// Initialize ...
func (vm *TestVM) Initialize(ctx *snow.Context, db database.Database, initState []byte, msgChan chan<- Message, fxs []*Fx) error {
	if vm.InitializeF != nil {
		return vm.InitializeF(ctx, db, initState, msgChan, fxs)
	}
	if vm.CantInitialize && vm.T != nil {
		vm.T.Fatal(errInitialize)
	}
	return errInitialize
}

// Bootstrapping ...
func (vm *TestVM) Bootstrapping() error {
	if vm.BootstrappingF != nil {
		return vm.BootstrappingF()
	} else if vm.CantBootstrapping {
		if vm.T != nil {
			vm.T.Fatalf("Unexpectedly called Bootstrapping")
		}
		return errors.New("unexpectedly called Bootstrapping")
	}
	return nil
}

// Bootstrapped ...
func (vm *TestVM) Bootstrapped() error {
	if vm.BootstrappedF != nil {
		return vm.BootstrappedF()
	} else if vm.CantBootstrapped {
		if vm.T != nil {
			vm.T.Fatalf("Unexpectedly called Bootstrapped")
		}
		return errors.New("unexpectedly called Bootstrapped")
	}
	return nil
}

// Shutdown ...
func (vm *TestVM) Shutdown() error {
	if vm.ShutdownF != nil {
		return vm.ShutdownF()
	} else if vm.CantShutdown {
		if vm.T != nil {
			vm.T.Fatalf("Unexpectedly called Shutdown")
		}
		return errors.New("unexpectedly called Shutdown")
	}
	return nil
}

// CreateHandlers ...
func (vm *TestVM) CreateHandlers() map[string]*HTTPHandler {
	if vm.CreateHandlersF != nil {
		return vm.CreateHandlersF()
	}
	if vm.CantCreateHandlers && vm.T != nil {
		vm.T.Fatalf("Unexpectedly called CreateHandlers")
	}
	return nil
}

// CreateStaticHandlers ...
func (vm *TestVM) CreateStaticHandlers() map[string]*HTTPHandler {
	if vm.CreateStaticHandlersF != nil {
		return vm.CreateStaticHandlersF()
	}
	if vm.CantCreateStaticHandlers && vm.T != nil {
		vm.T.Fatalf("Unexpectedly called CreateStaticHandlers")
	}
	return nil
}
