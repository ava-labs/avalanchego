// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package enginetest

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/version"
)

var (
	errInitialize       = errors.New("unexpectedly called Initialize")
	errSetState         = errors.New("unexpectedly called SetState")
	errShutdown         = errors.New("unexpectedly called Shutdown")
	errCreateHandlers   = errors.New("unexpectedly called CreateHandlers")
	errNewHTTPHandler   = errors.New("unexpectedly called NewHTTPHandler")
	errHealthCheck      = errors.New("unexpectedly called HealthCheck")
	errConnected        = errors.New("unexpectedly called Connected")
	errDisconnected     = errors.New("unexpectedly called Disconnected")
	errVersion          = errors.New("unexpectedly called Version")
	errAppRequest       = errors.New("unexpectedly called AppRequest")
	errAppResponse      = errors.New("unexpectedly called AppResponse")
	errAppRequestFailed = errors.New("unexpectedly called AppRequestFailed")
	errAppGossip        = errors.New("unexpectedly called AppGossip")

	_ common.VM = (*VM)(nil)
)

// VM is a test vm
type VM struct {
	T *testing.T

	CantInitialize, CantSetState,
	CantShutdown, CantCreateHandlers, CantNewHTTPHandler,
	CantHealthCheck, CantConnected, CantDisconnected, CantVersion,
	CantAppRequest, CantAppResponse, CantAppGossip, CantAppRequestFailed bool

	InitializeF       func(ctx context.Context, chainCtx *snow.Context, db database.Database, genesisBytes []byte, upgradeBytes []byte, configBytes []byte, fxs []*common.Fx, appSender common.AppSender) error
	SetStateF         func(ctx context.Context, state snow.State) error
	ShutdownF         func(context.Context) error
	CreateHandlersF   func(context.Context) (map[string]http.Handler, error)
	NewHTTPHandlerF   func(context.Context) (http.Handler, error)
	ConnectedF        func(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error
	DisconnectedF     func(ctx context.Context, nodeID ids.NodeID) error
	HealthCheckF      func(context.Context) (interface{}, error)
	AppRequestF       func(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, msg []byte) error
	AppResponseF      func(ctx context.Context, nodeID ids.NodeID, requestID uint32, msg []byte) error
	AppGossipF        func(ctx context.Context, nodeID ids.NodeID, msg []byte) error
	AppRequestFailedF func(ctx context.Context, nodeID ids.NodeID, requestID uint32, appErr *common.AppError) error
	VersionF          func(context.Context) (string, error)
	WaitForEventF     common.Subscription
}

func (vm *VM) WaitForEvent(ctx context.Context) (common.Message, error) {
	if vm.WaitForEventF != nil {
		return vm.WaitForEventF(ctx)
	}
	<-ctx.Done()
	return 0, ctx.Err()
}

func (vm *VM) Default(cant bool) {
	vm.CantInitialize = cant
	vm.CantSetState = cant
	vm.CantShutdown = cant
	vm.CantCreateHandlers = cant
	vm.CantNewHTTPHandler = cant
	vm.CantHealthCheck = cant
	vm.CantAppRequest = cant
	vm.CantAppRequestFailed = cant
	vm.CantAppResponse = cant
	vm.CantAppGossip = cant
	vm.CantVersion = cant
	vm.CantConnected = cant
	vm.CantDisconnected = cant
}

func (vm *VM) Initialize(
	ctx context.Context,
	chainCtx *snow.Context,
	db database.Database,
	genesisBytes,
	upgradeBytes,
	configBytes []byte,
	fxs []*common.Fx,
	appSender common.AppSender,
) error {
	if vm.InitializeF != nil {
		return vm.InitializeF(
			ctx,
			chainCtx,
			db,
			genesisBytes,
			upgradeBytes,
			configBytes,
			fxs,
			appSender,
		)
	}
	if vm.CantInitialize && vm.T != nil {
		require.FailNow(vm.T, errInitialize.Error())
	}
	return errInitialize
}

func (vm *VM) SetState(ctx context.Context, state snow.State) error {
	if vm.SetStateF != nil {
		return vm.SetStateF(ctx, state)
	}
	if vm.CantSetState {
		if vm.T != nil {
			require.FailNow(vm.T, errSetState.Error())
		}
		return errSetState
	}
	return nil
}

func (vm *VM) Shutdown(ctx context.Context) error {
	if vm.ShutdownF != nil {
		return vm.ShutdownF(ctx)
	}
	if vm.CantShutdown {
		if vm.T != nil {
			require.FailNow(vm.T, errShutdown.Error())
		}
		return errShutdown
	}
	return nil
}

func (vm *VM) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	if vm.CreateHandlersF != nil {
		return vm.CreateHandlers(ctx)
	}
	if vm.CantCreateHandlers && vm.T != nil {
		require.FailNow(vm.T, errCreateHandlers.Error())
	}
	return nil, nil
}

