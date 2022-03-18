// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package registry

import (
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms"
)

var (
	id1 = ids.GenerateTestID()
	id2 = ids.GenerateTestID()
	id3 = ids.GenerateTestID()
	id4 = ids.GenerateTestID()
)

// Tests the happy case where Reload succeeds.
func TestReload_Success(t *testing.T) {
	resources := initVMRegistryTest(t)
	defer resources.ctrl.Finish()

	factory1 := vms.NewMockFactory(resources.ctrl)
	factory2 := vms.NewMockFactory(resources.ctrl)
	factory3 := vms.NewMockFactory(resources.ctrl)
	factory4 := vms.NewMockFactory(resources.ctrl)

	registeredVms := map[ids.ID]vms.Factory{
		id1: factory1,
		id2: factory2,
	}

	unregisteredVms := map[ids.ID]vms.Factory{
		id3: factory3,
		id4: factory4,
	}

	resources.mockVMGetter.EXPECT().
		Get().
		Times(1).
		Return(registeredVms, unregisteredVms, nil)
	resources.mockVMRegisterer.EXPECT().
		Register(id3, factory3).
		Times(1).
		Return(nil)
	resources.mockVMRegisterer.EXPECT().
		Register(id4, factory4).
		Times(1).
		Return(nil)

	installedVMs, failedVMs, err := resources.vmRegistry.Reload()
	assert.ElementsMatch(t, []ids.ID{id3, id4}, installedVMs)
	assert.Empty(t, failedVMs)
	assert.Nil(t, err)
}

// Tests that we fail if we're not able to get the vms on disk
func TestReload_GetNewVMsFails(t *testing.T) {
	resources := initVMRegistryTest(t)
	defer resources.ctrl.Finish()

	resources.mockVMGetter.EXPECT().Get().Times(1).Return(nil, nil, errOops)

	installedVMs, failedVMs, err := resources.vmRegistry.Reload()
	assert.Nil(t, installedVMs)
	assert.Empty(t, failedVMs)
	assert.Equal(t, err, errOops)
}

// Tests that if we fail to register a VM, we fail.
func TestReload_PartialRegisterFailure(t *testing.T) {
	resources := initVMRegistryTest(t)
	defer resources.ctrl.Finish()

	factory1 := vms.NewMockFactory(resources.ctrl)
	factory2 := vms.NewMockFactory(resources.ctrl)
	factory3 := vms.NewMockFactory(resources.ctrl)
	factory4 := vms.NewMockFactory(resources.ctrl)

	registeredVms := map[ids.ID]vms.Factory{
		id1: factory1,
		id2: factory2,
	}

	unregisteredVms := map[ids.ID]vms.Factory{
		id3: factory3,
		id4: factory4,
	}

	resources.mockVMGetter.EXPECT().
		Get().
		Times(1).
		Return(registeredVms, unregisteredVms, nil)
	resources.mockVMRegisterer.EXPECT().
		Register(id3, factory3).
		Times(1).
		Return(errOops)
	resources.mockVMRegisterer.EXPECT().
		Register(id4, factory4).
		Times(1).
		Return(nil)

	installedVMs, failedVMs, err := resources.vmRegistry.Reload()

	assert.Len(t, failedVMs, 1)
	assert.Equal(t, failedVMs[id3], errOops)
	assert.Len(t, installedVMs, 1)
	assert.Equal(t, installedVMs[0], id4)
	assert.Nil(t, err)
}

// Tests the happy case where Reload succeeds.
func TestReloadWithReadLock_Success(t *testing.T) {
	resources := initVMRegistryTest(t)
	defer resources.ctrl.Finish()

	factory1 := vms.NewMockFactory(resources.ctrl)
	factory2 := vms.NewMockFactory(resources.ctrl)
	factory3 := vms.NewMockFactory(resources.ctrl)
	factory4 := vms.NewMockFactory(resources.ctrl)

	registeredVms := map[ids.ID]vms.Factory{
		id1: factory1,
		id2: factory2,
	}

	unregisteredVms := map[ids.ID]vms.Factory{
		id3: factory3,
		id4: factory4,
	}

	resources.mockVMGetter.EXPECT().
		Get().
		Times(1).
		Return(registeredVms, unregisteredVms, nil)
	resources.mockVMRegisterer.EXPECT().
		RegisterWithReadLock(id3, factory3).
		Times(1).
		Return(nil)
	resources.mockVMRegisterer.EXPECT().
		RegisterWithReadLock(id4, factory4).
		Times(1).
		Return(nil)

	installedVMs, failedVMs, err := resources.vmRegistry.ReloadWithReadLock()
	assert.ElementsMatch(t, []ids.ID{id3, id4}, installedVMs)
	assert.Empty(t, failedVMs)
	assert.Nil(t, err)
}

// Tests that we fail if we're not able to get the vms on disk
func TestReloadWithReadLock_GetNewVMsFails(t *testing.T) {
	resources := initVMRegistryTest(t)
	defer resources.ctrl.Finish()

	resources.mockVMGetter.EXPECT().Get().Times(1).Return(nil, nil, errOops)

	installedVMs, failedVMs, err := resources.vmRegistry.ReloadWithReadLock()
	assert.Nil(t, installedVMs)
	assert.Empty(t, failedVMs)
	assert.Equal(t, err, errOops)
}

// Tests that if we fail to register a VM, we fail.
func TestReloadWithReadLock_PartialRegisterFailure(t *testing.T) {
	resources := initVMRegistryTest(t)
	defer resources.ctrl.Finish()

	factory1 := vms.NewMockFactory(resources.ctrl)
	factory2 := vms.NewMockFactory(resources.ctrl)
	factory3 := vms.NewMockFactory(resources.ctrl)
	factory4 := vms.NewMockFactory(resources.ctrl)

	registeredVms := map[ids.ID]vms.Factory{
		id1: factory1,
		id2: factory2,
	}

	unregisteredVms := map[ids.ID]vms.Factory{
		id3: factory3,
		id4: factory4,
	}

	resources.mockVMGetter.EXPECT().
		Get().
		Times(1).
		Return(registeredVms, unregisteredVms, nil)
	resources.mockVMRegisterer.EXPECT().
		RegisterWithReadLock(id3, factory3).
		Times(1).
		Return(errOops)
	resources.mockVMRegisterer.EXPECT().
		RegisterWithReadLock(id4, factory4).
		Times(1).
		Return(nil)

	installedVMs, failedVMs, err := resources.vmRegistry.ReloadWithReadLock()

	assert.Len(t, failedVMs, 1)
	assert.Equal(t, failedVMs[id3], errOops)
	assert.Len(t, installedVMs, 1)
	assert.Equal(t, installedVMs[0], id4)
	assert.Nil(t, err)
}

type registryTestResources struct {
	ctrl             *gomock.Controller
	mockVMGetter     *MockVMGetter
	mockVMRegisterer *MockVMRegisterer
	vmRegistry       VMRegistry
}

func initVMRegistryTest(t *testing.T) *registryTestResources {
	ctrl := gomock.NewController(t)

	mockVMGetter := NewMockVMGetter(ctrl)
	mockVMRegisterer := NewMockVMRegisterer(ctrl)

	vmRegistry := NewVMRegistry(
		VMRegistryConfig{
			VMGetter:     mockVMGetter,
			VMRegisterer: mockVMRegisterer,
		},
	)

	return &registryTestResources{
		ctrl:             ctrl,
		mockVMGetter:     mockVMGetter,
		mockVMRegisterer: mockVMRegisterer,
		vmRegistry:       vmRegistry,
	}
}
