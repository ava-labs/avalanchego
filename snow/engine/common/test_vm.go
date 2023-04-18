// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/version"

	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/snow"
)

var (
	errInitialize                 = errors.New("unexpectedly called Initialize")
	errSetState                   = errors.New("unexpectedly called SetState")
	errShutdown                   = errors.New("unexpectedly called Shutdown")
	errCreateHandlers             = errors.New("unexpectedly called CreateHandlers")
	errCreateStaticHandlers       = errors.New("unexpectedly called CreateStaticHandlers")
	errHealthCheck                = errors.New("unexpectedly called HealthCheck")
	errConnected                  = errors.New("unexpectedly called Connected")
	errDisconnected               = errors.New("unexpectedly called Disconnected")
	errVersion                    = errors.New("unexpectedly called Version")
	errAppRequest                 = errors.New("unexpectedly called AppRequest")
	errAppResponse                = errors.New("unexpectedly called AppResponse")
	errAppRequestFailed           = errors.New("unexpectedly called AppRequestFailed")
	errAppGossip                  = errors.New("unexpectedly called AppGossip")
	errCrossChainAppRequest       = errors.New("unexpectedly called CrossChainAppRequest")
	errCrossChainAppResponse      = errors.New("unexpectedly called CrossChainAppResponse")
	errCrossChainAppRequestFailed = errors.New("unexpectedly called CrossChainAppRequestFailed")

	_ VM = (*TestVM)(nil)
)

// TestVM is a test vm
type TestVM struct {
	T *testing.T

	CantInitialize, CantSetState,
	CantShutdown, CantCreateHandlers, CantCreateStaticHandlers,
	CantHealthCheck, CantConnected, CantDisconnected, CantVersion,
	CantAppRequest, CantAppResponse, CantAppGossip, CantAppRequestFailed,
	CantCrossChainAppRequest, CantCrossChainAppResponse, CantCrossChainAppRequestFailed bool

	InitializeF                 func(ctx context.Context, chainCtx *snow.Context, db manager.Manager, genesisBytes []byte, upgradeBytes []byte, configBytes []byte, msgChan chan<- Message, fxs []*Fx, appSender AppSender) error
	SetStateF                   func(ctx context.Context, state snow.State) error
	ShutdownF                   func(context.Context) error
	CreateHandlersF             func(context.Context) (map[string]*HTTPHandler, error)
	CreateStaticHandlersF       func(context.Context) (map[string]*HTTPHandler, error)
	ConnectedF                  func(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error
	DisconnectedF               func(ctx context.Context, nodeID ids.NodeID) error
	HealthCheckF                func(context.Context) (interface{}, error)
	AppRequestF                 func(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, msg []byte) error
	AppResponseF                func(ctx context.Context, nodeID ids.NodeID, requestID uint32, msg []byte) error
	AppGossipF                  func(ctx context.Context, nodeID ids.NodeID, msg []byte) error
	AppRequestFailedF           func(ctx context.Context, nodeID ids.NodeID, requestID uint32) error
	VersionF                    func(context.Context) (string, error)
	CrossChainAppRequestF       func(ctx context.Context, chainID ids.ID, requestID uint32, deadline time.Time, msg []byte) error
	CrossChainAppResponseF      func(ctx context.Context, chainID ids.ID, requestID uint32, msg []byte) error
	CrossChainAppRequestFailedF func(ctx context.Context, chainID ids.ID, requestID uint32) error
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
	vm.CantCrossChainAppRequest = cant
	vm.CantCrossChainAppRequestFailed = cant
	vm.CantCrossChainAppResponse = cant
}

func (vm *TestVM) Initialize(
	ctx context.Context,
	chainCtx *snow.Context,
	db manager.Manager,
	genesisBytes,
	upgradeBytes,
	configBytes []byte,
	msgChan chan<- Message,
	fxs []*Fx,
	appSender AppSender,
) error {
	if vm.InitializeF != nil {
		return vm.InitializeF(
			ctx,
			chainCtx,
			db,
			genesisBytes,
			upgradeBytes,
			configBytes,
			msgChan,
			fxs,
			appSender,
		)
	}
	if vm.CantInitialize && vm.T != nil {
		vm.T.Fatal(errInitialize)
	}
	return errInitialize
}

func (vm *TestVM) SetState(ctx context.Context, state snow.State) error {
	if vm.SetStateF != nil {
		return vm.SetStateF(ctx, state)
	}
	if vm.CantSetState {
		if vm.T != nil {
			vm.T.Fatal(errSetState)
		}
		return errSetState
	}
	return nil
}

func (vm *TestVM) Shutdown(ctx context.Context) error {
	if vm.ShutdownF != nil {
		return vm.ShutdownF(ctx)
	}
	if vm.CantShutdown {
		if vm.T != nil {
			vm.T.Fatal(errShutdown)
		}
		return errShutdown
	}
	return nil
}

func (vm *TestVM) CreateHandlers(ctx context.Context) (map[string]*HTTPHandler, error) {
	if vm.CreateHandlersF != nil {
		return vm.CreateHandlersF(ctx)
	}
	if vm.CantCreateHandlers && vm.T != nil {
		vm.T.Fatal(errCreateHandlers)
	}
	return nil, nil
}

func (vm *TestVM) CreateStaticHandlers(ctx context.Context) (map[string]*HTTPHandler, error) {
	if vm.CreateStaticHandlersF != nil {
		return vm.CreateStaticHandlersF(ctx)
	}
	if vm.CantCreateStaticHandlers && vm.T != nil {
		vm.T.Fatal(errCreateStaticHandlers)
	}
	return nil, nil
}

func (vm *TestVM) HealthCheck(ctx context.Context) (interface{}, error) {
	if vm.HealthCheckF != nil {
		return vm.HealthCheckF(ctx)
	}
	if vm.CantHealthCheck && vm.T != nil {
		vm.T.Fatal(errHealthCheck)
	}
	return nil, errHealthCheck
}

func (vm *TestVM) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	if vm.AppRequestF != nil {
		return vm.AppRequestF(ctx, nodeID, requestID, deadline, request)
	}
	if !vm.CantAppRequest {
		return nil
	}
	if vm.T != nil {
		vm.T.Fatal(errAppRequest)
	}
	return errAppRequest
}

