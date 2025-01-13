// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"errors"
	"net/http"

	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/subnet-evm/core"
)

var (
	errNoGenesisData  = errors.New("no genesis data provided")
	errNoConfig       = errors.New("no config provided in genesis")
	errNoChainID      = errors.New("no chain ID provided in genesis config")
	errInvalidChainID = errors.New("chainID must be greater than 0")
	errNoAlloc        = errors.New("no alloc table provided in genesis")
)

// StaticService defines the static API services exposed by the evm
type StaticService struct{}

func CreateStaticService() *StaticService {
	return &StaticService{}
}

// BuildGenesisArgs are arguments for BuildGenesis
type BuildGenesisArgs struct {
	GenesisData *core.Genesis       `json:"genesisData"`
	Encoding    formatting.Encoding `json:"encoding"`
}

// BuildGenesisReply is the reply from BuildGenesis
type BuildGenesisReply struct {
	GenesisBytes string              `json:"genesisBytes"`
	Encoding     formatting.Encoding `json:"encoding"`
}

// BuildGenesis constructs a genesis file.
func (ss *StaticService) BuildGenesis(_ *http.Request, args *BuildGenesisArgs, reply *BuildGenesisReply) error {
	// check for critical fields first
	switch {
	case args.GenesisData == nil:
		return errNoGenesisData
	case args.GenesisData.Config == nil:
		return errNoConfig
	case args.GenesisData.Config.ChainID == nil:
		return errNoChainID
	case args.GenesisData.Config.ChainID.Cmp(common.Big1) == -1:
		return errInvalidChainID
	case args.GenesisData.Alloc == nil:
		return errNoAlloc
	}

	bytes, err := args.GenesisData.MarshalJSON()
	if err != nil {
		return err
	}
	bytesStr, err := formatting.Encode(args.Encoding, bytes)
	if err != nil {
		return err
	}
	reply.GenesisBytes = bytesStr
	reply.Encoding = args.Encoding
	return nil
}

// DecodeGenesisArgs are arguments for DecodeGenesis
type DecodeGenesisArgs struct {
	GenesisBytes string              `json:"genesisBytes"`
	Encoding     formatting.Encoding `json:"encoding"`
}

// DecodeGenesisReply is the reply from DecodeGenesis
type DecodeGenesisReply struct {
	Genesis  *core.Genesis       `json:"genesisData"`
	Encoding formatting.Encoding `json:"encoding"`
}

// BuildGenesis constructs a genesis file.
func (ss *StaticService) DecodeGenesis(_ *http.Request, args *DecodeGenesisArgs, reply *DecodeGenesisReply) error {
	bytesStr, err := formatting.Decode(args.Encoding, args.GenesisBytes)
	if err != nil {
		return err
	}
	genesis := core.Genesis{}
	if err := genesis.UnmarshalJSON(bytesStr); err != nil {
		return err
	}
	reply.Genesis = &genesis
	reply.Encoding = args.Encoding
	return nil
}
