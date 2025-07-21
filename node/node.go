// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
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
	"github.com/ava-labs/libevm/accounts"
	"github.com/ava-labs/subnet-evm/rpc"
)

// Node is a container on which services can be registered.
type Node struct {
	config *Config
	accman *accounts.Manager
}

// New creates a new P2P node, ready for protocol registration.
func New(conf *Config) (*Node, error) {
	// Copy config and resolve the datadir so future changes to the current
	// working directory don't affect the node.
	confCopy := *conf
	conf = &confCopy

	node := &Node{config: conf}

	// Ensure that the AccountManager method works before the node has started. We rely on
	// this in cmd/geth.
	am, err := makeAccountManager(conf)
	if err != nil {
		return nil, err
	}
	node.accman = am

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

// RegisterAPIs registers the APIs a service provides on the node.
func (n *Node) APIs() []rpc.API {
	return n.apis()
}
