// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package registry

import (
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/api/server"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/mocks"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms"
)

var id = ids.GenerateTestID()

// Register should succeed even if we can't register a VM
func TestRegisterRegisterVMFails(t *testing.T) {
	resources := initRegistererTest(t)
	defer resources.ctrl.Finish()

	vmFactory := vms.NewMockFactory(resources.ctrl)

	// We fail to register the VM
	resources.mockManager.EXPECT().RegisterFactory(id, vmFactory).Times(1).Return(errOops)

	assert.Error(t, errOops, resources.registerer.Register(id, vmFactory))
}

// Tests Register if a VM doesn't actually implement VM.
func TestRegisterBadVM(t *testing.T) {
	resources := initRegistererTest(t)
	defer resources.ctrl.Finish()

	vmFactory := vms.NewMockFactory(resources.ctrl)
	vm := "this is not a vm..."

	resources.mockManager.EXPECT().RegisterFactory(id, vmFactory).Times(1).Return(nil)
	// Since this factory produces a bad vm, we should get an error.
	vmFactory.EXPECT().New(nil).Times(1).Return(vm, nil)

	assert.Error(t, errOops, resources.registerer.Register(id, vmFactory))
}

// Tests Register if creating endpoints for a VM fails + shutdown fails
func TestRegisterCreateHandlersAndShutdownFails(t *testing.T) {
	resources := initRegistererTest(t)
	defer resources.ctrl.Finish()

	vmFactory := vms.NewMockFactory(resources.ctrl)
	vm := &mocks.ChainVM{}

	resources.mockManager.EXPECT().RegisterFactory(id, vmFactory).Times(1).Return(nil)
	vmFactory.EXPECT().New(nil).Times(1).Return(vm, nil)
	// We fail to create handlers + fail to shutdown
	vm.On("CreateStaticHandlers").Once().Return(nil, errOops)
	vm.On("Shutdown").Once().Return(errOops)

	assert.Error(t, errOops, resources.registerer.Register(id, vmFactory))
}

// Tests Register if creating endpoints for a VM fails + shutdown succeeds
func TestRegisterCreateHandlersFails(t *testing.T) {
	resources := initRegistererTest(t)
	defer resources.ctrl.Finish()

	vmFactory := vms.NewMockFactory(resources.ctrl)
	vm := &mocks.ChainVM{}

	resources.mockManager.EXPECT().RegisterFactory(id, vmFactory).Times(1).Return(nil)
	vmFactory.EXPECT().New(nil).Times(1).Return(vm, nil)
	// We fail to create handlers + but succeed our shutdown
	vm.On("CreateStaticHandlers").Once().Return(nil, errOops)
	vm.On("Shutdown").Once().Return(nil)

	assert.Error(t, errOops, resources.registerer.Register(id, vmFactory))
}

// Tests Register if we fail to regsiter the new endpoint on the server.
func TestRegisterAddRouteFails(t *testing.T) {
	resources := initRegistererTest(t)
	defer resources.ctrl.Finish()

	vmFactory := vms.NewMockFactory(resources.ctrl)
	vm := &mocks.ChainVM{}

	handlers := map[string]*common.HTTPHandler{
		"foo": {},
	}

	resources.mockManager.EXPECT().RegisterFactory(id, vmFactory).Times(1).Return(nil)
	vmFactory.EXPECT().New(nil).Times(1).Return(vm, nil)
	vm.On("CreateStaticHandlers").Once().Return(handlers, nil)
	// We fail to create an endpoint for the handler
	resources.mockServer.EXPECT().
		AddRoute(handlers["foo"], gomock.Any(), constants.VMAliasPrefix+id.String(), "foo").
		Times(1).
		Return(errOops)

	assert.Error(t, errOops, resources.registerer.Register(id, vmFactory))
}

