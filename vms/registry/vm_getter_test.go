// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package registry

import (
	"errors"
	"io/fs"
	"testing"

	"gotest.tools/assert"

	"github.com/golang/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/filesystem"
	"github.com/ava-labs/avalanchego/vms"
)

var (
	pluginDir = "plugin getter"

	// errors
	errOops = errors.New("oops")

	// vm names
	registeredVMName   = "mgj786NP7uDwBCcq6YwThhaN8FLyybkCa4zBWTQbNgmK6k9A6"
	unregisteredVMName = "tGas3T58KzdjLHhBDMnH2TvrddhqTji5iZAMZ3RXs2NLpSnhH"

	// files
	directory = filesystem.MockFile{
		MockName:  "directory-1",
		MockIsDir: true,
	}
	registeredVM = filesystem.MockFile{
		MockName: registeredVMName + ".abc",
	}
	unregisteredVM = filesystem.MockFile{
		MockName: unregisteredVMName + ".xyz",
	}
	invalidVM = filesystem.MockFile{
		MockName: "invalid-vm.file",
	}

	// read dir results
	oneValidVM = []fs.DirEntry{
		directory,
		registeredVM,
	}
	twoValidVMs = []fs.DirEntry{
		directory,
		registeredVM,
		unregisteredVM,
	}
	invalidVMs = []fs.DirEntry{
		directory,
		invalidVM,
	}
)

// Get should fail if we hit an io issue when reading files on the disk
func TestGet_ReadDirFails(t *testing.T) {
	resources := initVMGetterTest(t)
	defer resources.ctrl.Finish()

	// disk read fails
	resources.mockReader.EXPECT().ReadDir(pluginDir).Times(1).Return(nil, errOops)

	_, _, err := resources.getter.Get()
	assert.Equal(t, errOops, err)
}

// Get should fail if we see an invalid VM id
func TestGet_InvalidVMName(t *testing.T) {
	resources := initVMGetterTest(t)
	defer resources.ctrl.Finish()

	resources.mockReader.EXPECT().ReadDir(pluginDir).Times(1).Return(invalidVMs, nil)
	// didn't find an alias, so we'll try using this invalid vm name
	resources.mockManager.EXPECT().Lookup("invalid-vm").Times(1).Return(ids.Empty, errOops)

	_, _, err := resources.getter.Get()
	assert.ErrorContains(t, err, "invalid vmID invalid-vm")
}

// Get should fail if we can't get the VM factory
func TestGet_GetFactoryFails(t *testing.T) {
	resources := initVMGetterTest(t)
	defer resources.ctrl.Finish()

	vm, _ := ids.FromString("vmId")

	resources.mockReader.EXPECT().ReadDir(pluginDir).Times(1).Return(oneValidVM, nil)
	resources.mockManager.EXPECT().Lookup(registeredVMName).Times(1).Return(vm, nil)
	// Getting the factory fails
	resources.mockManager.EXPECT().GetFactory(vm).Times(1).Return(nil, errOops)

	_, _, err := resources.getter.Get()
	assert.Equal(t, errOops, err)
}

// Get should return the correct registered and unregistered VMs.
func TestGet_Success(t *testing.T) {
	resources := initVMGetterTest(t)
	defer resources.ctrl.Finish()

	registeredVMId := ids.GenerateTestID()
	unregisteredVMId := ids.GenerateTestID()

	registeredVMFactory := vms.NewMockFactory(resources.ctrl)

	resources.mockReader.EXPECT().ReadDir(pluginDir).Times(1).Return(twoValidVMs, nil)
	resources.mockManager.EXPECT().Lookup(registeredVMName).Times(1).Return(registeredVMId, nil)
	resources.mockManager.EXPECT().GetFactory(registeredVMId).Times(1).Return(registeredVMFactory, nil)
	resources.mockManager.EXPECT().Lookup(unregisteredVMName).Times(1).Return(unregisteredVMId, nil)
	resources.mockManager.EXPECT().GetFactory(unregisteredVMId).Times(1).Return(nil, vms.ErrNotFound)

	registeredVMs, unregisteredVMs, err := resources.getter.Get()

	// we should have one registered vm, and one unregistered vm.
	assert.Equal(t, len(registeredVMs), 1)
	assert.Check(t, registeredVMs[registeredVMId] != nil)

	assert.Equal(t, len(unregisteredVMs), 1)
	assert.Check(t, unregisteredVMs[unregisteredVMId] != nil)

	assert.NilError(t, err)
}

type vmGetterTestResources struct {
	ctrl        *gomock.Controller
	mockReader  *filesystem.MockReader
	mockManager *vms.MockManager
	getter      VMGetter
}

func initVMGetterTest(t *testing.T) *vmGetterTestResources {
	ctrl := gomock.NewController(t)

	mockReader := filesystem.NewMockReader(ctrl)
	mockManager := vms.NewMockManager(ctrl)

	getter := NewVMGetter(
		VMGetterConfig{
			FileReader:      mockReader,
			Manager:         mockManager,
			PluginDirectory: pluginDir,
		},
	)

	return &vmGetterTestResources{
		ctrl:        ctrl,
		mockReader:  mockReader,
		mockManager: mockManager,
		getter:      getter,
	}
}
