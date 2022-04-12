// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package registry

import (
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/api/server"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms"
)

var _ VMRegisterer = &vmRegisterer{}

// VMRegisterer defines functionality to install a virtual machine.
type VMRegisterer interface {
	registerer
	// RegisterWithReadLock installs the VM assuming that the http read-lock is
	// held.
	RegisterWithReadLock(ids.ID, vms.Factory) error
}

type registerer interface {
	// Register installs the VM.
	Register(ids.ID, vms.Factory) error
}

// VMRegistererConfig configures settings for VMRegisterer.
type VMRegistererConfig struct {
	APIServer server.Server
	Log       logging.Logger
	VMManager vms.Manager
}

type vmRegisterer struct {
	config VMRegistererConfig
}

// NewVMRegisterer returns an instance of VMRegisterer
func NewVMRegisterer(config VMRegistererConfig) VMRegisterer {
	return &vmRegisterer{
		config: config,
	}
}

func (r *vmRegisterer) Register(vmID ids.ID, factory vms.Factory) error {
	return r.register(r.config.APIServer, vmID, factory)
}

func (r *vmRegisterer) RegisterWithReadLock(vmID ids.ID, factory vms.Factory) error {
	return r.register(server.PathWriterFromWithReadLock(r.config.APIServer), vmID, factory)
}

func (r *vmRegisterer) register(pathAdder server.PathAdder, vmID ids.ID, factory vms.Factory) error {
	if err := r.config.VMManager.RegisterFactory(vmID, factory); err != nil {
		return err
	}
	handlers, err := r.createStaticHandlers(vmID, factory)
	if err != nil {
		return err
	}

	// all static endpoints go to the vm endpoint, defaulting to the vm id
	defaultEndpoint := constants.VMAliasPrefix + vmID.String()

	if err := r.createStaticEndpoints(pathAdder, handlers, defaultEndpoint); err != nil {
		return err
	}
	urlAliases, err := r.getURLAliases(vmID, defaultEndpoint)
	if err != nil {
		return err
	}
	return pathAdder.AddAliases(defaultEndpoint, urlAliases...)
}

func (r *vmRegisterer) createStaticHandlers(vmID ids.ID, factory vms.Factory) (map[string]*common.HTTPHandler, error) {
	vm, err := factory.New(nil)
	if err != nil {
		return nil, err
	}

	commonVM, ok := vm.(common.VM)
	if !ok {
		return nil, fmt.Errorf("%s doesn't implement VM", vmID)
	}

	handlers, err := commonVM.CreateStaticHandlers()
	if err != nil {
		r.config.Log.Error("creating static API endpoints for %q errored with: %s", vmID, err)

		if err := commonVM.Shutdown(); err != nil {
			return nil, fmt.Errorf("shutting down VM errored with: %w", err)
		}
		return nil, err
	}
	return handlers, nil
}

func (r *vmRegisterer) createStaticEndpoints(pathAdder server.PathAdder, handlers map[string]*common.HTTPHandler, defaultEndpoint string) error {
	// use a single lock for this entire vm
	lock := new(sync.RWMutex)
	// register the static endpoints
	for extension, service := range handlers {
		r.config.Log.Verbo("adding static API endpoint: %s%s", defaultEndpoint, extension)
		if err := pathAdder.AddRoute(service, lock, defaultEndpoint, extension); err != nil {
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

func (r vmRegisterer) getURLAliases(vmID ids.ID, defaultEndpoint string) ([]string, error) {
	aliases, err := r.config.VMManager.Aliases(vmID)
	if err != nil {
		return nil, err
	}

	var urlAliases []string
	for _, alias := range aliases {
		urlAlias := constants.VMAliasPrefix + alias
		if urlAlias != defaultEndpoint {
			urlAliases = append(urlAliases, urlAlias)
		}
	}
	return urlAliases, err
}

type readRegisterer struct {
	registerer VMRegisterer
}

func (r readRegisterer) Register(vmID ids.ID, factory vms.Factory) error {
	return r.registerer.RegisterWithReadLock(vmID, factory)
}
