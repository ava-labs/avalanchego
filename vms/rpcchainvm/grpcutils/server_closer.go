// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package grpcutils

import (
	"sync"

	"google.golang.org/grpc"
)

type ServerCloser struct {
	lock    sync.Mutex
	closed  bool
	servers []*grpc.Server
}

func (s *ServerCloser) Add(server *grpc.Server) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.closed {
		server.Stop()
	} else {
		s.servers = append(s.servers, server)
	}
}

func (s *ServerCloser) Stop() {
	s.lock.Lock()
	defer s.lock.Unlock()

	for _, server := range s.servers {
		server.Stop()
	}
	s.closed = true
	s.servers = nil
}

func (s *ServerCloser) GracefulStop() {
	s.lock.Lock()
	defer s.lock.Unlock()

	for _, server := range s.servers {
		server.GracefulStop()
	}
	s.closed = true
	s.servers = nil
}
