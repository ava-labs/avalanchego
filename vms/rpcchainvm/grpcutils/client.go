// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package grpcutils

import (
	"math"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

const (
	// After a duration of this time if the client doesn't see any activity it
	// pings the server to see if the transport is still alive.
	// If set below 10s, a minimum value of 10s will be used instead.
	// grpc-go default infinity
	defaultClientKeepAliveTime = 30 * time.Second
	// After having pinged for keepalive check, the client waits for a duration
	// of Timeout and if no activity is seen even after that the connection is
	// closed. grpc-go default 20s
	defaultClientKeepAliveTimeOut = 10 * time.Second
	// If true, client sends keepalive pings even with no active RPCs. If false,
	// when there are no active RPCs, Time and Timeout will be ignored and no
	// keepalive pings will be sent. grpc-go default false
	defaultPermitWithoutStream = true
	// WaitForReady configures the action to take when an RPC is attempted on
	// broken connections or unreachable servers. If waitForReady is false and
	// the connection is in the TRANSIENT_FAILURE state, the RPC will fail
	// immediately. Otherwise, the RPC client will block the call until a
	// connection is available (or the call is canceled or times out) and will
	// retry the call if it fails due to a transient error. gRPC will not retry
	// if data was written to the wire unless the server indicates it did not
	// process the data. Please refer to
	// https://github.com/grpc/grpc/blob/master/doc/wait-for-ready.md.
	//
	// gRPC default behavior is to NOT "wait for ready".
	defaultWaitForReady = true
)

var DefaultDialOptions = []grpc.DialOption{
	grpc.WithDefaultCallOptions(
		grpc.MaxCallRecvMsgSize(math.MaxInt),
		grpc.MaxCallSendMsgSize(math.MaxInt),
		grpc.WaitForReady(defaultWaitForReady),
	),
	grpc.WithKeepaliveParams(keepalive.ClientParameters{
		Time:                defaultClientKeepAliveTime,
		Timeout:             defaultClientKeepAliveTimeOut,
		PermitWithoutStream: defaultPermitWithoutStream,
	}),
	grpc.WithTransportCredentials(insecure.NewCredentials()),
}

// gRPC clients created from this ClientConn will wait forever for the Server to
// become Ready. If you desire a dial timeout ensure context is properly plumbed
// to the client and use context.WithTimeout.
//
// Dial returns a gRPC ClientConn with the dial options as defined by
// DefaultDialOptions. DialOption can also optionally be passed.
func Dial(addr string, opts ...DialOption) (*grpc.ClientConn, error) {
	return grpc.Dial("passthrough:///"+addr, newDialOpts(opts...)...)
}

// DialOptions are options which can be applied to a gRPC client in addition to
// the defaults set by DefaultDialOptions.
type DialOptions struct {
	opts []grpc.DialOption
}

// append(DefaultDialOptions, ...) will always allocate a new slice and will
// not overwrite any potential data that may have previously been appended to
// DefaultServerOptions https://go.dev/ref/spec#Composite_literals
func newDialOpts(opts ...DialOption) []grpc.DialOption {
	d := &DialOptions{opts: DefaultDialOptions}
	d.applyOpts(opts)
	return d.opts
}

func (d *DialOptions) applyOpts(opts []DialOption) {
	for _, opt := range opts {
		opt(d)
	}
}

type DialOption func(*DialOptions)

// WithChainUnaryInterceptor takes a list of unary client interceptors which
// are added to the dial options.
func WithChainUnaryInterceptor(interceptors ...grpc.UnaryClientInterceptor) DialOption {
	return func(d *DialOptions) {
		d.opts = append(d.opts, grpc.WithChainUnaryInterceptor(interceptors...))
	}
}

// WithChainStreamInterceptor takes a list of stream client interceptors which
// are added to the dial options.
func WithChainStreamInterceptor(interceptors ...grpc.StreamClientInterceptor) DialOption {
	return func(d *DialOptions) {
		d.opts = append(d.opts, grpc.WithChainStreamInterceptor(interceptors...))
	}
}
