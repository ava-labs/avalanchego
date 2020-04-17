// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gconn

import (
	"context"
	"net"
	"time"

	"github.com/ava-labs/gecko/vms/rpcchainvm/ghttp/gconn/proto"
)

// Server is a http.Handler that is managed over RPC.
type Server struct{ conn net.Conn }

// NewServer returns a http.Handler instance manage remotely
func NewServer(conn net.Conn) *Server {
	return &Server{conn: conn}
}

// Read ...
func (s *Server) Read(ctx context.Context, req *proto.ReadRequest) (*proto.ReadResponse, error) {
	buf := make([]byte, int(req.Length))
	n, err := s.conn.Read(buf)
	resp := &proto.ReadResponse{
		Read: buf[:n],
	}
	if err != nil {
		resp.Errored = true
		resp.Error = err.Error()
	}
	return resp, nil
}

// Write ...
func (s *Server) Write(ctx context.Context, req *proto.WriteRequest) (*proto.WriteResponse, error) {
	n, err := s.conn.Write(req.Payload)
	if err != nil {
		return nil, err
	}
	return &proto.WriteResponse{
		Length: int32(n),
	}, nil
}

// Close ...
func (s *Server) Close(ctx context.Context, req *proto.CloseRequest) (*proto.CloseResponse, error) {
	return &proto.CloseResponse{}, s.conn.Close()
}

// SetDeadline ...
func (s *Server) SetDeadline(ctx context.Context, req *proto.SetDeadlineRequest) (*proto.SetDeadlineResponse, error) {
	deadline := time.Time{}
	err := deadline.UnmarshalBinary(req.Time)
	if err != nil {
		return nil, err
	}
	return &proto.SetDeadlineResponse{}, s.conn.SetDeadline(deadline)
}

// SetReadDeadline ...
func (s *Server) SetReadDeadline(ctx context.Context, req *proto.SetReadDeadlineRequest) (*proto.SetReadDeadlineResponse, error) {
	deadline := time.Time{}
	err := deadline.UnmarshalBinary(req.Time)
	if err != nil {
		return nil, err
	}
	return &proto.SetReadDeadlineResponse{}, s.conn.SetReadDeadline(deadline)
}

// SetWriteDeadline ...
func (s *Server) SetWriteDeadline(ctx context.Context, req *proto.SetWriteDeadlineRequest) (*proto.SetWriteDeadlineResponse, error) {
	deadline := time.Time{}
	err := deadline.UnmarshalBinary(req.Time)
	if err != nil {
		return nil, err
	}
	return &proto.SetWriteDeadlineResponse{}, s.conn.SetWriteDeadline(deadline)
}
