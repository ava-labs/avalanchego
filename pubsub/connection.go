// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pubsub

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ava-labs/avalanchego/utils/bloom"
)

var (
	ErrFilterNotInitialized        = errors.New("filter not initialized")
	ErrAddressLimit                = errors.New("address limit exceeded")
	ErrInvalidFilterParam          = errors.New("invalid bloom filter params")
	ErrInvalidCommand              = errors.New("invalid command")
	_                       Filter = &connection{}
)

type Filter interface {
	Check(addr []byte) bool
}

// connection is a representation of the websocket connection.
type connection struct {
	s *Server

	// The websocket connection.
	conn *websocket.Conn

	// Buffered channel of outbound messages.
	send chan interface{}

	fp *FilterParam

	active uint32
}

func (c *connection) Check(addr []byte) bool {
	return c.fp.Check(addr)
}

func (c *connection) isActive() bool {
	active := atomic.LoadUint32(&c.active)
	return active != 0
}

func (c *connection) deactivate() {
	atomic.StoreUint32(&c.active, 0)
}

func (c *connection) Send(msg interface{}) bool {
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
func (c *connection) readPump() {
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
func (c *connection) writePump() {
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

func (c *connection) readMessage() error {
	_, r, err := c.conn.NextReader()
	if err != nil {
		return err
	}
	cmd := &Command{}
	err = json.NewDecoder(r).Decode(cmd)
	if err != nil {
		return err
	}

	switch {
	case cmd.NewBloom != nil:
		err = c.handleNewBloom(cmd.NewBloom)
	case cmd.NewSet != nil:
		c.handleNewSet(cmd.NewSet)
	case cmd.AddAddresses != nil:
		err = c.handleAddAddresses(cmd.AddAddresses)
	default:
		err = ErrInvalidCommand
	}
	if err != nil {
		c.Send(&errorMsg{
			Error: err.Error(),
		})
	}
	return err
}

func (c *connection) handleNewBloom(cmd *NewBloom) error {
	if !cmd.IsParamsValid() {
		return ErrInvalidFilterParam
	}
	filter, err := bloom.New(uint64(cmd.MaxElements), float64(cmd.CollisionProb), MaxBytes)
	if err != nil {
		return fmt.Errorf("bloom filter creation failed %w", err)
	}
	c.fp.SetFilter(filter)
	return nil
}

func (c *connection) handleNewSet(_ *NewSet) {
	c.fp.NewSet()
}

func (c *connection) handleAddAddresses(cmd *AddAddresses) error {
	if err := cmd.parseAddresses(); err != nil {
		return fmt.Errorf("address parse failed %w", err)
	}
	err := c.fp.Add(cmd.addressIds...)
	if err != nil {
		return fmt.Errorf("address append failed %w", err)
	}
	c.s.subscribedConnections.Add(c)
	return nil
}
