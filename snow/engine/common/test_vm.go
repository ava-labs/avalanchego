// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"errors"
	"testing"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/snow"
)

var (
	errInitialize = errors.New("unexpectedly called Initialize")
)

// VMTest is a test vm
type VMTest struct {
	T *testing.T

	CantInitialize, CantShutdown, CantCreateHandlers, CantCreateStaticHandlers bool

	InitializeF           func(*snow.Context, database.Database, []byte, chan<- Message, []*Fx) error
	ShutdownF             func() error
	CreateHandlersF       func() map[string]*HTTPHandler
	CreateStaticHandlersF func() map[string]*HTTPHandler
}

// Default ...
func (vm *VMTest) Default(cant bool) {
	vm.CantInitialize = cant
	vm.CantShutdown = cant
	vm.CantCreateHandlers = cant
}

// Initialize ...
func (vm *VMTest) Initialize(ctx *snow.Context, db database.Database, initState []byte, msgChan chan<- Message, fxs []*Fx) error {
	if vm.InitializeF != nil {
		return vm.InitializeF(ctx, db, initState, msgChan, fxs)
	}
	if vm.CantInitialize && vm.T != nil {
		vm.T.Fatal(errInitialize)
	}
	return errInitialize
}

// Shutdown ...
func (vm *VMTest) Shutdown() error {
	if vm.ShutdownF != nil {
		return vm.ShutdownF()
	} else if vm.CantShutdown {
		if vm.T != nil {
			vm.T.Fatalf("Unexpectedly called Shutdown")
		}
		return errors.New("Unexpectedly called Shutdown")
	}
	return nil
}

// CreateHandlers ...
func (vm *VMTest) CreateHandlers() map[string]*HTTPHandler {
	if vm.CreateHandlersF != nil {
		return vm.CreateHandlersF()
	}
	if vm.CantCreateHandlers && vm.T != nil {
		vm.T.Fatalf("Unexpectedly called CreateHandlers")
	}
	return nil
}

// CreateStaticHandlers ...
func (vm *VMTest) CreateStaticHandlers() map[string]*HTTPHandler {
	if vm.CreateStaticHandlersF != nil {
		return vm.CreateStaticHandlersF()
	}
	if vm.CantCreateStaticHandlers && vm.T != nil {
		vm.T.Fatalf("Unexpectedly called CreateStaticHandlers")
	}
	return nil
}
