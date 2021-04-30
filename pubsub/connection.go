// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pubsub

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/gorilla/websocket"
)

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
	if !c.isActive() {
		return false
	}
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
