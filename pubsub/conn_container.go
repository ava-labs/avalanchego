// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pubsub

import "sync"

type connContainer struct {
	lock      sync.RWMutex
	conns     map[*connection]struct{}
	connsList []FilterInterface
}

func newConnContainer() *connContainer {
	return &connContainer{conns: make(map[*connection]struct{})}
}

func (c *connContainer) Conns() []FilterInterface {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return append([]FilterInterface{}, c.connsList...)
}

func (c *connContainer) Remove(conn *connection) {
	c.lock.Lock()
	defer c.lock.Unlock()
	delete(c.conns, conn)
	c.createConnsList()
}

func (c *connContainer) Add(conn *connection) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.conns[conn] = struct{}{}
	c.createConnsList()
}

func (c *connContainer) createConnsList() {
	resp := make([]FilterInterface, 0, len(c.conns))
	for c := range c.conns {
		resp = append(resp, c)
	}
	c.connsList = resp
}