func (vm *TestVM) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	if vm.AppRequestFailedF != nil {
		return vm.AppRequestFailedF(ctx, nodeID, requestID)
	}
	if !vm.CantAppRequestFailed {
		return nil
	}
	if vm.T != nil {
		vm.T.Fatal(errAppRequestFailed)
	}
	return errAppRequestFailed
}

func (vm *TestVM) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	if vm.AppResponseF != nil {
		return vm.AppResponseF(ctx, nodeID, requestID, response)
	}
	if !vm.CantAppResponse {
		return nil
	}
	if vm.T != nil {
		vm.T.Fatal(errAppResponse)
	}
	return errAppResponse
}

func (vm *TestVM) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	if vm.AppGossipF != nil {
		return vm.AppGossipF(ctx, nodeID, msg)
	}
	if !vm.CantAppGossip {
		return nil
	}
	if vm.T != nil {
		vm.T.Fatal(errAppGossip)
	}
	return errAppGossip
}

func (vm *TestVM) CrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, deadline time.Time, request []byte) error {
	if vm.CrossChainAppRequestF != nil {
		return vm.CrossChainAppRequestF(ctx, chainID, requestID, deadline, request)
	}
	if !vm.CantCrossChainAppRequest {
		return nil
	}
	if vm.T != nil {
		vm.T.Fatal(errCrossChainAppRequest)
	}
	return errCrossChainAppRequest
}

func (vm *TestVM) CrossChainAppRequestFailed(ctx context.Context, chainID ids.ID, requestID uint32) error {
	if vm.CrossChainAppRequestFailedF != nil {
		return vm.CrossChainAppRequestFailedF(ctx, chainID, requestID)
	}
	if !vm.CantCrossChainAppRequestFailed {
		return nil
	}
	if vm.T != nil {
		vm.T.Fatal(errCrossChainAppRequestFailed)
	}
	return errCrossChainAppRequestFailed
}

func (vm *TestVM) CrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, response []byte) error {
	if vm.CrossChainAppResponseF != nil {
		return vm.CrossChainAppResponseF(ctx, chainID, requestID, response)
	}
	if !vm.CantCrossChainAppResponse {
		return nil
	}
	if vm.T != nil {
		vm.T.Fatal(errCrossChainAppResponse)
	}
	return errCrossChainAppResponse
}

func (vm *TestVM) Connected(ctx context.Context, id ids.NodeID, nodeVersion *version.Application) error {
	if vm.ConnectedF != nil {
		return vm.ConnectedF(ctx, id, nodeVersion)
	}
	if vm.CantConnected && vm.T != nil {
		vm.T.Fatal(errConnected)
	}
	return nil
}

func (vm *TestVM) Disconnected(ctx context.Context, id ids.NodeID) error {
	if vm.DisconnectedF != nil {
		return vm.DisconnectedF(ctx, id)
	}
	if vm.CantDisconnected && vm.T != nil {
		vm.T.Fatal(errDisconnected)
	}
	return nil
}

func (vm *TestVM) Version(ctx context.Context) (string, error) {
	if vm.VersionF != nil {
		return vm.VersionF(ctx)
	}
	if vm.CantVersion && vm.T != nil {
		vm.T.Fatal(errVersion)
	}
	return "", nil
}
