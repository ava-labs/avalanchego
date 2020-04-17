// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gwriter

import (
	"context"
	"io"

	"github.com/ava-labs/gecko/vms/rpcchainvm/ghttp/gwriter/proto"
)

// Server is a http.Handler that is managed over RPC.
type Server struct{ writer io.Writer }

// NewServer returns a http.Handler instance manage remotely
func NewServer(writer io.Writer) *Server {
	return &Server{writer: writer}
}

// Write ...
func (s *Server) Write(ctx context.Context, req *proto.WriteRequest) (*proto.WriteResponse, error) {
	n, err := s.writer.Write(req.Payload)
	resp := &proto.WriteResponse{
		Written: int32(n),
	}
	if err != nil {
		resp.Errored = true
		resp.Error = err.Error()
	}
	return resp, nil
}
