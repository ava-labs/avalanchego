// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package registry

import (
	"context"
	"fmt"
	"path"
	"sync"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api/server"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms"
)

var _ VMRegisterer = (*vmRegisterer)(nil)

// VMRegisterer defines functionality to install a virtual machine.
type VMRegisterer interface {
	registerer
	// RegisterWithReadLock installs the VM assuming that the http read-lock is
	// held.
	RegisterWithReadLock(context.Context, ids.ID, vms.Factory) error
}

type registerer interface {
	// Register installs the VM.
	Register(context.Context, ids.ID, vms.Factory) error
}

// VMRegistererConfig configures settings for VMRegisterer.
type VMRegistererConfig struct {
	APIServer    server.Server
	Log          logging.Logger
	VMFactoryLog logging.Logger
	VMManager    vms.Manager
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

func (r *vmRegisterer) Register(ctx context.Context, vmID ids.ID, factory vms.Factory) error {
	return r.register(ctx, r.config.APIServer, vmID, factory)
}

func (r *vmRegisterer) RegisterWithReadLock(ctx context.Context, vmID ids.ID, factory vms.Factory) error {
	return r.register(ctx, server.PathWriterFromWithReadLock(r.config.APIServer), vmID, factory)
}

func (r *vmRegisterer) register(ctx context.Context, pathAdder server.PathAdder, vmID ids.ID, factory vms.Factory) error {
	if err := r.config.VMManager.RegisterFactory(ctx, vmID, factory); err != nil {
		return err
	}
	handlers, err := r.createStaticHandlers(ctx, vmID, factory)
	if err != nil {
		return err
	}

	// all static endpoints go to the vm endpoint, defaulting to the vm id
	defaultEndpoint := path.Join(constants.VMAliasPrefix, vmID.String())

	if err := r.createStaticEndpoints(pathAdder, handlers, defaultEndpoint); err != nil {
		return err
	}
	urlAliases, err := r.getURLAliases(vmID, defaultEndpoint)
	if err != nil {
		return err
	}
	return pathAdder.AddAliases(defaultEndpoint, urlAliases...)
}

// Creates a dedicated VM instance for the sole purpose of serving the static
// handlers.
func (r *vmRegisterer) createStaticHandlers(
	ctx context.Context,
	vmID ids.ID,
	factory vms.Factory,
) (map[string]*common.HTTPHandler, error) {
	vm, err := factory.New(r.config.VMFactoryLog)
	if err != nil {
		return nil, err
	}

	commonVM, ok := vm.(common.VM)
	if !ok {
		return nil, fmt.Errorf("%s doesn't implement VM", vmID)
	}

	handlers, err := commonVM.CreateStaticHandlers(ctx)
	if err != nil {
		r.config.Log.Error("failed to create static API endpoints",
			zap.Stringer("vmID", vmID),
			zap.Error(err),
		)

		if err := commonVM.Shutdown(ctx); err != nil {
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
		r.config.Log.Verbo("adding static API endpoint",
			zap.String("endpoint", defaultEndpoint),
			zap.String("extension", extension),
		)
		if err := pathAdder.AddRoute(service, lock, defaultEndpoint, extension); err != nil {
			return fmt.Errorf(
				"failed to add static API endpoint %s%s: %w",
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
		urlAlias := path.Join(constants.VMAliasPrefix, alias)
		if urlAlias != defaultEndpoint {
			urlAliases = append(urlAliases, urlAlias)
		}
	}
	return urlAliases, err
}

type readRegisterer struct {
	registerer VMRegisterer
}

func (r readRegisterer) Register(ctx context.Context, vmID ids.ID, factory vms.Factory) error {
	return r.registerer.RegisterWithReadLock(ctx, vmID, factory)
}
