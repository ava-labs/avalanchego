// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package grpcutils

import (
	"context"
	"io"
	"sync"

	"github.com/ava-labs/avalanchego/utils/wrappers"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const (
	maxOps = 100
)

var (
	_ Conn              = &redialer{}
	_ grpc.ClientStream = &stream{}
)

type Conn interface {
	io.Closer
	grpc.ClientConnInterface
}

//  1. Call Close on the ClientConn.
//  2. Cancel the context provided.
//  3. Call RecvMsg until a non-nil error is returned. A protobuf-generated
//     client-streaming RPC, for instance, might use the helper function
//     CloseAndRecv (note that CloseSend does not Recv, therefore is not
//     guaranteed to release all resources).
//  4. Receive a non-nil, non-io.EOF error from Header or SendMsg.
type stream struct {
	grpc.ClientStream

	// TODO: decRef when the context provided by [NewStream] is canceled.
	decRef sync.Once
	conn   *conn
}

func (s *stream) Header() (metadata.MD, error) {
	md, err := s.ClientStream.Header()
	if err != nil && err != io.EOF {
		s.close()
	}
	return md, err
}

func (s *stream) SendMsg(m interface{}) error {
	err := s.ClientStream.SendMsg(m)
	if err != nil && err != io.EOF {
		s.close()
	}
	return err
}

func (s *stream) RecvMsg(m interface{}) error {
	err := s.ClientStream.RecvMsg(m)
	if err != nil {
		s.close()
	}
	return err
}

func (s *stream) close() {
	s.decRef.Do(func() {
		s.conn.redialer.lock.Lock()
		s.conn.DecRef()
		s.conn.redialer.lock.Unlock()
	})
}

type conn struct {
	redialer *redialer

	refs int // number of references to this connection
	ops  int // number of operations performed on this connection
	conn *grpc.ClientConn
}

// Lock is held
func (c *conn) IncRef() {
	c.refs++
	c.ops++
}

// Lock is held
func (c *conn) DecRef() {
	c.refs--
	if !c.redialer.closed && c.refs == 0 {
		c.redialer.closeErrs.Add(c.conn.Close())
		delete(c.redialer.oldConns, c)
	}
}

type redialer struct {
	addr string
	opts []grpc.DialOption

	lock        sync.Mutex
	closed      bool
	currentConn *conn
	oldConns    map[*conn]struct{}
	closeErrs   wrappers.Errs
}

// Lock is held
func (r *redialer) getConn() (*conn, error) {
	if r.currentConn.ops >= maxOps {
		newConn, err := createClientConn(r.addr, r.opts...)
		if err != nil {
			return nil, err
		}

		oldConn := r.currentConn
		r.currentConn = &conn{
			redialer: r,
			refs:     1,
			conn:     newConn,
		}
		r.oldConns[oldConn] = struct{}{}
		oldConn.DecRef()
	}

	r.currentConn.IncRef()
	return r.currentConn, nil
}

func (r *redialer) Invoke(ctx context.Context, method string, args interface{}, reply interface{}, opts ...grpc.CallOption) error {
	r.lock.Lock()
	if r.closed {
		r.lock.Unlock()
		return r.currentConn.conn.Invoke(ctx, method, args, reply, opts...)
	}
	c, err := r.getConn()
	r.lock.Unlock()
	if err != nil {
		return err
	}

	err = c.conn.Invoke(ctx, method, args, reply, opts...)

	r.lock.Lock()
	c.DecRef()
	r.lock.Unlock()
	return err
}

// We don't currently use any Streams... So this really is just for completeness
func (r *redialer) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	r.lock.Lock()
	if r.closed {
		r.lock.Unlock()
		return r.currentConn.conn.NewStream(ctx, desc, method, opts...)
	}
	c, err := r.getConn()
	r.lock.Unlock()
	if err != nil {
		return nil, err
	}

	s, err := c.conn.NewStream(ctx, desc, method, opts...)
	if err != nil {
		r.lock.Lock()
		c.DecRef()
		r.lock.Unlock()
		return nil, err
	}

	return &stream{
		ClientStream: s,
		conn:         c,
	}, nil
}

func (r *redialer) Close() error {
	r.lock.Lock()
	defer r.lock.Unlock()
	r.closed = true

	r.closeErrs.Add(r.currentConn.conn.Close())
	for conn := range r.oldConns {
		r.closeErrs.Add(conn.conn.Close())
		delete(r.oldConns, conn)
	}
	return r.closeErrs.Err
}

func Dial(addr string, opts ...grpc.DialOption) (Conn, error) {
	if len(opts) == 0 {
		opts = append(opts, DefaultDialOptions...)
	}

	c, err := createClientConn(addr, opts...)
	if err != nil {
		return nil, err
	}

	r := &redialer{
		addr: addr,
		opts: opts,
	}
	r.currentConn = &conn{
		redialer: r,
		refs:     1,
		conn:     c,
	}
	return r, nil
}
