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
	CantHealthCheck, CantConnected, CantDisconnected, CantVersion,
	CantAppRequest, CantAppResponse, CantAppGossip, CantAppRequestFailed bool

	InitializeF                              func(*snow.Context, manager.Manager, []byte, []byte, []byte, chan<- Message, []*Fx, AppSender) error
	BootstrappingF, BootstrappedF, ShutdownF func() error
	CreateHandlersF                          func() (map[string]*HTTPHandler, error)
	CreateStaticHandlersF                    func() (map[string]*HTTPHandler, error)
	ConnectedF                               func(ids.ShortID) error
	DisconnectedF                            func(ids.ShortID) error
	HealthCheckF                             func() (interface{}, error)
	AppRequestF, AppGossipF, AppResponseF    func(nodeID ids.ShortID, requestID uint32, msg []byte) error
	AppRequestFailedF                        func(nodeID ids.ShortID, requestID uint32) error
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
	vm.CantAppRequest = cant
	vm.CantAppRequestFailed = cant
	vm.CantAppResponse = cant
	vm.CantAppGossip = cant
	vm.CantVersion = cant
	vm.CantConnected = cant
	vm.CantDisconnected = cant
}

func (vm *TestVM) Initialize(ctx *snow.Context, db manager.Manager, genesisBytes, upgradeBytes, configBytes []byte, msgChan chan<- Message, fxs []*Fx, appSender AppSender) error {
	if vm.InitializeF != nil {
		return vm.InitializeF(ctx, db, genesisBytes, upgradeBytes, configBytes, msgChan, fxs, appSender)
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

func (vm *TestVM) AppRequestFailed(nodeID ids.ShortID, requestID uint32) error {
	if vm.AppRequestFailedF != nil {
		return vm.AppRequestFailedF(nodeID, requestID)
	}
	if !vm.CantAppRequestFailed {
		return nil
	}
	if vm.T != nil {
		vm.T.Fatalf("Unexpectedly called AppRequestFailed")
	}
	return errors.New("unexpectedly called AppRequestFailed")
}

func (vm *TestVM) AppRequest(nodeID ids.ShortID, requestID uint32, request []byte) error {
	if vm.AppRequestF != nil {
		return vm.AppRequestF(nodeID, requestID, request)
	}
	if !vm.CantAppRequest {
		return nil
	}
	if vm.T != nil {
		vm.T.Fatalf("Unexpectedly called AppRequest")
	}
	return errors.New("unexpectedly called AppRequest")
}

func (vm *TestVM) AppResponse(nodeID ids.ShortID, requestID uint32, response []byte) error {
	if vm.AppResponseF != nil {
		return vm.AppResponseF(nodeID, requestID, response)
	}
	if !vm.CantAppResponse {
		return nil
	}
	if vm.T != nil {
		vm.T.Fatalf("Unexpectedly called AppResponse")
	}
	return errors.New("unexpectedly called AppResponse")
}

func (vm *TestVM) AppGossip(nodeID ids.ShortID, msgID uint32, msg []byte) error {
	if vm.AppGossipF != nil {
		return vm.AppGossipF(nodeID, msgID, msg)
	}
	if !vm.CantAppGossip {
		return nil
	}
	if vm.T != nil {
		vm.T.Fatalf("Unexpectedly called AppGossip")
	}
	return errors.New("unexpectedly called AppGossip")
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
