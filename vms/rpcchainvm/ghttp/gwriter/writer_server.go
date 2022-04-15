// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gwriter

import (
	"context"
	"io"

	writerpb "github.com/chain4travel/caminogo/proto/pb/io/writer"
)

var _ writerpb.WriterServer = &Server{}

// Server is an http.Handler that is managed over RPC.
type Server struct {
	writerpb.UnimplementedWriterServer
	writer io.Writer
}

// NewServer returns an http.Handler instance managed remotely
func NewServer(writer io.Writer) *Server {
	return &Server{writer: writer}
}

func (s *Server) Write(ctx context.Context, req *writerpb.WriteRequest) (*writerpb.WriteResponse, error) {
	n, err := s.writer.Write(req.Payload)
	resp := &writerpb.WriteResponse{
		Written: int32(n),
	}
	if err != nil {
		resp.Errored = true
		resp.Error = err.Error()
	}
	return resp, nil
}
