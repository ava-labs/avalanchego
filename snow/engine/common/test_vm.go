// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"errors"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/version"

	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/snow"
)

var (
	errInitialize           = errors.New("unexpectedly called Initialize")
	errSetState             = errors.New("unexpectedly called SetState")
	errShutdown             = errors.New("unexpectedly called Shutdown")
	errCreateHandlers       = errors.New("unexpectedly called CreateHandlers")
	errCreateStaticHandlers = errors.New("unexpectedly called CreateStaticHandlers")
	errHealthCheck          = errors.New("unexpectedly called HealthCheck")
	errConnected            = errors.New("unexpectedly called Connected")
	errDisconnected         = errors.New("unexpectedly called Disconnected")
	errVersion              = errors.New("unexpectedly called Version")
	errAppRequest           = errors.New("unexpectedly called AppRequest")
	errAppResponse          = errors.New("unexpectedly called AppResponse")
	errAppRequestFailed     = errors.New("unexpectedly called AppRequestFailed")
	errAppGossip            = errors.New("unexpectedly called AppGossip")

	_ VM = &TestVM{}
)

// TestVM is a test vm
type TestVM struct {
	T *testing.T

	CantInitialize, CantSetState,
	CantShutdown, CantCreateHandlers, CantCreateStaticHandlers,
	CantHealthCheck, CantConnected, CantDisconnected, CantVersion,
	CantAppRequest, CantAppResponse, CantAppGossip, CantAppRequestFailed bool

	InitializeF           func(*snow.Context, manager.Manager, []byte, []byte, []byte, chan<- Message, []*Fx, AppSender) error
	SetStateF             func(snow.State) error
	ShutdownF             func() error
	CreateHandlersF       func() (map[string]*HTTPHandler, error)
	CreateStaticHandlersF func() (map[string]*HTTPHandler, error)
	ConnectedF            func(nodeID ids.ShortID, nodeVersion version.Application) error
	DisconnectedF         func(nodeID ids.ShortID) error
	HealthCheckF          func() (interface{}, error)
	AppRequestF           func(nodeID ids.ShortID, requestID uint32, deadline time.Time, msg []byte) error
	AppResponseF          func(nodeID ids.ShortID, requestID uint32, msg []byte) error
	AppGossipF            func(nodeID ids.ShortID, msg []byte) error
	AppRequestFailedF     func(nodeID ids.ShortID, requestID uint32) error
	VersionF              func() (string, error)
}

func (vm *TestVM) Default(cant bool) {
	vm.CantInitialize = cant
	vm.CantSetState = cant
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

func (vm *TestVM) SetState(state snow.State) error {
	if vm.SetStateF != nil {
		return vm.SetStateF(state)
	}
	if vm.CantSetState {
		if vm.T != nil {
			vm.T.Fatal(errSetState)
		}
		return errSetState
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
		vm.T.Fatal(errAppRequest)
	}
	return errAppRequest
}

func (vm *TestVM) AppRequest(nodeID ids.ShortID, requestID uint32, deadline time.Time, request []byte) error {
	if vm.AppRequestF != nil {
		return vm.AppRequestF(nodeID, requestID, deadline, request)
	}
	if !vm.CantAppRequest {
		return nil
	}
	if vm.T != nil {
		vm.T.Fatal(errAppRequest)
	}
	return errAppRequest
}

func (vm *TestVM) AppResponse(nodeID ids.ShortID, requestID uint32, response []byte) error {
	if vm.AppResponseF != nil {
		return vm.AppResponseF(nodeID, requestID, response)
	}
	if !vm.CantAppResponse {
		return nil
	}
	if vm.T != nil {
		vm.T.Fatal(errAppResponse)
	}
	return errAppResponse
}

func (vm *TestVM) AppGossip(nodeID ids.ShortID, msg []byte) error {
	if vm.AppGossipF != nil {
		return vm.AppGossipF(nodeID, msg)
	}
	if !vm.CantAppGossip {
		return nil
	}
	if vm.T != nil {
		vm.T.Fatal(errAppGossip)
	}
	return errAppGossip
}

func (vm *TestVM) Connected(id ids.ShortID, nodeVersion version.Application) error {
	if vm.ConnectedF != nil {
		return vm.ConnectedF(id, nodeVersion)
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
