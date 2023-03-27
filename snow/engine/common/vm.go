// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
)

// VM describes the interface that all consensus VMs must implement
type VM interface {
	AppHandler

	// Returns nil if the VM is healthy.
	// Periodically called and reported via the node's Health API.
	health.Checker

	// Connector represents a handler that is called on connection connect/disconnect
	validators.Connector

	// Initialize this VM.
	// [chainCtx]: Metadata about this VM.
	//     [chainCtx.networkID]: The ID of the network this VM's chain is
	//                           running on.
	//     [chainCtx.chainID]: The unique ID of the chain this VM is running on.
	//     [chainCtx.Log]: Used to log messages
	//     [chainCtx.NodeID]: The unique staker ID of this node.
	//     [chainCtx.Lock]: A Read/Write lock shared by this VM and the
	//                      consensus engine that manages this VM. The write
	//                      lock is held whenever code in the consensus engine
	//                      calls the VM.
	// [dbManager]: The manager of the database this VM will persist data to.
	// [genesisBytes]: The byte-encoding of the genesis information of this
	//                 VM. The VM uses it to initialize its state. For
	//                 example, if this VM were an account-based payments
	//                 system, `genesisBytes` would probably contain a genesis
	//                 transaction that gives coins to some accounts, and this
	//                 transaction would be in the genesis block.
	// [toEngine]: The channel used to send messages to the consensus engine.
	// [fxs]: Feature extensions that attach to this VM.
	Initialize(
		ctx context.Context,
		chainCtx *snow.Context,
		dbManager manager.Manager,
		genesisBytes []byte,
		upgradeBytes []byte,
		configBytes []byte,
		toEngine chan<- Message,
		fxs []*Fx,
		appSender AppSender,
	) error

	// SetState communicates to VM its next state it starts
	SetState(ctx context.Context, state snow.State) error

	// Shutdown is called when the node is shutting down.
	Shutdown(context.Context) error

	// Version returns the version of the VM.
	Version(context.Context) (string, error)

	// Creates the HTTP handlers for custom VM network calls.
	//
	// This exposes handlers that the outside world can use to communicate with
	// a static reference to the VM. Each handler has the path:
	// [Address of node]/ext/VM/[VM ID]/[extension]
	//
	// Returns a mapping from [extension]s to HTTP handlers.
	//
	// Each extension can specify how locking is managed for convenience.
	//
	// For example, it might make sense to have an extension for creating
	// genesis bytes this VM can interpret.
	//
	// Note: If this method is called, no other method will be called on this VM.
	// Each registered VM will have a single instance created to handle static
	// APIs. This instance will be handled separately from instances created to
	// service an instance of a chain.
	CreateStaticHandlers(context.Context) (map[string]*HTTPHandler, error)

	// Creates the HTTP handlers for custom chain network calls.
	//
	// This exposes handlers that the outside world can use to communicate with
	// the chain. Each handler has the path:
	// [Address of node]/ext/bc/[chain ID]/[extension]
	//
	// Returns a mapping from [extension]s to HTTP handlers.
	//
	// Each extension can specify how locking is managed for convenience.
	//
	// For example, if this VM implements an account-based payments system,
	// it have an extension called `accounts`, where clients could get
	// information about their accounts.
	CreateHandlers(context.Context) (map[string]*HTTPHandler, error)
}
