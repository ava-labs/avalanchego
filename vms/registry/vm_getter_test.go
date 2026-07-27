// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package registry

import (
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/filesystem"
	"github.com/ava-labs/avalanchego/utils/filesystem/filesystemmock"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/vmsmock"
)

var (
	pluginDir = "plugin getter"

	// errors
	errTest = errors.New("non-nil error")

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

	// disk read fails
	resources.mockReader.EXPECT().ReadDir(pluginDir).Times(1).Return(nil, errTest)

	_, _, err := resources.getter.Get()
	require.ErrorIs(t, err, errTest)
}

// Get should fail if we see an invalid VM id
func TestGet_InvalidVMName(t *testing.T) {
	resources := initVMGetterTest(t)

	resources.mockReader.EXPECT().ReadDir(pluginDir).Times(1).Return(invalidVMs, nil)

	_, _, err := resources.getter.Get()
	require.ErrorIs(t, err, errInvalidVMID)
}

// Get should return the correct registered and unregistered VMs.
func TestGet_Success(t *testing.T) {
	require := require.New(t)

	resources := initVMGetterTest(t)

	registeredVMId := ids.GenerateTestID()
	unregisteredVMId := ids.GenerateTestID()

	// Set up the registered VM: alias + factory
	require.NoError(resources.manager.Alias(registeredVMId, registeredVMName))
	registeredVMFactory := vmsmock.NewFactory(resources.ctrl)
	registeredVMFactory.EXPECT().New(gomock.Any()).Return(nil, nil)
	require.NoError(resources.manager.RegisterFactory(t.Context(), registeredVMId, registeredVMFactory))

	// Set up the unregistered VM: alias only (no factory registered)
	require.NoError(resources.manager.Alias(unregisteredVMId, unregisteredVMName))

	resources.mockReader.EXPECT().ReadDir(pluginDir).Times(1).Return(twoValidVMs, nil)

	registeredVMs, unregisteredVMs, err := resources.getter.Get()

	// we should have one registered vm, and one unregistered vm.
	require.Len(registeredVMs, 1)
	require.NotNil(registeredVMs[registeredVMId])

	require.Len(unregisteredVMs, 1)
	require.NotNil(unregisteredVMs[unregisteredVMId])

	require.NoError(err)
}

type vmGetterTestResources struct {
	ctrl       *gomock.Controller
	mockReader *filesystemmock.Reader
	manager    *vms.Manager
	getter     VMGetter
}

func initVMGetterTest(t *testing.T) *vmGetterTestResources {
	ctrl := gomock.NewController(t)

	mockReader := filesystemmock.NewReader(ctrl)
	manager := vms.NewManager(logging.NoLog{}, ids.NewAliaser())
	mockRegistry := prometheus.NewRegistry()
	mockCPUTracker, err := resource.NewManager(
		logging.NoLog{},
		"",
		time.Hour,
		time.Hour,
		time.Hour,
		mockRegistry,
	)
	require.NoError(t, err)

	getter := NewVMGetter(
		VMGetterConfig{
			FileReader:      mockReader,
			Manager:         manager,
			PluginDirectory: pluginDir,
			CPUTracker:      mockCPUTracker,
		},
	)

	return &vmGetterTestResources{
		ctrl:       ctrl,
		mockReader: mockReader,
		manager:    manager,
		getter:     getter,
	}
}
