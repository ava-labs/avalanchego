// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package grpcutils

import (
	"fmt"
	"math"
	"net"
	"net/http"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	spb "google.golang.org/genproto/googleapis/rpc/status"

	httppb "github.com/ava-labs/avalanchego/proto/pb/http"
)

const (
	// Server:

	// MinTime is the minimum amount of time a client should wait before sending
	// a keepalive ping. grpc-go default 5 mins
	defaultServerKeepAliveMinTime = 5 * time.Second
	// After a duration of this time if the server doesn't see any activity it
	// pings the client to see if the transport is still alive.
	// If set below 1s, a minimum value of 1s will be used instead.
	// grpc-go default 2h
	defaultServerKeepAliveInterval = 2 * time.Hour
	// After having pinged for keepalive check, the server waits for a duration
	// of Timeout and if no activity is seen even after that the connection is
	// closed. grpc-go default 20s
	defaultServerKeepAliveTimeout = 20 * time.Second

	// Client:

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
)

var (
	DefaultDialOptions = []grpc.DialOption{
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(math.MaxInt)),
		grpc.WithDefaultCallOptions(grpc.MaxCallSendMsgSize(math.MaxInt)),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                defaultClientKeepAliveTime,
			Timeout:             defaultClientKeepAliveTimeOut,
			PermitWithoutStream: defaultPermitWithoutStream,
		}),
	}

	DefaultServerOptions = []grpc.ServerOption{
		grpc.MaxRecvMsgSize(math.MaxInt),
		grpc.MaxSendMsgSize(math.MaxInt),
		grpc.MaxConcurrentStreams(math.MaxUint32),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             defaultServerKeepAliveMinTime,
			PermitWithoutStream: defaultPermitWithoutStream,
		}),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    defaultServerKeepAliveInterval,
			Timeout: defaultServerKeepAliveTimeout,
		}),
	}
)

func Errorf(code int, tmpl string, args ...interface{}) error {
	return GetGRPCErrorFromHTTPResponse(&httppb.HandleSimpleHTTPResponse{
		Code: int32(code),
		Body: []byte(fmt.Sprintf(tmpl, args...)),
	})
}

// GetGRPCErrorFromHTTPRespone takes an HandleSimpleHTTPResponse as input and returns a gRPC error.
func GetGRPCErrorFromHTTPResponse(resp *httppb.HandleSimpleHTTPResponse) error {
	a, err := anypb.New(resp)
	if err != nil {
		return err
	}

	return status.ErrorProto(&spb.Status{
		Code:    resp.Code,
		Message: string(resp.Body),
		Details: []*anypb.Any{a},
	})
}

// GetHTTPResponseFromError takes an gRPC error as input and returns a gRPC
// HandleSimpleHTTPResponse.
func GetHTTPResponseFromError(err error) (*httppb.HandleSimpleHTTPResponse, bool) {
	s, ok := status.FromError(err)
	if !ok {
		return nil, false
	}

	status := s.Proto()
	if len(status.Details) != 1 {
		return nil, false
	}

	var resp httppb.HandleSimpleHTTPResponse
	if err := anypb.UnmarshalTo(status.Details[0], &resp, proto.UnmarshalOptions{}); err != nil {
		return nil, false
	}

	return &resp, true
}

// GetHTTPHeader takes an http.Header as input and returns a slice of Header.
func GetHTTPHeader(hs http.Header) []*httppb.Element {
	result := make([]*httppb.Element, 0, len(hs))
	for k, vs := range hs {
		result = append(result, &httppb.Element{
			Key:    k,
			Values: vs,
		})
	}
	return result
}

// MergeHTTPHeader takes a slice of Header and merges with http.Header map.
func MergeHTTPHeader(hs []*httppb.Element, header http.Header) {
	for _, h := range hs {
		header[h.Key] = h.Values
	}
}

func Serve(listener net.Listener, grpcServerFunc func([]grpc.ServerOption) *grpc.Server) {
	var opts []grpc.ServerOption
	grpcServer := grpcServerFunc(opts)

	// TODO: While errors will be reported later, it could be useful to somehow
	//       log this if it is the primary error.
	//
	// There is nothing to with the error returned by serve here. Later requests
	// will propegate their error if they occur.
	_ = grpcServer.Serve(listener)

	// Similarly, there is nothing to with an error when the listener is closed.
	_ = listener.Close()
}

func createClientConn(addr string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	return grpc.Dial(addr, opts...)
}

// NewDefaultServer ensures the plugin service is served with proper
// defaults. This should always be passed to GRPCServer field of
// plugin.ServeConfig.
func NewDefaultServer(opts []grpc.ServerOption) *grpc.Server {
	if len(opts) == 0 {
		opts = append(opts, DefaultServerOptions...)
	}
	return grpc.NewServer(opts...)
}
