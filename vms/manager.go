// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vms

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

var (
	ErrNotFound = errors.New("not found")

	_ Manager = &manager{}
)

// A Factory creates new instances of a VM
type Factory interface {
	New(*snow.Context) (interface{}, error)
}

// Manager tracks a collection of VM factories, their aliases, and their
// versions.
// It has the following functionality:
//   1) Register a VM factory. To register a VM is to associate its ID with a
//		VMFactory which, when New() is called upon it, creates a new instance of
//      that VM.
//	 2) Get a VM factory. Given the ID of a VM that has been registered, return
//      the factory that the ID is associated with.
//   3) Manage the aliases of VMs
//   3) Manage the versions of VMs
type Manager interface {
	ids.Aliaser

	// Return a factory that can create new instances of the vm whose ID is
	// [vmID]
	GetFactory(vmID ids.ID) (Factory, error)

	// Map [vmID] to [factory]. [factory] creates new instances of the vm whose
	// ID is [vmID]
	RegisterFactory(vmID ids.ID, factory Factory) error

	// ListFactories returns all the IDs that have had factories registered.
	ListFactories() ([]ids.ID, error)

	// Versions returns the primary alias of the VM mapped to the reported
	// version of the VM for all the registered VMs that reported versions.
	Versions() (map[string]string, error)
}

type manager struct {
	// Note: The string representation of a VM's ID is also considered to be an
	// alias of the VM. That is, [vmID].String() is an alias for [vmID].
	ids.Aliaser

	// Key: A VM's ID
	// Value: A factory that creates new instances of that VM
	factories map[ids.ID]Factory

	// Key: A VM's ID
	// Value: version the VM returned
	versions map[ids.ID]string
}

// NewManager returns an instance of a VM manager
func NewManager() Manager {
	return &manager{
		Aliaser:   ids.NewAliaser(),
		factories: make(map[ids.ID]Factory),
		versions:  make(map[ids.ID]string),
	}
}

func (m *manager) GetFactory(vmID ids.ID) (Factory, error) {
	if factory, ok := m.factories[vmID]; ok {
		return factory, nil
	}
	return nil, fmt.Errorf("%q was %w", vmID, ErrNotFound)
}

func (m *manager) RegisterFactory(vmID ids.ID, factory Factory) error {
	if _, exists := m.factories[vmID]; exists {
		return fmt.Errorf("%q was already registered as a vm", vmID)
	}
	if err := m.Alias(vmID, vmID.String()); err != nil {
		return err
	}

	m.factories[vmID] = factory

	vm, err := factory.New(nil)
	if err != nil {
		return err
	}

	commonVM, ok := vm.(common.VM)
	if !ok {
		return nil
	}

	version, err := commonVM.Version()
	if err != nil {
		// Drop the shutdown error to surface the original error
		_ = commonVM.Shutdown()
		return err
	}

	m.versions[vmID] = version
	return commonVM.Shutdown()
}

func (m *manager) ListFactories() ([]ids.ID, error) {
	vmIDs := make([]ids.ID, 0, len(m.factories))
	for vmID := range m.factories {
		vmIDs = append(vmIDs, vmID)
	}
	return vmIDs, nil
}

func (m *manager) Versions() (map[string]string, error) {
	versions := make(map[string]string, len(m.versions))
	for vmID, version := range m.versions {
		alias, err := m.PrimaryAlias(vmID)
		if err != nil {
			return nil, err
		}
		versions[alias] = version
	}
	return versions, nil
}
