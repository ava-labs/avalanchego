// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pubsub

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/units"
)

const (
	// Size of the ws read buffer
	readBufferSize = units.KiB

	// Size of the ws write buffer
	writeBufferSize = units.KiB

	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 10 * units.KiB // bytes

	// Maximum number of pending messages to send to a peer.
	maxPendingMessages = 1024 // messages

	// MaxBytes the max number of bytes for a filter
	MaxBytes = 1 * units.MiB

	// MaxAddresses the max number of addresses allowed
	MaxAddresses = 10000
)

type errorMsg struct {
	Error string `json:"error"`
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  readBufferSize,
	WriteBufferSize: writeBufferSize,
	CheckOrigin:     func(*http.Request) bool { return true },
}

// Server maintains the set of active clients and sends messages to the clients.
type Server struct {
	log  logging.Logger
	lock sync.RWMutex
	// conns a list of all our connections
	conns map[*connection]struct{}
	// subscribedConnections the connections that have activated subscriptions
	subscribedConnections *connections
}

func New(networkID uint32, log logging.Logger) *Server {
	return &Server{
		log:                   log,
		conns:                 make(map[*connection]struct{}),
		subscribedConnections: newConnections(),
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	wsConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.Debug("Failed to upgrade %s", err)
		return
	}
	conn := &connection{
		s:      s,
		conn:   wsConn,
		send:   make(chan interface{}, maxPendingMessages),
		fp:     NewFilterParam(),
		active: 1,
	}
	s.addConnection(conn)
}

func (s *Server) Publish(parser Filterer) {
	conns := s.subscribedConnections.Conns()
	toNotify, msg := parser.Filter(conns)
	for i, shouldNotify := range toNotify {
		if !shouldNotify {
			continue
		}
		conn := conns[i].(*connection)
		if !conn.Send(msg) {
			s.log.Verbo("dropping message to subscribed connection due to too many pending messages")
		}
	}
}

func (s *Server) addConnection(conn *connection) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.conns[conn] = struct{}{}

	go conn.writePump()
	go conn.readPump()
}

func (s *Server) removeConnection(conn *connection) {
	s.subscribedConnections.Remove(conn)

	s.lock.Lock()
	defer s.lock.Unlock()

	delete(s.conns, conn)
}
