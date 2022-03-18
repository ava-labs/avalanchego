// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package registry

import "github.com/ava-labs/avalanchego/ids"

var _ VMRegistry = &vmRegistry{}

// VMRegistry defines functionality to get any new virtual machines on the node,
// and install them if they're not already installed.
type VMRegistry interface {
	// Reload installs all non-installed vms on the node.
	Reload() ([]ids.ID, map[ids.ID]error, error)
	// ReloadWithReadLock installs all non-installed vms on the node assuming
	// the http read lock is currently held.
	ReloadWithReadLock() ([]ids.ID, map[ids.ID]error, error)
}

// VMRegistryConfig defines configurations for VMRegistry
type VMRegistryConfig struct {
	VMGetter     VMGetter
	VMRegisterer VMRegisterer
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

func (r *vmRegistry) Reload() ([]ids.ID, map[ids.ID]error, error) {
	return r.reload(r.config.VMRegisterer)
}

func (r *vmRegistry) ReloadWithReadLock() ([]ids.ID, map[ids.ID]error, error) {
	return r.reload(readRegisterer{
		registerer: r.config.VMRegisterer,
	})
}

func (r *vmRegistry) reload(registerer registerer) ([]ids.ID, map[ids.ID]error, error) {
	_, unregisteredVMs, err := r.config.VMGetter.Get()
	if err != nil {
		return nil, nil, err
	}

	registeredVms := make([]ids.ID, 0, len(unregisteredVMs))
	failedVMs := make(map[ids.ID]error)

	for vmID, factory := range unregisteredVMs {
		if err := registerer.Register(vmID, factory); err != nil {
			failedVMs[vmID] = err
			continue
		}

		registeredVms = append(registeredVms, vmID)
	}
	return registeredVms, failedVMs, nil
}
