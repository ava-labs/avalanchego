// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"net/http"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/database"
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
	// [fxs]: Feature extensions that attach to this VM.
	Initialize(
		ctx context.Context,
		chainCtx *snow.Context,
		db database.Database,
		genesisBytes []byte,
		upgradeBytes []byte,
		configBytes []byte,
		fxs []*Fx,
		appSender AppSender,
	) error

	// SetState communicates to VM its next state it starts
	SetState(ctx context.Context, state snow.State) error

	// Shutdown is called when the node is shutting down.
	Shutdown(context.Context) error

	// Version returns the version of the VM.
	Version(context.Context) (string, error)

	// Creates the HTTP handlers for custom chain network calls.
	//
	// This exposes handlers that the outside world can use to communicate with
	// the chain. Each handler has the path:
	// [Address of node]/ext/bc/[chain ID]/[extension]
	//
	// Returns a mapping from [extension]s to HTTP handlers.
	//
	// For example, if this VM implements an account-based payments system,
	// it have an extension called `accounts`, where clients could get
	// information about their accounts.
	CreateHandlers(context.Context) (map[string]http.Handler, error)

	// NewHTTPHandler returns the handler to register into the avalanchego http
	// server. The server.HTTPHeaderRoute header must be specified with this VM's
	// corresponding chain id by clients to route requests to this handler.
	NewHTTPHandler(ctx context.Context) (http.Handler, error)

	// WaitForEvent blocks until either the given context is cancelled, or a message is returned.
	WaitForEvent(ctx context.Context) (Message, error)
}
