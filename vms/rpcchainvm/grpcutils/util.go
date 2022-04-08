// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package grpcutils

import (
	"fmt"
	"math"
	"net"
	"net/http"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	spb "google.golang.org/genproto/googleapis/rpc/status"

	httppb "github.com/ava-labs/avalanchego/proto/pb/http"
)

var (
	DefaultDialOptions = []grpc.DialOption{
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(math.MaxInt)),
		grpc.WithDefaultCallOptions(grpc.MaxCallSendMsgSize(math.MaxInt)),
	}
	DefaultServerOptions = []grpc.ServerOption{
		grpc.MaxRecvMsgSize(math.MaxInt),
		grpc.MaxSendMsgSize(math.MaxInt),
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
