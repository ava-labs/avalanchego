// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package registry

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/ava-labs/avalanchego/ids"
	rpcchainvm2 "github.com/ava-labs/avalanchego/node/rpcchainvm"
	"github.com/ava-labs/avalanchego/utils/filesystem"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/runtime"
)

var (
	_ VMGetter = (*vmGetter)(nil)

	errInvalidVMID = errors.New("invalid vmID")
)

// VMGetter defines functionality to get the plugins on the node.
type VMGetter interface {
	// Get fetches the VMs that are registered and the VMs that are not
	// registered but available to be installed on the node.
	Get() (
		registeredVMs map[ids.ID]vms.Factory[*rpcchainvm.VMClient],
		unregisteredVMs map[ids.ID]vms.Factory[*rpcchainvm.VMClient],
		err error,
	)
}

// VMGetterConfig defines settings for VMGetter
type VMGetterConfig struct {
	FileReader      filesystem.Reader
	Manager         rpcchainvm2.Manager
	PluginDirectory string
	CPUTracker      resource.ProcessTracker
	RuntimeTracker  runtime.Tracker
}

type vmGetter struct {
	config VMGetterConfig
}

// NewVMGetter returns a new instance of a VMGetter
func NewVMGetter(config VMGetterConfig) VMGetter {
	return &vmGetter{
		config: config,
	}
}

func (getter *vmGetter) Get() (map[ids.ID]vms.Factory[*rpcchainvm.VMClient], map[ids.ID]vms.Factory[*rpcchainvm.VMClient], error) {
	files, err := getter.config.FileReader.ReadDir(getter.config.PluginDirectory)
	if err != nil {
		return nil, nil, err
	}

	registeredVMs := make(map[ids.ID]vms.Factory[*rpcchainvm.VMClient])
	unregisteredVMs := make(map[ids.ID]vms.Factory[*rpcchainvm.VMClient])
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		nameWithExtension := file.Name()
		// Strip any extension from the file. This is to support windows .exe
		// files.
		name := nameWithExtension[:len(nameWithExtension)-len(filepath.Ext(nameWithExtension))]

		// Skip hidden files.
		if len(name) == 0 {
			continue
		}

		vmID, err := getter.config.Manager.Lookup(name)
		if err != nil {
			// there is no alias with plugin name, try to use full vmID.
			vmID, err = ids.FromString(name)
			if err != nil {
				return nil, nil, fmt.Errorf("%w: %q", errInvalidVMID, name)
			}
		}

		registeredFactory, err := getter.config.Manager.GetFactory(vmID)

		if err == nil {
			// If we already have the VM registered, we shouldn't attempt to
			// register it again.
			registeredVMs[vmID] = registeredFactory
			continue
		}

		// If the error isn't "not found", then we should report the error.
		if !errors.Is(err, rpcchainvm2.ErrNotFound) {
			return nil, nil, err
		}

		unregisteredVMs[vmID] = rpcchainvm.NewFactory(
			filepath.Join(getter.config.PluginDirectory, file.Name()),
			getter.config.CPUTracker,
			getter.config.RuntimeTracker,
		)
	}
	return registeredVMs, unregisteredVMs, nil
}
