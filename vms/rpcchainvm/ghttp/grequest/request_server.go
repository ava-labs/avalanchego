// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package grequest

import (
	"context"
	"io"

	"github.com/ava-labs/avalanchego/proto/pb/http/request"
)

var _ request.RequestServer = (*Server)(nil)

func NewServer(body io.Reader) *Server {
	return &Server{
		body: body,
	}
}

type Server struct {
	request.UnimplementedRequestServer
	body io.Reader
}

func (s *Server) Body(_ context.Context, bodyRequest *request.BodyRequest) (*request.BodyReply, error) {
	body := make([]byte, bodyRequest.N)
	n, err := s.body.Read(body)
	if err != nil {
		return nil, err
	}

	return &request.BodyReply{
		Body: body[:n],
	}, nil
}
