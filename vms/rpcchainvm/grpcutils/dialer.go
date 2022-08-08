// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package grpcutils

import (
	"context"
	"errors"
	"io"
	"sync"

	"golang.org/x/sync/errgroup"

	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
)

const (
	maxOps = 100
)

var (
	_ Conn = &redialer{}

	logger = grpclog.Component("external-dialer")

	errUnimplemented = errors.New("unimplemented")
)

type Conn interface {
	io.Closer
	grpc.ClientConnInterface
}

type conn struct {
	redialer *redialer

	// When this connection is created, the redialer will hold a reference.
	// Whenever the connection is used, the call will grab a reference and then
	// remove the reference when the call is done.
	// When this connection is rotated, the redialer will remove the reference.
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
		c.redialer.closer.Go(c.conn.Close)
		delete(c.redialer.oldConns, c)

		logger.Infof(
			"closing rotated connection %s after %d operations",
			c.conn.Target(),
			c.ops,
		)
	}
}

// redialer is a wrapper around a grpc.ClientConn that periodically rotates the
// connection. This avoids overflowing the underlying streamId.
type redialer struct {
	addr string
	opts []grpc.DialOption

	lock        sync.Mutex
	closed      bool
	currentConn *conn // never nil
	oldConns    map[*conn]struct{}
	closer      errgroup.Group
}

// Lock is held
// redialer is not closed
func (r *redialer) getConn() (*conn, error) {
	if r.currentConn.ops >= maxOps {
		logger.Infof(
			"rotating connection %s after %d operations",
			r.currentConn.conn.Target(),
			r.currentConn.ops,
		)

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
		// Calling Invoke on the closed connection is an easy way to ensure we
		// act similarly to the underlying grpc implementation.
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

// We don't currently use any Streams
func (r *redialer) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, errUnimplemented
}

func (r *redialer) Close() error {
	r.lock.Lock()
	defer r.lock.Unlock()
	r.closed = true

	r.closer.Go(r.currentConn.conn.Close)
	for conn := range r.oldConns {
		r.closer.Go(conn.conn.Close)
		delete(r.oldConns, conn)
	}
	return r.closer.Wait()
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
		addr:     addr,
		opts:     opts,
		oldConns: make(map[*conn]struct{}),
	}
	r.currentConn = &conn{
		redialer: r,
		refs:     1,
		conn:     c,
	}
	return r, nil
}
