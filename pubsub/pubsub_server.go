// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pubsub

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"

	"github.com/ava-labs/avalanchego/utils/bloom"
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
	lock sync.RWMutex
	log  logging.Logger
	hrp  string
	// TODO make the key not a pointer
	conns            map[*Connection]struct{}
	eventTypeToConns map[EventType]*connContainer
}

// NewPubSubServer ...
func New(networkID uint32, log logging.Logger) *Server {
	hrp := constants.GetHRP(networkID)
	return &Server{
		log:   log,
		hrp:   hrp,
		conns: make(map[*Connection]struct{}),
		eventTypeToConns: map[EventType]*connContainer{
			Accepted: newConnContainer(),
			Rejected: newConnContainer(),
			Verified: newConnContainer(),
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

	for _, conn := range conns.Conns() {
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
	s.conns[conn] = struct{}{}
	s.lock.Unlock()

	go conn.writePump()
	go conn.readPump()
}

func (s *Server) removeConnection(conn *Connection) {
	s.lock.RLock()
	for _, conns := range s.eventTypeToConns {
		conns.Remove(conn)
	}
	s.lock.RUnlock()

	s.lock.Lock()
	defer s.lock.Unlock()
	delete(s.conns, conn)
}

func (s *Server) subscribe(conn *Connection, eventType EventType) {
	s.lock.RLock()
	conns, ok := s.eventTypeToConns[eventType]
	s.lock.RUnlock()
	if !ok {
		s.log.Warn("got unexpected event type %v", eventType)
		return
	}
	conns.Add(conn)
}

func (s *Server) unsubscribe(conn *Connection, eventType EventType) {
	s.lock.RLock()
	conns, ok := s.eventTypeToConns[eventType]
	s.lock.RUnlock()
	if !ok {
		s.log.Warn("got unexpected event type %v", eventType)
		return
	}
	conns.Remove(conn)
}

// Connection is a representation of the websocket connection.
type Connection struct {
	s *Server

	// The websocket connection.
	conn *websocket.Conn

	// Buffered channel of outbound messages.
	send chan interface{}

	fp *FilterParam

	active uint32
}

func (c *Connection) isActive() bool {
	active := atomic.LoadUint32(&c.active)
	return active != 0
}

func (c *Connection) deactivate() {
	atomic.StoreUint32(&c.active, 0)
}

func (c *Connection) Send(msg interface{}) bool {
	select {
	case c.send <- msg:
		return true
	default:
	}
	return false
}

// readPump pumps messages from the websocket connection to the hub.
//
// The application runs readPump in a per-connection goroutine. The application
// ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.
func (c *Connection) readPump() {
	defer func() {
		c.deactivate()
		c.s.removeConnection(c)

		// close is called by both the writePump and the readPump so one of them
		// will always error
		_ = c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	// SetReadDeadline returns an error if the connection is corrupted
	if err := c.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		return
	}
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		err := c.readMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.s.log.Debug("Unexpected close in websockets: %s", err)
			}
			break
		}
	}
}

// writePump pumps messages from the hub to the websocket connection.
//
// A goroutine running writePump is started for each connection. The
// application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
func (c *Connection) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		c.deactivate()
		ticker.Stop()
		c.s.removeConnection(c)

		// close is called by both the writePump and the readPump so one of them
		// will always error
		_ = c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				c.s.log.Debug("failed to set the write deadline, closing the connection due to %s", err)
				return
			}
			if !ok {
				// The hub closed the channel. Attempt to close the connection
				// gracefully.
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteJSON(message); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				c.s.log.Debug("failed to set the write deadline, closing the connection due to %s", err)
				return
			}
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Connection) readMessage() error {
	_, r, err := c.conn.NextReader()
	if err != nil {
		return err
	}
	cmdMsg, err := NewCommandMessage(r, c.s.hrp)
	if err != nil {
		return err
	}

	switch cmdMsg.Command {
	case "":
		if cmdMsg.Unsubscribe {
			c.s.subscribe(c, cmdMsg.EventType)
		} else {
			c.s.unsubscribe(c, cmdMsg.EventType)
		}
		return nil
	case CommandFilters:
		return c.handleCommandFilterUpdate(cmdMsg)
	case CommandAddresses:
		c.handleCommandAddressUpdate(cmdMsg)
		return nil
	default:
		errmsg := &errorMsg{Error: fmt.Sprintf("command '%s' invalid", cmdMsg.Command)}
		c.Send(errmsg)
		return fmt.Errorf(errmsg.Error)
	}
}

func (c *Connection) handleCommandFilterUpdate(cmdMsg *CommandMessage) error {
	if cmdMsg.Unsubscribe {
		c.fp.SetFilter(nil)
		return nil
	}
	bfilter, err := c.updateNewFilter(cmdMsg)
	if err != nil {
		c.Send(&errorMsg{Error: fmt.Sprintf("filter create failed %v", err)})
		return err
	}
	bfilter.Add(cmdMsg.AddressIds...)
	return nil
}

func (c *Connection) updateNewFilter(cmdMsg *CommandMessage) (bloom.Filter, error) {
	bfilter := c.fp.Filter()
	if !(bfilter == nil || cmdMsg.IsNewFilter()) {
		return bfilter, nil
	}
	// no filter exists..  Or they provided filter params
	cmdMsg.FilterOrDefault()
	bfilter, err := bloom.New(cmdMsg.FilterMax, cmdMsg.FilterError, MaxBytes)
	if err != nil {
		return nil, err
	}
	return c.fp.SetFilter(bfilter), nil
}

func (c *Connection) handleCommandAddressUpdate(cmdMsg *CommandMessage) {
	if c.fp.Len()+len(cmdMsg.AddressIds) > MaxAddresses {
		c.Send(&errorMsg{Error: "address limit reached"})
		return
	}
	c.fp.UpdateAddressMulti(cmdMsg.Unsubscribe, cmdMsg.AddressIds...)
}

func ByteToID(address []byte) ids.ShortID {
	var sid ids.ShortID
	copy(sid[:], address)
	return sid
}
