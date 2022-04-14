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

package gresponsewriter

import (
	"context"
	"errors"
	"net/http"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/chain4travel/caminogo/vms/rpcchainvm/ghttp/gconn"
	"github.com/chain4travel/caminogo/vms/rpcchainvm/ghttp/greader"
	"github.com/chain4travel/caminogo/vms/rpcchainvm/ghttp/gwriter"
	"github.com/chain4travel/caminogo/vms/rpcchainvm/grpcutils"

	responsewriterpb "github.com/chain4travel/caminogo/proto/pb/http/responsewriter"
	readerpb "github.com/chain4travel/caminogo/proto/pb/io/reader"
	writerpb "github.com/chain4travel/caminogo/proto/pb/io/writer"
	connpb "github.com/chain4travel/caminogo/proto/pb/net/conn"
)

var (
	errUnsupportedFlushing  = errors.New("response writer doesn't support flushing")
	errUnsupportedHijacking = errors.New("response writer doesn't support hijacking")

	_ responsewriterpb.WriterServer = &Server{}
)

// Server is an http.ResponseWriter that is managed over RPC.
type Server struct {
	responsewriterpb.UnimplementedWriterServer
	writer http.ResponseWriter
}

// NewServer returns an http.ResponseWriter instance managed remotely
func NewServer(writer http.ResponseWriter) *Server {
	return &Server{
		writer: writer,
	}
}

func (s *Server) Write(ctx context.Context, req *responsewriterpb.WriteRequest) (*responsewriterpb.WriteResponse, error) {
	headers := s.writer.Header()
	for key := range headers {
		delete(headers, key)
	}
	for _, header := range req.Headers {
		headers[header.Key] = header.Values
	}

	n, err := s.writer.Write(req.Payload)
	if err != nil {
		return nil, err
	}
	return &responsewriterpb.WriteResponse{
		Written: int32(n),
	}, nil
}

func (s *Server) WriteHeader(ctx context.Context, req *responsewriterpb.WriteHeaderRequest) (*emptypb.Empty, error) {
	headers := s.writer.Header()
	for key := range headers {
		delete(headers, key)
	}
	for _, header := range req.Headers {
		headers[header.Key] = header.Values
	}
	s.writer.WriteHeader(int(req.StatusCode))
	return &emptypb.Empty{}, nil
}

func (s *Server) Flush(ctx context.Context, req *emptypb.Empty) (*emptypb.Empty, error) {
	flusher, ok := s.writer.(http.Flusher)
	if !ok {
		return nil, errUnsupportedFlushing
	}
	flusher.Flush()
	return &emptypb.Empty{}, nil
}

func (s *Server) Hijack(ctx context.Context, req *emptypb.Empty) (*responsewriterpb.HijackResponse, error) {
	hijacker, ok := s.writer.(http.Hijacker)
	if !ok {
		return nil, errUnsupportedHijacking
	}
	conn, readWriter, err := hijacker.Hijack()
	if err != nil {
		return nil, err
	}

	serverListener, err := grpcutils.NewListener()
	if err != nil {
		return nil, err
	}
	serverAddr := serverListener.Addr().String()

	closer := grpcutils.ServerCloser{}
	go grpcutils.Serve(serverListener, func(opts []grpc.ServerOption) *grpc.Server {
		if len(opts) == 0 {
			opts = append(opts, grpcutils.DefaultServerOptions...)
		}
		server := grpc.NewServer(opts...)
		closer.Add(server)
		connpb.RegisterConnServer(server, gconn.NewServer(conn, &closer))
		readerpb.RegisterReaderServer(server, greader.NewServer(readWriter))
		writerpb.RegisterWriterServer(server, gwriter.NewServer(readWriter))
		return server
	})

	local := conn.LocalAddr()
	remote := conn.RemoteAddr()

	return &responsewriterpb.HijackResponse{
		LocalNetwork:  local.Network(),
		LocalString:   local.String(),
		RemoteNetwork: remote.Network(),
		RemoteString:  remote.String(),
		ServerAddr:    serverAddr,
	}, nil
}
