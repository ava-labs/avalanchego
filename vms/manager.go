// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vms

import (
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/api/server"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// A Factory creates new instances of a VM
type Factory interface {
	New(*snow.Context) (interface{}, error)
}

// Manager is a VM manager.
// It has the following functionality:
//   1) Register a VM factory. To register a VM is to associate its ID with a
//		 VMFactory which, when New() is called upon it, creates a new instance of that VM.
//	 2) Get a VM factory. Given the ID of a VM that has been
//      registered, return the factory that the ID is associated with.
//   3) Associate a VM with an alias
//   4) Get the ID of the VM by the VM's alias
//   5) Get the aliases of a VM
type Manager interface {
	// Returns a factory that can create new instances of the VM
	// with the given ID
	GetFactory(ids.ID) (Factory, error)

	// Associate an ID with the factory that creates new instances
	// of the VM with the given ID
	RegisterFactory(ids.ID, Factory) error

	// Versions returns the versions of all the VMs that have been registered
	Versions() (map[string]string, error)

	// Given an alias, return the ID of the VM associated with that alias
	Lookup(string) (ids.ID, error)

	// Return the aliases associated with a VM
	Aliases(ids.ID) []string

	// Give an alias to a VM
	Alias(ids.ID, string) error
}

// Implements Manager
type manager struct {
	// Note: The string representation of a VM's ID is also considered to be an
	// alias of the VM. That is, [VM].String() is an alias for the VM, too.
	ids.Aliaser

	// Key: A VM's ID
	// Value: A factory that creates new instances of that VM
	factories map[ids.ID]Factory

	// Key: A VM's ID
	// Value: version the VM returned
	versions map[ids.ID]string

	// The node's API server.
	// [manager] adds routes to this server to expose new API endpoints/services
	apiServer *server.Server

	log logging.Logger
}

// NewManager returns an instance of a VM manager
func NewManager(apiServer *server.Server, log logging.Logger) Manager {
	m := &manager{
		factories: make(map[ids.ID]Factory),
		versions:  make(map[ids.ID]string),
		apiServer: apiServer,
		log:       log,
	}
	m.Initialize()
	return m
}

// Return a factory that can create new instances of the vm whose
// ID is [vmID]
func (m *manager) GetFactory(vmID ids.ID) (Factory, error) {
	if factory, ok := m.factories[vmID]; ok {
		return factory, nil
	}
	return nil, fmt.Errorf("%q was not registered as a vm", vmID)
}

// Map [vmID] to [factory]. [factory] creates new instances of the vm whose
// ID is [vmID]
func (m *manager) RegisterFactory(vmID ids.ID, factory Factory) error {
	if _, exists := m.factories[vmID]; exists {
		return fmt.Errorf("%q was already registered as a vm", vmID)
	}
	if err := m.Alias(vmID, vmID.String()); err != nil {
		return err
	}

	m.factories[vmID] = factory

	// VMs can expose a static API (one that does not depend on the state of a
	// particular chain.) This adds to the node's API server the static API of
	// the VM with ID [vmID]. This allows clients to call the VM's static API
	// methods.

	m.log.Debug("adding static API for vm %q", vmID)

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
		m.log.Error("fetching version for %q errored with: %s", vmID, err)

		if err := commonVM.Shutdown(); err != nil {
			return fmt.Errorf("shutting down VM errored with: %s", err)
		}
		return nil
	}
	m.versions[vmID] = version

	handlers, err := commonVM.CreateStaticHandlers()
	if err != nil {
		m.log.Error("creating static API endpoints for %q errored with: %s", vmID, err)

		if err := commonVM.Shutdown(); err != nil {
			return fmt.Errorf("shutting down VM errored with: %s", err)
		}
		return nil
	}

	// all static endpoints go to the vm endpoint, defaulting to the vm id
	defaultEndpoint := constants.VMAliasPrefix + vmID.String()
	// use a single lock for this entire vm
	lock := new(sync.RWMutex)
	// register the static endpoints
	for extension, service := range handlers {
		m.log.Verbo("adding static API endpoint: %s%s", defaultEndpoint, extension)
		if err := m.apiServer.AddRoute(service, lock, defaultEndpoint, extension, m.log); err != nil {
			return fmt.Errorf(
				"failed to add static API endpoint %s%s: %s",
				defaultEndpoint,
				extension,
				err,
			)
		}
	}
	return nil
}

// Versions returns the primary alias of the VM mapped to the reported version
// of the VM for all the registered VMs that reported versions.
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
