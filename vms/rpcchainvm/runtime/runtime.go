// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package runtime

import (
	"context"
	"errors"
	"time"
)

const (
	// Address of the runtime engine server.
	EngineAddressKey = "AVALANCHE_VM_RUNTIME_ENGINE_ADDR"

	// Duration before handshake timeout during bootstrap.
	DefaultHandshakeTimeout = 5 * time.Second

	// Duration of time to wait for graceful termination to complete.
	DefaultGracefulTimeout = 5 * time.Second
)

var (
	ErrProtocolVersionMismatch = errors.New("RPCChainVM protocol version mismatch between AvalancheGo and Virtual Machine plugin")
	ErrHandshakeFailed         = errors.New("handshake failed")
	ErrInvalidConfig           = errors.New("invalid config")
	ErrProcessNotFound         = errors.New("vm process not found")
)

type Initializer interface {
	// Initialize provides AvalancheGo with compatibility, networking and
	// process information of a VM.
	Initialize(ctx context.Context, protocolVersion uint, vmAddr string) error
}

type Stopper interface {
	// Stop begins shutdown of a VM. This method must not block
	// and multiple calls to this method will result in no-op.
	Stop(ctx context.Context)
}

type Tracker interface {
	// TrackRuntime adds a VM stopper to the manager.
	TrackRuntime(runtime Stopper)
}

type Manager interface {
	Tracker
	// Stop all managed VMs.
	Stop(ctx context.Context)
}
