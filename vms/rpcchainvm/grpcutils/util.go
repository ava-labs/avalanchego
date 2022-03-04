// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package grpcutils

import (
	"fmt"
	"net/http"

	spb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/ava-labs/avalanchego/api/proto/ghttpproto"
)

func Errorf(code int, tmpl string, args ...interface{}) error {
	return GetGRPCErrorFromHTTPResponse(&ghttpproto.HandleSimpleHTTPResponse{
		Code: int32(code),
		Body: []byte(fmt.Sprintf(tmpl, args...)),
	})
}

// GetGRPCErrorFromHTTPRespone takes an HandleSimpleHTTPResponse as input and returns a gRPC error.
func GetGRPCErrorFromHTTPResponse(resp *ghttpproto.HandleSimpleHTTPResponse) error {
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
func GetHTTPResponseFromError(err error) (*ghttpproto.HandleSimpleHTTPResponse, bool) {
	s, ok := status.FromError(err)
	if !ok {
		return nil, false
	}

	status := s.Proto()
	if len(status.Details) != 1 {
		return nil, false
	}

	var resp ghttpproto.HandleSimpleHTTPResponse
	if err := anypb.UnmarshalTo(status.Details[0], &resp, proto.UnmarshalOptions{}); err != nil {
		return nil, false
	}

	return &resp, true
}

// GetHTTPHeader takes an http.Header as input and returns a slice of Header.
func GetHTTPHeader(hs http.Header) []*ghttpproto.Element {
	result := make([]*ghttpproto.Element, 0, len(hs))
	for k, vs := range hs {
		result = append(result, &ghttpproto.Element{
			Key:    k,
			Values: vs,
		})
	}
	return result
}

// MergeHTTPHeader takes a slice of Header and merges with http.Header map.
func MergeHTTPHeader(hs []*ghttpproto.Element, header http.Header) {
	for _, h := range hs {
		header[h.Key] = h.Values
	}
}