// Tests Register we can't find the alias for the newly registered vm
func TestRegisterAliasLookupFails(t *testing.T) {
	resources := initRegistererTest(t)
	defer resources.ctrl.Finish()

	vmFactory := vms.NewMockFactory(resources.ctrl)
	vm := &mocks.ChainVM{}

	handlers := map[string]*common.HTTPHandler{
		"foo": {},
	}

	resources.mockManager.EXPECT().RegisterFactory(id, vmFactory).Times(1).Return(nil)
	vmFactory.EXPECT().New(nil).Times(1).Return(vm, nil)
	vm.On("CreateStaticHandlers").Once().Return(handlers, nil)
	// Registering the route fails
	resources.mockServer.EXPECT().
		AddRoute(handlers["foo"], gomock.Any(), constants.VMAliasPrefix+id.String(), "foo").
		Times(1).
		Return(nil)
	resources.mockManager.EXPECT().Aliases(id).Times(1).Return(nil, errOops)

	assert.Error(t, errOops, resources.registerer.Register(id, vmFactory))
}

// Tests Register if adding aliases for the newly registered vm fails
func TestRegisterAddAliasesFails(t *testing.T) {
	resources := initRegistererTest(t)
	defer resources.ctrl.Finish()

	vmFactory := vms.NewMockFactory(resources.ctrl)
	vm := &mocks.ChainVM{}

	handlers := map[string]*common.HTTPHandler{
		"foo": {},
	}
	aliases := []string{"alias-1", "alias-2"}

	resources.mockManager.EXPECT().RegisterFactory(id, vmFactory).Times(1).Return(nil)
	vmFactory.EXPECT().New(nil).Times(1).Return(vm, nil)
	vm.On("CreateStaticHandlers").Once().Return(handlers, nil)
	resources.mockServer.EXPECT().
		AddRoute(handlers["foo"], gomock.Any(), constants.VMAliasPrefix+id.String(), "foo").
		Times(1).
		Return(nil)
	resources.mockManager.EXPECT().Aliases(id).Times(1).Return(aliases, nil)
	// Adding aliases fails
	resources.mockServer.EXPECT().
		AddAliases(constants.VMAliasPrefix+id.String(), constants.VMAliasPrefix+aliases[0], constants.VMAliasPrefix+aliases[1]).
		Return(errOops)

	assert.Error(t, errOops, resources.registerer.Register(id, vmFactory))
}

// Tests Register if no errors are thrown
func TestRegisterHappyCase(t *testing.T) {
	resources := initRegistererTest(t)
	defer resources.ctrl.Finish()

	vmFactory := vms.NewMockFactory(resources.ctrl)
	vm := &mocks.ChainVM{}

	handlers := map[string]*common.HTTPHandler{
		"foo": {},
	}
	aliases := []string{"alias-1", "alias-2"}

	resources.mockManager.EXPECT().RegisterFactory(id, vmFactory).Times(1).Return(nil)
	vmFactory.EXPECT().New(nil).Times(1).Return(vm, nil)
	vm.On("CreateStaticHandlers").Once().Return(handlers, nil)
	resources.mockServer.EXPECT().
		AddRoute(handlers["foo"], gomock.Any(), constants.VMAliasPrefix+id.String(), "foo").
		Times(1).
		Return(nil)
	resources.mockManager.EXPECT().Aliases(id).Times(1).Return(aliases, nil)
	resources.mockServer.EXPECT().
		AddAliases(constants.VMAliasPrefix+id.String(), constants.VMAliasPrefix+aliases[0], constants.VMAliasPrefix+aliases[1]).
		Times(1).
		Return(nil)

	assert.Nil(t, resources.registerer.Register(id, vmFactory))
}

// RegisterWithReadLock should succeed even if we can't register a VM
func TestRegisterWithReadLockRegisterVMFails(t *testing.T) {
	resources := initRegistererTest(t)
	defer resources.ctrl.Finish()

	vmFactory := vms.NewMockFactory(resources.ctrl)

	// We fail to register the VM
	resources.mockManager.EXPECT().RegisterFactory(id, vmFactory).Times(1).Return(errOops)

	assert.Error(t, errOops, resources.registerer.RegisterWithReadLock(id, vmFactory))
}

// Tests RegisterWithReadLock if a VM doesn't actually implement VM.
func TestRegisterWithReadLockBadVM(t *testing.T) {
	resources := initRegistererTest(t)
	defer resources.ctrl.Finish()

	vmFactory := vms.NewMockFactory(resources.ctrl)
	vm := "this is not a vm..."

	resources.mockManager.EXPECT().RegisterFactory(id, vmFactory).Times(1).Return(nil)
	// Since this factory produces a bad vm, we should get an error.
	vmFactory.EXPECT().New(nil).Times(1).Return(vm, nil)

	assert.Error(t, errOops, resources.registerer.RegisterWithReadLock(id, vmFactory))
}

