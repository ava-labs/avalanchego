package pubsub

import "sync"

type connContainer struct {
	lock  sync.RWMutex
	conns map[*Connection]struct{}
}

func newConnContainer() *connContainer {
	return &connContainer{conns: make(map[*Connection]struct{})}
}

func (c *connContainer) Conns() []*Connection {
	c.lock.RLock()
	defer c.lock.RUnlock()
	resp := make([]*Connection, 0, len(c.conns))
	for c := range c.conns {
		// only active connections
		if !c.isActive() {
			continue
		}
		resp = append(resp, c)
	}
	return resp
}

func (c *connContainer) Remove(conn *Connection) {
	c.lock.Lock()
	defer c.lock.Unlock()
	delete(c.conns, conn)
}

func (c *connContainer) Add(conn *Connection) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.conns[conn] = struct{}{}
}
