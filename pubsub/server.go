// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pubsub

import (
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"

	"github.com/ava-labs/avalanchego/utils/logging"

	"github.com/gorilla/websocket"
)

type EventType int

const (
	Accepted EventType = iota
	Rejected
	Verified
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

type Publish struct {
	EventType EventType   `json:"eventType"`
	Value     interface{} `json:"value"`
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  readBufferSize,
	WriteBufferSize: writeBufferSize,
	CheckOrigin:     func(*http.Request) bool { return true },
}

// Server maintains the set of active clients and sends messages to the clients.
type Server struct {
	nextConnID int32
	lock       sync.RWMutex
	log        logging.Logger
	hrp        string
	conns      map[int32]*Connection
	// Event Type --> Connection ID --> Connection
	// where Connection is subscribed to the given event type
	eventTypeToConns map[EventType]map[int32]*Connection
}

// NewPubSubServer ...
func New(networkID uint32, log logging.Logger) *Server {
	hrp := constants.GetHRP(networkID)
	return &Server{
		log:   log,
		hrp:   hrp,
		conns: make(map[int32]*Connection),
		eventTypeToConns: map[EventType]map[int32]*Connection{
			Accepted: {},
			Rejected: {},
			Verified: {},
		},
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	wsConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.Debug("Failed to upgrade %s", err)
		return
	}
	conn := &Connection{
		id:     atomic.AddInt32(&s.nextConnID, 1),
		s:      s,
		conn:   wsConn,
		send:   make(chan interface{}, maxPendingMessages),
		fp:     NewFilterParam(),
		active: 1,
	}
	s.addConnection(conn)
}

// Publish ...
func (s *Server) Publish(eventType EventType, msg interface{}, parser Parser) {
	s.lock.RLock()
	conns, ok := s.eventTypeToConns[eventType]
	s.lock.RUnlock()
	if !ok {
		s.log.Warn("got unexpected event type %v", eventType)
		return
	}

	for _, conn := range conns {
		m := &Publish{
			EventType: eventType,
			Value:     msg,
		}
		if conn.fp.HasFilter() {
			fr := parser.Filter(conn.fp)
			if fr == nil {
				continue
			}
			fr.Address, _ = formatting.FormatBech32(s.hrp, fr.AddressID[:])
			m.Value = fr
		}
		s.publishMsg(conn, m)
	}
}

func (s *Server) publishMsg(conn *Connection, msg interface{}) {
	if !conn.Send(msg) {
		s.log.Verbo("dropping message to subscribed connection due to too many pending messages")
	}
}

func (s *Server) addConnection(conn *Connection) {
	s.lock.Lock()
	s.conns[conn.id] = conn
	s.lock.Unlock()

	go conn.writePump()
	go conn.readPump()
}

func (s *Server) removeConnection(connID int32) {
	s.lock.Lock()
	defer s.lock.Unlock()

	// TODO make sure the connection is closed
	for _, conns := range s.eventTypeToConns {
		delete(conns, connID)
	}
	delete(s.conns, connID)
}

func (s *Server) subscribe(conn *Connection, eventType EventType) {
	s.lock.Lock()
	defer s.lock.Unlock()

	conns, ok := s.eventTypeToConns[eventType]
	if !ok {
		s.log.Warn("got unexpected event type %v", eventType)
		return
	}
	conns[conn.id] = conn
}

func (s *Server) unsubscribe(connID int32, eventType EventType) {
	s.lock.Lock()
	defer s.lock.Unlock()

	conns, ok := s.eventTypeToConns[eventType]
	if !ok {
		s.log.Warn("got unexpected event type %v", eventType)
		return
	}
	delete(conns, connID)
}

func ByteToID(address []byte) ids.ShortID {
	var sid ids.ShortID
	copy(sid[:], address)
	return sid
}
