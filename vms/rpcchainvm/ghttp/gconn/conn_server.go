// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gconn

import (
	"context"
	"net"
	"time"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"

	connpb "github.com/ava-labs/avalanchego/proto/pb/net/conn"
)

var _ connpb.ConnServer = &Server{}

// Server is an http.Conn that is managed over RPC.
type Server struct {
	connpb.UnimplementedConnServer
	conn   net.Conn
	closer *grpcutils.ServerCloser
}

// NewServer returns an http.Conn managed remotely
func NewServer(conn net.Conn, closer *grpcutils.ServerCloser) *Server {
	return &Server{
		conn:   conn,
		closer: closer,
	}
}

func (s *Server) Read(ctx context.Context, req *connpb.ReadRequest) (*connpb.ReadResponse, error) {
	buf := make([]byte, int(req.Length))
	n, err := s.conn.Read(buf)
	resp := &connpb.ReadResponse{
		Read: buf[:n],
	}
	if err != nil {
		resp.Errored = true
		resp.Error = err.Error()
	}
	return resp, nil
}

func (s *Server) Write(ctx context.Context, req *connpb.WriteRequest) (*connpb.WriteResponse, error) {
	n, err := s.conn.Write(req.Payload)
	if err != nil {
		return nil, err
	}
	return &connpb.WriteResponse{
		Length: int32(n),
	}, nil
}

func (s *Server) Close(ctx context.Context, req *emptypb.Empty) (*emptypb.Empty, error) {
	err := s.conn.Close()
	s.closer.Stop()
	return &emptypb.Empty{}, err
}

func (s *Server) SetDeadline(ctx context.Context, req *connpb.SetDeadlineRequest) (*emptypb.Empty, error) {
	deadline := time.Time{}
	err := deadline.UnmarshalBinary(req.Time)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, s.conn.SetDeadline(deadline)
}

func (s *Server) SetReadDeadline(ctx context.Context, req *connpb.SetDeadlineRequest) (*emptypb.Empty, error) {
	deadline := time.Time{}
	err := deadline.UnmarshalBinary(req.Time)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, s.conn.SetReadDeadline(deadline)
}

func (s *Server) SetWriteDeadline(ctx context.Context, req *connpb.SetDeadlineRequest) (*emptypb.Empty, error) {
	deadline := time.Time{}
	err := deadline.UnmarshalBinary(req.Time)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, s.conn.SetWriteDeadline(deadline)
}
