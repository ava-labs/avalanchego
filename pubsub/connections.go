// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pubsub

import "sync"

type connections struct {
	lock      sync.RWMutex
	conns     map[*connection]struct{}
	connsList []Filter
}

func newConnections() *connections {
	return &connections{
		conns: make(map[*connection]struct{}),
	}
}

func (c *connections) Conns() []Filter {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return append([]Filter{}, c.connsList...)
}

func (c *connections) Remove(conn *connection) {
	c.lock.Lock()
	defer c.lock.Unlock()

	delete(c.conns, conn)
	c.createConnsList()
}

func (c *connections) Add(conn *connection) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.conns[conn] = struct{}{}
	c.createConnsList()
}

func (c *connections) createConnsList() {
	resp := make([]Filter, 0, len(c.conns))
	for c := range c.conns {
		resp = append(resp, c)
	}
	c.connsList = resp
}
