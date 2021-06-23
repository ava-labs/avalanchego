// (c) 2019-2020, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package node

import (
	"sync"

	"github.com/ethereum/go-ethereum/event"

	"github.com/ava-labs/coreth/accounts"
	"github.com/ava-labs/coreth/rpc"
)

// Node is a container on which services can be registered.
type Node struct {
	eventmux *event.TypeMux
	config   *Config
	accman   *accounts.Manager
	// log      log.Logger
	// ephemKeystore string // if non-empty, the key directory that will be removed by Stop
	// dirLock       fileutil.Releaser // prevents concurrent use of instance directory
	// stop          chan struct{}     // Channel to wait for termination notifications
	// server        *p2p.Server // Currently running P2P networking layer
	// startStopLock sync.Mutex // Start/Stop are protected by an additional lock
	// state int // Tracks state of node lifecycle

	lock    sync.Mutex
	rpcAPIs []rpc.API // List of APIs currently provided by the node
	// inprocHandler *rpc.Server // In-process RPC request handler to process the API requests

	// databases map[*closeTrackingDB]struct{} // All open databases

	corethVersion string
}

// const (
// 	initializingState = iota
// 	closedState
// )

// func (n *Node) openDataDir() error {
// 	if n.config.DataDir == "" {
// 		return nil // ephemeral
// 	}
//
// 	instdir := filepath.Join(n.config.DataDir, n.config.name())
// 	if err := os.MkdirAll(instdir, 0700); err != nil {
// 		return err
// 	}
// 	// Lock the instance directory to prevent concurrent use by another instance as well as
// 	// accidental use of the instance directory as a database.
// 	release, _, err := fileutil.Flock(filepath.Join(instdir, "LOCK"))
// 	if err != nil {
// 		return convertFileLockError(err)
// 	}
// 	n.dirLock = release
// 	return nil
// }

// New creates a new P2P node, ready for protocol registration.
func New(conf *Config) (*Node, error) {
	// Copy config and resolve the datadir so future changes to the current
	// working directory don't affect the node.
	confCopy := *conf
	conf = &confCopy

	node := &Node{
		config:   conf,
		eventmux: new(event.TypeMux),
	}

	// Register built-in APIs.
	node.rpcAPIs = append(node.rpcAPIs, node.apis()...)

	// Ensure that the AccountManager method works before the node has started. We rely on
	// this in cmd/geth.
	am, err := makeAccountManager(conf)
	if err != nil {
		return nil, err
	}
	node.accman = am

	// Configure RPC servers.

	return node, nil
}

// Config returns the configuration of node.
func (n *Node) Config() *Config {
	return n.config
}

// AccountManager retrieves the account manager used by the protocol stack.
func (n *Node) AccountManager() *accounts.Manager {
	return n.accman
}

// EventMux retrieves the event multiplexer used by all the network services in
// the current protocol stack.
func (n *Node) EventMux() *event.TypeMux {
	return n.eventmux
}

// RegisterAPIs registers the APIs a service provides on the node.
func (n *Node) RegisterAPIs(apis []rpc.API) {
	n.lock.Lock()
	defer n.lock.Unlock()

	n.rpcAPIs = append(n.rpcAPIs, apis...)
}
