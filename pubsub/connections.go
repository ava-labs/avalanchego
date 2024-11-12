// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pubsub

import (
	"sync"

	"github.com/ava-labs/avalanchego/utils/set"
)

type connections struct {
	lock      sync.RWMutex
	conns     set.Set[*connection]
	connsList []Filter
}

func newConnections() *connections {
	return &connections{}
}

func (c *connections) Conns() []Filter {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return append([]Filter{}, c.connsList...)
}

func (c *connections) Remove(conn *connection) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.conns.Remove(conn)
	c.createConnsList()
}

func (c *connections) Add(conn *connection) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.conns.Add(conn)
	c.createConnsList()
}

func (c *connections) createConnsList() {
	resp := make([]Filter, 0, len(c.conns))
	for c := range c.conns {
		resp = append(resp, c)
	}
	c.connsList = resp
}
