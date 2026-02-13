// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package registry

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/registry/registrymock"
	"github.com/ava-labs/avalanchego/vms/vmsmock"
)

var (
	id1 = ids.GenerateTestID()
	id2 = ids.GenerateTestID()
	id3 = ids.GenerateTestID()
	id4 = ids.GenerateTestID()
)

// Tests the happy case where Reload succeeds.
func TestReload_Success(t *testing.T) {
	require := require.New(t)

	resources := initVMRegistryTest(t)

	factory1 := vmsmock.NewFactory(resources.ctrl)
	factory2 := vmsmock.NewFactory(resources.ctrl)
	factory3 := vmsmock.NewFactory(resources.ctrl)
	factory4 := vmsmock.NewFactory(resources.ctrl)

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
	resources.mockVMManager.EXPECT().
		RegisterFactory(gomock.Any(), id3, factory3).
		Times(1).
		Return(nil)
	resources.mockVMManager.EXPECT().
		RegisterFactory(gomock.Any(), id4, factory4).
		Times(1).
		Return(nil)

	installedVMs, failedVMs, err := resources.vmRegistry.Reload(t.Context())
	require.NoError(err)
	require.ElementsMatch([]ids.ID{id3, id4}, installedVMs)
	require.Empty(failedVMs)
}

// Tests that we fail if we're not able to get the vms on disk
func TestReload_GetNewVMsFails(t *testing.T) {
	require := require.New(t)

	resources := initVMRegistryTest(t)

	resources.mockVMGetter.EXPECT().Get().Times(1).Return(nil, nil, errTest)

	installedVMs, failedVMs, err := resources.vmRegistry.Reload(t.Context())
	require.ErrorIs(err, errTest)
	require.Empty(installedVMs)
	require.Empty(failedVMs)
}

// Tests that if we fail to register a VM, we fail.
func TestReload_PartialRegisterFailure(t *testing.T) {
	require := require.New(t)

	resources := initVMRegistryTest(t)

	factory1 := vmsmock.NewFactory(resources.ctrl)
	factory2 := vmsmock.NewFactory(resources.ctrl)
	factory3 := vmsmock.NewFactory(resources.ctrl)
	factory4 := vmsmock.NewFactory(resources.ctrl)

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
	resources.mockVMManager.EXPECT().
		RegisterFactory(gomock.Any(), id3, factory3).
		Times(1).
		Return(errTest)
	resources.mockVMManager.EXPECT().
		RegisterFactory(gomock.Any(), id4, factory4).
		Times(1).
		Return(nil)

	installedVMs, failedVMs, err := resources.vmRegistry.Reload(t.Context())
	require.NoError(err)
	require.Len(failedVMs, 1)
	require.ErrorIs(failedVMs[id3], errTest)
	require.Len(installedVMs, 1)
	require.Equal(id4, installedVMs[0])
}

type registryTestResources struct {
	ctrl          *gomock.Controller
	mockVMGetter  *registrymock.VMGetter
	mockVMManager *vmsmock.Manager
	vmRegistry    VMRegistry
}

func initVMRegistryTest(t *testing.T) *registryTestResources {
	ctrl := gomock.NewController(t)

	mockVMGetter := registrymock.NewVMGetter(ctrl)
	mockVMManager := vmsmock.NewManager(ctrl)

	vmRegistry := NewVMRegistry(
		VMRegistryConfig{
			VMGetter:  mockVMGetter,
			VMManager: mockVMManager,
		},
	)

	return &registryTestResources{
		ctrl:          ctrl,
		mockVMGetter:  mockVMGetter,
		mockVMManager: mockVMManager,
		vmRegistry:    vmRegistry,
	}
}
