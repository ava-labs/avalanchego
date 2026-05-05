// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package galiasreader

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"

	aliasreaderpb "github.com/ava-labs/avalanchego/proto/pb/aliasreader"
)

var _ aliasreaderpb.AliasReaderServer = (*Server)(nil)

// Server enables alias lookups over RPC.
type Server struct {
	aliasreaderpb.UnsafeAliasReaderServer

	aliaser ids.AliaserReader
}

// NewServer returns an alias lookup connected to a remote alias lookup
func NewServer(aliaser ids.AliaserReader) *Server {
	return &Server{aliaser: aliaser}
}

func (s *Server) Lookup(
	_ context.Context,
	req *aliasreaderpb.Alias,
) (*aliasreaderpb.ID, error) {
	id, err := s.aliaser.Lookup(req.Alias)
	if err != nil {
		return nil, err
	}
	return &aliasreaderpb.ID{
		Id: id[:],
	}, nil
}

func (s *Server) PrimaryAlias(
	_ context.Context,
	req *aliasreaderpb.ID,
) (*aliasreaderpb.Alias, error) {
	id, err := ids.ToID(req.Id)
	if err != nil {
		return nil, err
	}
	alias, err := s.aliaser.PrimaryAlias(id)
	return &aliasreaderpb.Alias{
		Alias: alias,
	}, err
}

func (s *Server) Aliases(
	_ context.Context,
	req *aliasreaderpb.ID,
) (*aliasreaderpb.AliasList, error) {
	id, err := ids.ToID(req.Id)
	if err != nil {
		return nil, err
	}
	aliases, err := s.aliaser.Aliases(id)
	return &aliasreaderpb.AliasList{
		Aliases: aliases,
	}, err
}
