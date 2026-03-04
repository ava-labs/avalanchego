// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gresponsewriter

import (
	"context"
	"errors"
	"net/http"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/vms/rpcchainvm/ghttp/gconn"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/ghttp/greader"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/ghttp/gwriter"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"

	responsewriterpb "github.com/ava-labs/avalanchego/proto/pb/http/responsewriter"
	readerpb "github.com/ava-labs/avalanchego/proto/pb/io/reader"
	writerpb "github.com/ava-labs/avalanchego/proto/pb/io/writer"
	connpb "github.com/ava-labs/avalanchego/proto/pb/net/conn"
)

var (
	errUnsupportedFlushing  = errors.New("response writer doesn't support flushing")
	errUnsupportedHijacking = errors.New("response writer doesn't support hijacking")

	_ responsewriterpb.WriterServer = (*Server)(nil)
)

// Server is an http.ResponseWriter that is managed over RPC.
type Server struct {
	responsewriterpb.UnsafeWriterServer
	writer http.ResponseWriter
}

// NewServer returns an http.ResponseWriter instance managed remotely
func NewServer(writer http.ResponseWriter) *Server {
	return &Server{
		writer: writer,
	}
}

func (s *Server) Write(
	_ context.Context,
	req *responsewriterpb.WriteRequest,
) (*responsewriterpb.WriteResponse, error) {
	headers := s.writer.Header()
	clear(headers)
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

func (s *Server) WriteHeader(
	_ context.Context,
	req *responsewriterpb.WriteHeaderRequest,
) (*emptypb.Empty, error) {
	headers := s.writer.Header()
	clear(headers)
	for _, header := range req.Headers {
		headers[header.Key] = header.Values
	}
	s.writer.WriteHeader(grpcutils.EnsureValidResponseCode(int(req.StatusCode)))
	return &emptypb.Empty{}, nil
}

func (s *Server) Flush(context.Context, *emptypb.Empty) (*emptypb.Empty, error) {
	flusher, ok := s.writer.(http.Flusher)
	if !ok {
		return nil, errUnsupportedFlushing
	}
	flusher.Flush()
	return &emptypb.Empty{}, nil
}

func (s *Server) Hijack(context.Context, *emptypb.Empty) (*responsewriterpb.HijackResponse, error) {
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

	server := grpcutils.NewServer()
	closer := grpcutils.ServerCloser{}
	closer.Add(server)

	connpb.RegisterConnServer(server, gconn.NewServer(conn, &closer))
	readerpb.RegisterReaderServer(server, greader.NewServer(readWriter))
	writerpb.RegisterWriterServer(server, gwriter.NewServer(readWriter))

	go grpcutils.Serve(serverListener, server)

	local := conn.LocalAddr()
	remote := conn.RemoteAddr()

	return &responsewriterpb.HijackResponse{
		LocalNetwork:  local.Network(),
		LocalString:   local.String(),
		RemoteNetwork: remote.Network(),
		RemoteString:  remote.String(),
		ServerAddr:    serverListener.Addr().String(),
	}, nil
}
