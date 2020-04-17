// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gresponsewriter

import (
	"context"
	"errors"
	"net/http"

	"github.com/ava-labs/gecko/vms/rpcchainvm/ghttp/gconn"
	"github.com/ava-labs/gecko/vms/rpcchainvm/ghttp/greader"
	"github.com/ava-labs/gecko/vms/rpcchainvm/ghttp/gwriter"

	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	connproto "github.com/ava-labs/gecko/vms/rpcchainvm/ghttp/gconn/proto"
	readerproto "github.com/ava-labs/gecko/vms/rpcchainvm/ghttp/greader/proto"
	responsewriterproto "github.com/ava-labs/gecko/vms/rpcchainvm/ghttp/gresponsewriter/proto"
	writerproto "github.com/ava-labs/gecko/vms/rpcchainvm/ghttp/gwriter/proto"
)

// Server is a http.Handler that is managed over RPC.
type Server struct {
	writer http.ResponseWriter
	broker *plugin.GRPCBroker
}

// NewServer returns a http.Handler instance manage remotely
func NewServer(writer http.ResponseWriter, broker *plugin.GRPCBroker) *Server {
	return &Server{
		writer: writer,
		broker: broker,
	}
}

// Write ...
func (s *Server) Write(ctx context.Context, req *responsewriterproto.WriteRequest) (*responsewriterproto.WriteResponse, error) {
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
	return &responsewriterproto.WriteResponse{
		Written: int32(n),
	}, nil
}

// WriteHeader ...
func (s *Server) WriteHeader(ctx context.Context, req *responsewriterproto.WriteHeaderRequest) (*responsewriterproto.WriteHeaderResponse, error) {
	headers := s.writer.Header()
	for key := range headers {
		delete(headers, key)
	}
	for _, header := range req.Headers {
		headers[header.Key] = header.Values
	}
	s.writer.WriteHeader(int(req.StatusCode))
	return &responsewriterproto.WriteHeaderResponse{}, nil
}

// Flush ...
func (s *Server) Flush(ctx context.Context, req *responsewriterproto.FlushRequest) (*responsewriterproto.FlushResponse, error) {
	flusher, ok := s.writer.(http.Flusher)
	if !ok {
		return nil, errors.New("response writer doesn't support flushing")
	}
	flusher.Flush()
	return &responsewriterproto.FlushResponse{}, nil
}

// Hijack ...
func (s *Server) Hijack(ctx context.Context, req *responsewriterproto.HijackRequest) (*responsewriterproto.HijackResponse, error) {
	hijacker, ok := s.writer.(http.Hijacker)
	if !ok {
		return nil, errors.New("response writer doesn't support hijacking")
	}
	conn, readWriter, err := hijacker.Hijack()
	if err != nil {
		return nil, err
	}

	connID := s.broker.NextId()
	readerID := s.broker.NextId()
	writerID := s.broker.NextId()

	go s.broker.AcceptAndServe(connID, func(opts []grpc.ServerOption) *grpc.Server {
		connServer := grpc.NewServer(opts...)
		connproto.RegisterConnServer(connServer, gconn.NewServer(conn))
		return connServer
	})
	go s.broker.AcceptAndServe(readerID, func(opts []grpc.ServerOption) *grpc.Server {
		readerServer := grpc.NewServer(opts...)
		readerproto.RegisterReaderServer(readerServer, greader.NewServer(readWriter))
		return readerServer
	})
	go s.broker.AcceptAndServe(writerID, func(opts []grpc.ServerOption) *grpc.Server {
		writerServer := grpc.NewServer(opts...)
		writerproto.RegisterWriterServer(writerServer, gwriter.NewServer(readWriter))
		return writerServer
	})

	local := conn.LocalAddr()
	remote := conn.RemoteAddr()

	return &responsewriterproto.HijackResponse{
		ConnServer:    connID,
		LocalNetwork:  local.Network(),
		LocalString:   local.String(),
		RemoteNetwork: remote.Network(),
		RemoteString:  remote.String(),
		ReaderServer:  readerID,
		WriterServer:  writerID,
	}, nil
}
