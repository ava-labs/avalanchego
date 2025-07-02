// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gconn

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"time"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"

	connpb "github.com/ava-labs/avalanchego/proto/pb/net/conn"
)

var _ connpb.ConnServer = (*Server)(nil)

// Server is an http.Conn that is managed over RPC.
type Server struct {
	connpb.UnsafeConnServer
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

func (s *Server) Read(_ context.Context, req *connpb.ReadRequest) (*connpb.ReadResponse, error) {
	buf := make([]byte, int(req.Length))
	n, err := s.conn.Read(buf)
	resp := &connpb.ReadResponse{
		Read: buf[:n],
	}
	if err != nil {
		resp.Error = &connpb.Error{
			Message: err.Error(),
		}

		// Sentinel errors must be special-cased through an error code
		switch {
		case err == io.EOF:
			resp.Error.ErrorCode = connpb.ErrorCode_ERROR_CODE_EOF
		case errors.Is(err, os.ErrDeadlineExceeded):
			resp.Error.ErrorCode = connpb.ErrorCode_ERROR_CODE_OS_ERR_DEADLINE_EXCEEDED
		}
	}
	return resp, nil
}

func (s *Server) Write(_ context.Context, req *connpb.WriteRequest) (*connpb.WriteResponse, error) {
	n, err := s.conn.Write(req.Payload)
	if err != nil {
		return nil, err
	}
	return &connpb.WriteResponse{
		Length: int32(n),
	}, nil
}

func (s *Server) Close(context.Context, *emptypb.Empty) (*emptypb.Empty, error) {
	err := s.conn.Close()
	s.closer.Stop()
	return &emptypb.Empty{}, err
}

func (s *Server) SetDeadline(_ context.Context, req *connpb.SetDeadlineRequest) (*emptypb.Empty, error) {
	deadline := time.Time{}
	err := deadline.UnmarshalBinary(req.Time)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, s.conn.SetDeadline(deadline)
}

func (s *Server) SetReadDeadline(_ context.Context, req *connpb.SetDeadlineRequest) (*emptypb.Empty, error) {
	deadline := time.Time{}
	err := deadline.UnmarshalBinary(req.Time)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, s.conn.SetReadDeadline(deadline)
}

func (s *Server) SetWriteDeadline(_ context.Context, req *connpb.SetDeadlineRequest) (*emptypb.Empty, error) {
	deadline := time.Time{}
	err := deadline.UnmarshalBinary(req.Time)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, s.conn.SetWriteDeadline(deadline)
}
