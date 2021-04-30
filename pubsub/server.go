// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pubsub

import (
	"net/http"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/utils/logging"

	"github.com/gorilla/websocket"
)

const (
	// Size of the ws read buffer
	readBufferSize = 1024

	// Size of the ws write buffer
	writeBufferSize = 1024

	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 10 * 1024 // bytes

	// Maximum number of pending messages to send to a peer.
	maxPendingMessages = 1024 // messages

	// MaxBytes the max number of bytes for a filter
	MaxBytes = 1 * 1024 * 1024

	// MaxAddresses the max number of addresses allowed
	MaxAddresses = 10000

	CommandFilters   = "filters"
	CommandAddresses = "addresses"

	ParamAddress = "address"

	DefaultFilterMax   = 1000
	DefaultFilterError = .1
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
	lock                 sync.RWMutex
	log                  logging.Logger
	conns                map[FilterInterface]struct{}
	connectionsContainer *connContainer
}

func New(networkID uint32, log logging.Logger) *Server {
	return &Server{
		log:                  log,
		conns:                make(map[FilterInterface]struct{}),
		connectionsContainer: newConnContainer(),
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

// Publish ...
func (s *Server) Publish(msg interface{}, parser Parser) {
	pubconns, msg := parser.Filter(s.connectionsContainer.Conns())
	for _, c := range pubconns {
		s.publishMsg(c.(*connection), msg)
	}
}

func (s *Server) publishMsg(conn *connection, msg interface{}) {
	if !conn.Send(msg) {
		s.log.Verbo("dropping message to subscribed connection due to too many pending messages")
	}
}

func (s *Server) addConnection(conn *connection) {
	s.lock.Lock()
	s.conns[conn] = struct{}{}
	s.lock.Unlock()

	go conn.writePump()
	go conn.readPump()
}

func (s *Server) removeConnection(conn *connection) {
	s.unsubscribe(conn)
	s.lock.Lock()
	defer s.lock.Unlock()
	delete(s.conns, conn)
}

func (s *Server) subscribe(conn *connection) {
	s.connectionsContainer.Add(conn)
}

func (s *Server) unsubscribe(conn *connection) {
	s.connectionsContainer.Remove(conn)
}