func (vm *VM) NewHTTPHandler(ctx context.Context) (http.Handler, error) {
	if vm.NewHTTPHandlerF != nil {
		return vm.NewHTTPHandlerF(ctx)
	}
	if vm.CantNewHTTPHandler && vm.T != nil {
		require.FailNow(vm.T, errNewHTTPHandler.Error())
	}
	return nil, nil
}

func (vm *VM) HealthCheck(ctx context.Context) (interface{}, error) {
	if vm.HealthCheckF != nil {
		return vm.HealthCheckF(ctx)
	}
	if vm.CantHealthCheck && vm.T != nil {
		require.FailNow(vm.T, errHealthCheck.Error())
	}
	return nil, errHealthCheck
}

func (vm *VM) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	if vm.AppRequestF != nil {
		return vm.AppRequestF(ctx, nodeID, requestID, deadline, request)
	}
	if !vm.CantAppRequest {
		return nil
	}
	if vm.T != nil {
		require.FailNow(vm.T, errAppRequest.Error())
	}
	return errAppRequest
}

func (vm *VM) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32, appErr *common.AppError) error {
	if vm.AppRequestFailedF != nil {
		return vm.AppRequestFailedF(ctx, nodeID, requestID, appErr)
	}
	if !vm.CantAppRequestFailed {
		return nil
	}
	if vm.T != nil {
		require.FailNow(vm.T, errAppRequestFailed.Error())
	}
	return errAppRequestFailed
}

func (vm *VM) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	if vm.AppResponseF != nil {
		return vm.AppResponseF(ctx, nodeID, requestID, response)
	}
	if !vm.CantAppResponse {
		return nil
	}
	if vm.T != nil {
		require.FailNow(vm.T, errAppResponse.Error())
	}
	return errAppResponse
}

func (vm *VM) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	if vm.AppGossipF != nil {
		return vm.AppGossipF(ctx, nodeID, msg)
	}
	if !vm.CantAppGossip {
		return nil
	}
	if vm.T != nil {
		require.FailNow(vm.T, errAppGossip.Error())
	}
	return errAppGossip
}

func (vm *VM) Connected(ctx context.Context, id ids.NodeID, nodeVersion *version.Application) error {
	if vm.ConnectedF != nil {
		return vm.ConnectedF(ctx, id, nodeVersion)
	}
	if vm.CantConnected && vm.T != nil {
		require.FailNow(vm.T, errConnected.Error())
	}
	return nil
}

func (vm *VM) Disconnected(ctx context.Context, id ids.NodeID) error {
	if vm.DisconnectedF != nil {
		return vm.DisconnectedF(ctx, id)
	}
	if vm.CantDisconnected && vm.T != nil {
		require.FailNow(vm.T, errDisconnected.Error())
	}
	return nil
}

func (vm *VM) Version(ctx context.Context) (string, error) {
	if vm.VersionF != nil {
		return vm.VersionF(ctx)
	}
	if vm.CantVersion && vm.T != nil {
		require.FailNow(vm.T, errVersion.Error())
	}
	return "", nil
}