// Tests RegisterWithReadLock if creating endpoints for a VM fails + shutdown fails
func TestRegisterWithReadLockCreateHandlersAndShutdownFails(t *testing.T) {
	resources := initRegistererTest(t)
	defer resources.ctrl.Finish()

	vmFactory := vms.NewMockFactory(resources.ctrl)
	vm := &mocks.ChainVM{}

	resources.mockManager.EXPECT().RegisterFactory(id, vmFactory).Times(1).Return(nil)
	vmFactory.EXPECT().New(nil).Times(1).Return(vm, nil)
	// We fail to create handlers + fail to shutdown
	vm.On("CreateStaticHandlers").Once().Return(nil, errOops)
	vm.On("Shutdown").Once().Return(errOops)

	assert.Error(t, errOops, resources.registerer.RegisterWithReadLock(id, vmFactory))
}

// Tests RegisterWithReadLock if creating endpoints for a VM fails + shutdown succeeds
func TestRegisterWithReadLockCreateHandlersFails(t *testing.T) {
	resources := initRegistererTest(t)
	defer resources.ctrl.Finish()

	vmFactory := vms.NewMockFactory(resources.ctrl)
	vm := &mocks.ChainVM{}

	resources.mockManager.EXPECT().RegisterFactory(id, vmFactory).Times(1).Return(nil)
	vmFactory.EXPECT().New(nil).Times(1).Return(vm, nil)
	// We fail to create handlers + but succeed our shutdown
	vm.On("CreateStaticHandlers").Once().Return(nil, errOops)
	vm.On("Shutdown").Once().Return(nil)

	assert.Error(t, errOops, resources.registerer.RegisterWithReadLock(id, vmFactory))
}

// Tests RegisterWithReadLock if we fail to regsiter the new endpoint on the server.
func TestRegisterWithReadLockAddRouteWithReadLockFails(t *testing.T) {
	resources := initRegistererTest(t)
	defer resources.ctrl.Finish()

	vmFactory := vms.NewMockFactory(resources.ctrl)
	vm := &mocks.ChainVM{}

	handlers := map[string]*common.HTTPHandler{
		"foo": {},
	}

	resources.mockManager.EXPECT().RegisterFactory(id, vmFactory).Times(1).Return(nil)
	vmFactory.EXPECT().New(nil).Times(1).Return(vm, nil)
	vm.On("CreateStaticHandlers").Once().Return(handlers, nil)
	// We fail to create an endpoint for the handler
	resources.mockServer.EXPECT().
		AddRouteWithReadLock(handlers["foo"], gomock.Any(), constants.VMAliasPrefix+id.String(), "foo").
		Times(1).
		Return(errOops)

	assert.Error(t, errOops, resources.registerer.RegisterWithReadLock(id, vmFactory))
}

// Tests RegisterWithReadLock we can't find the alias for the newly registered vm
func TestRegisterWithReadLockAliasLookupFails(t *testing.T) {
	resources := initRegistererTest(t)
	defer resources.ctrl.Finish()

	vmFactory := vms.NewMockFactory(resources.ctrl)
	vm := &mocks.ChainVM{}

	handlers := map[string]*common.HTTPHandler{
		"foo": {},
	}

	resources.mockManager.EXPECT().RegisterFactory(id, vmFactory).Times(1).Return(nil)
	vmFactory.EXPECT().New(nil).Times(1).Return(vm, nil)
	vm.On("CreateStaticHandlers").Once().Return(handlers, nil)
	// RegisterWithReadLocking the route fails
	resources.mockServer.EXPECT().
		AddRouteWithReadLock(handlers["foo"], gomock.Any(), constants.VMAliasPrefix+id.String(), "foo").
		Times(1).
		Return(nil)
	resources.mockManager.EXPECT().Aliases(id).Times(1).Return(nil, errOops)

	assert.Error(t, errOops, resources.registerer.RegisterWithReadLock(id, vmFactory))
}

