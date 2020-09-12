// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package galiaslookup

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/galiaslookup/galiaslookupproto"
)

// Server is a messenger that is managed over RPC.
type Server struct {
	aliaser snow.AliasLookup
}

// NewServer returns a vm instance connected to a remote vm instance
func NewServer(aliaser snow.AliasLookup) *Server {
	return &Server{aliaser: aliaser}
}

// Lookup ...
func (s *Server) Lookup(
	_ context.Context,
	req *galiaslookupproto.LookupRequest,
) (*galiaslookupproto.LookupResponse, error) {
	id, err := s.aliaser.Lookup(req.Alias)
	if err != nil {
		return nil, err
	}
	return &galiaslookupproto.LookupResponse{
		Id: id.Bytes(),
	}, nil
}

// PrimaryAlias ...
func (s *Server) PrimaryAlias(
	_ context.Context,
	req *galiaslookupproto.PrimaryAliasRequest,
) (*galiaslookupproto.PrimaryAliasResponse, error) {
	id, err := ids.ToID(req.Id)
	if err != nil {
		return nil, err
	}
	alias, err := s.aliaser.PrimaryAlias(id)
	return &galiaslookupproto.PrimaryAliasResponse{
		Alias: alias,
	}, err
}
