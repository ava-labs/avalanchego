// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package registry

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms"
)

var _ VMRegistry = (*vmRegistry)(nil)

// VMRegistry defines functionality to get any new virtual machines on the node,
// and install them if they're not already installed.
type VMRegistry interface {
	// Reload installs all non-installed vms on the node.
	Reload(ctx context.Context) ([]ids.ID, map[ids.ID]error, error)
}

// VMRegistryConfig defines configurations for VMRegistry
type VMRegistryConfig struct {
	VMGetter  VMGetter
	VMManager vms.Manager
}

type vmRegistry struct {
	config VMRegistryConfig
}

// NewVMRegistry returns a VMRegistry
func NewVMRegistry(config VMRegistryConfig) VMRegistry {
	return &vmRegistry{
		config: config,
	}
}

func (r *vmRegistry) Reload(ctx context.Context) ([]ids.ID, map[ids.ID]error, error) {
	_, unregisteredVMs, err := r.config.VMGetter.Get()
	if err != nil {
		return nil, nil, err
	}

	registeredVms := make([]ids.ID, 0, len(unregisteredVMs))
	failedVMs := make(map[ids.ID]error)

	for vmID, factory := range unregisteredVMs {
		if err := r.config.VMManager.RegisterFactory(ctx, vmID, factory); err != nil {
			failedVMs[vmID] = err
			continue
		}

		registeredVms = append(registeredVms, vmID)
	}
	return registeredVms, failedVMs, nil
}