// Tests RegisterWithReadLock if adding aliases for the newly registered vm fails
func TestRegisterWithReadLockAddAliasesFails(t *testing.T) {
	resources := initRegistererTest(t)
	defer resources.ctrl.Finish()

	vmFactory := vms.NewMockFactory(resources.ctrl)
	vm := &mocks.ChainVM{}

	handlers := map[string]*common.HTTPHandler{
		"foo": {},
	}
	aliases := []string{"alias-1", "alias-2"}

	resources.mockManager.EXPECT().RegisterFactory(id, vmFactory).Times(1).Return(nil)
	vmFactory.EXPECT().New(nil).Times(1).Return(vm, nil)
	vm.On("CreateStaticHandlers").Once().Return(handlers, nil)
	resources.mockServer.EXPECT().
		AddRouteWithReadLock(handlers["foo"], gomock.Any(), constants.VMAliasPrefix+id.String(), "foo").
		Times(1).
		Return(nil)
	resources.mockManager.EXPECT().Aliases(id).Times(1).Return(aliases, nil)
	// Adding aliases fails
	resources.mockServer.EXPECT().
		AddAliasesWithReadLock(constants.VMAliasPrefix+id.String(), constants.VMAliasPrefix+aliases[0], constants.VMAliasPrefix+aliases[1]).
		Return(errOops)

	assert.Error(t, errOops, resources.registerer.RegisterWithReadLock(id, vmFactory))
}

// Tests RegisterWithReadLock if no errors are thrown
func TestRegisterWithReadLockHappyCase(t *testing.T) {
	resources := initRegistererTest(t)
	defer resources.ctrl.Finish()

	vmFactory := vms.NewMockFactory(resources.ctrl)
	vm := &mocks.ChainVM{}

	handlers := map[string]*common.HTTPHandler{
		"foo": {},
	}
	aliases := []string{"alias-1", "alias-2"}

	resources.mockManager.EXPECT().RegisterFactory(id, vmFactory).Times(1).Return(nil)
	vmFactory.EXPECT().New(nil).Times(1).Return(vm, nil)
	vm.On("CreateStaticHandlers").Once().Return(handlers, nil)
	resources.mockServer.EXPECT().
		AddRouteWithReadLock(handlers["foo"], gomock.Any(), constants.VMAliasPrefix+id.String(), "foo").
		Times(1).
		Return(nil)
	resources.mockManager.EXPECT().Aliases(id).Times(1).Return(aliases, nil)
	resources.mockServer.EXPECT().
		AddAliasesWithReadLock(constants.VMAliasPrefix+id.String(), constants.VMAliasPrefix+aliases[0], constants.VMAliasPrefix+aliases[1]).
		Times(1).
		Return(nil)

	assert.Nil(t, resources.registerer.RegisterWithReadLock(id, vmFactory))
}

type vmRegistererTestResources struct {
	ctrl        *gomock.Controller
	mockManager *vms.MockManager
	mockServer  *server.MockServer
	mockLogger  *logging.MockLogger
	registerer  VMRegisterer
}

func initRegistererTest(t *testing.T) *vmRegistererTestResources {
	ctrl := gomock.NewController(t)

	mockManager := vms.NewMockManager(ctrl)
	mockServer := server.NewMockServer(ctrl)
	mockLog := logging.NewMockLogger(ctrl)

	registerer := NewVMRegisterer(VMRegistererConfig{
		APIServer: mockServer,
		Log:       mockLog,
		VMManager: mockManager,
	})

	mockLog.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()
	mockLog.EXPECT().Warn(gomock.Any(), gomock.Any()).AnyTimes()
	mockLog.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()
	mockLog.EXPECT().Debug(gomock.Any(), gomock.Any()).AnyTimes()
	mockLog.EXPECT().Trace(gomock.Any(), gomock.Any()).AnyTimes()
	mockLog.EXPECT().Verbo(gomock.Any(), gomock.Any()).AnyTimes()

	return &vmRegistererTestResources{
		ctrl:        ctrl,
		mockManager: mockManager,
		mockServer:  mockServer,
		mockLogger:  mockLog,
		registerer:  registerer,
	}
}
