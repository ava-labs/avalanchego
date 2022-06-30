// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"encoding/json"

	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/coreth/core"
)

// StaticService defines the static API services exposed by the evm
type StaticService struct{}

// BuildGenesisReply is the reply from BuildGenesis
type BuildGenesisReply struct {
	Bytes    string              `json:"bytes"`
	Encoding formatting.Encoding `json:"encoding"`
}

// BuildGenesis returns the UTXOs such that at least one address in [args.Addresses] is
// referenced in the UTXO.
func (*StaticService) BuildGenesis(_ context.Context, args *core.Genesis) (*BuildGenesisReply, error) {
	bytes, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	bytesStr, err := formatting.Encode(formatting.Hex, bytes)
	if err != nil {
		return nil, err
	}
	return &BuildGenesisReply{
		Bytes:    bytesStr,
		Encoding: formatting.Hex,
	}, nil
}
