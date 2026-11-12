// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validator

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/ava-labs/avalanchego/network/p2p/oracle"

	pb "github.com/ava-labs/avalanchego/proto/pb/oracle"
)

var _ oracle.SidecarClient = (*GRPCSidecarClient)(nil)

// ErrSidecarRejected is returned when the sidecar responds with a non-OK,
// non-Unavailable status (i.e. the event was rejected as invalid).
var ErrSidecarRejected = errors.New("sidecar rejected event")

// GRPCSidecarClient calls a sidecar process over gRPC. It is the
// production implementation of SidecarClient.
//
// The sidecar must implement the OracleSidecar gRPC service defined in
// proto/oracle/oracle.proto.
type GRPCSidecarClient struct {
	client pb.OracleSidecarClient
}

// NewGRPCSidecarClient dials the sidecar at addr and returns a client.
// addr is a gRPC target such as "127.0.0.1:9900".
func NewGRPCSidecarClient(addr string) (*GRPCSidecarClient, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to create oracle sidecar client for %s: %w", addr, err)
	}
	return &GRPCSidecarClient{
		client: pb.NewOracleSidecarClient(conn),
	}, nil
}

func (c *GRPCSidecarClient) Verify(ctx context.Context, event *oracle.OracleEvent) error {
	_, err := c.client.Verify(ctx, &pb.VerifyRequest{
		MessageBytes:  event.Message.Bytes(),
		Justification: event.Justification,
	})
	if err == nil {
		return nil
	}
	if status.Code(err) == codes.Unavailable {
		return fmt.Errorf("sidecar unreachable: %w", oracle.ErrSourceUnavailable)
	}
	return fmt.Errorf("%w: %w", ErrSidecarRejected, err)
}
