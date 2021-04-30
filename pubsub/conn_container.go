// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pubsub

import "sync"

type connContainer struct {
	lock  sync.RWMutex
	conns map[*connection]struct{}
}

func newConnContainer() *connContainer {
	return &connContainer{conns: make(map[*connection]struct{})}
}

func (c *connContainer) Conns() []FilterInterface {
	c.lock.RLock()
	defer c.lock.RUnlock()
	resp := make([]FilterInterface, 0, len(c.conns))
	for c := range c.conns {
		// only active connections
		if c.isActive() {
			continue
		}
		resp = append(resp, c)
	}
	return resp
}

func (c *connContainer) Remove(conn *connection) {
	c.lock.Lock()
	defer c.lock.Unlock()
	delete(c.conns, conn)
}

func (c *connContainer) Add(conn *connection) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.conns[conn] = struct{}{}
}
