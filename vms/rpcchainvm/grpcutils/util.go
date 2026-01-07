// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package grpcutils

import (
	"fmt"
	"net/http"
	"time"

	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	httppb "github.com/ava-labs/avalanchego/proto/pb/http"
	spb "google.golang.org/genproto/googleapis/rpc/status"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
)

// GetGRPCErrorFromHTTPResponse takes an HandleSimpleHTTPResponse as input and returns a gRPC error.
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

// SetHeaders sets headers to next
func SetHeaders(headers http.Header, next []*httppb.Element) {
	clear(headers)

	for _, h := range next {
		headers[h.Key] = h.Values
	}
}

// TimestampAsTime validates timestamppb timestamp and returns time.Time.
func TimestampAsTime(ts *tspb.Timestamp) (time.Time, error) {
	if err := ts.CheckValid(); err != nil {
		return time.Time{}, fmt.Errorf("invalid timestamp: %w", err)
	}
	return ts.AsTime(), nil
}

// TimestampFromTime converts time.Time to a timestamppb timestamp.
func TimestampFromTime(time time.Time) *tspb.Timestamp {
	return tspb.New(time)
}

// EnsureValidResponseCode ensures that the response code is valid otherwise it returns 500.
func EnsureValidResponseCode(code int) int {
	// Response code outside of this range is invalid and could panic.
	// ref. https://www.w3.org/Protocols/rfc2616/rfc2616-sec10.html
	if code < 100 || code > 599 {
		return http.StatusInternalServerError
	}
	return code
}
