// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package galiasreader

import (
	"context"

	"github.com/ava-labs/avalanchego/api/proto/galiasreaderproto"
	"github.com/ava-labs/avalanchego/ids"
)

var _ galiasreaderproto.AliasReaderServer = &Server{}

// Server enables alias lookups over RPC.
type Server struct {
	galiasreaderproto.UnimplementedAliasReaderServer
	aliaser ids.AliaserReader
}

// NewServer returns an alias lookup connected to a remote alias lookup
func NewServer(aliaser ids.AliaserReader) *Server {
	return &Server{aliaser: aliaser}
}

func (s *Server) Lookup(
	_ context.Context,
	req *galiasreaderproto.Alias,
) (*galiasreaderproto.ID, error) {
	id, err := s.aliaser.Lookup(req.Alias)
	if err != nil {
		return nil, err
	}
	return &galiasreaderproto.ID{
		Id: id[:],
	}, nil
}

func (s *Server) PrimaryAlias(
	_ context.Context,
	req *galiasreaderproto.ID,
) (*galiasreaderproto.Alias, error) {
	id, err := ids.ToID(req.Id)
	if err != nil {
		return nil, err
	}
	alias, err := s.aliaser.PrimaryAlias(id)
	return &galiasreaderproto.Alias{
		Alias: alias,
	}, err
}

func (s *Server) Aliases(
	_ context.Context,
	req *galiasreaderproto.ID,
) (*galiasreaderproto.AliasList, error) {
	id, err := ids.ToID(req.Id)
	if err != nil {
		return nil, err
	}
	aliases, err := s.aliaser.Aliases(id)
	return &galiasreaderproto.AliasList{
		Aliases: aliases,
	}, err
}
