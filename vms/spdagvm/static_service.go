// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spdagvm

import (
	"net/http"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/json"
)

// StaticService defines the static API exposed by the spdag VM
type StaticService struct{}

// APIOutput ...
type APIOutput struct {
	Amount     json.Uint64   `json:"amount"`
	Locktime   json.Uint64   `json:"locktime"`
	Threshold  json.Uint32   `json:"threshold"`
	Addresses  []ids.ShortID `json:"addresses"`
	Locktime2  json.Uint64   `json:"locktime2"`
	Threshold2 json.Uint32   `json:"threshold2"`
	Addresses2 []ids.ShortID `json:"addresses2"`
}

// BuildGenesisArgs are arguments for BuildGenesis
type BuildGenesisArgs struct {
	Outputs []APIOutput `json:"outputs"`
}

// BuildGenesisReply is the reply from BuildGenesis
type BuildGenesisReply struct {
	Bytes formatting.CB58 `json:"bytes"`
}

// BuildGenesis returns the UTXOs such that at least one address in [args.Addresses] is
// referenced in the UTXO.
func (*StaticService) BuildGenesis(_ *http.Request, args *BuildGenesisArgs, reply *BuildGenesisReply) error {
	builder := Builder{
		NetworkID: 0,
		ChainID:   ids.Empty,
	}
	outs := []Output{}
	for _, output := range args.Outputs {
		if output.Locktime2 == 0 && output.Threshold2 == 0 && len(output.Addresses2) == 0 {
			outs = append(outs, builder.NewOutputPayment(
				uint64(output.Amount),
				uint64(output.Locktime),
				uint32(output.Threshold),
				output.Addresses,
			))
		} else {
			outs = append(outs, builder.NewOutputTakeOrLeave(
				uint64(output.Amount),
				uint64(output.Locktime),
				uint32(output.Threshold),
				output.Addresses,
				uint64(output.Locktime2),
				uint32(output.Threshold2),
				output.Addresses2,
			))
		}
	}
	tx, err := builder.NewTx(
		/*ins=*/ nil,
		/*outs=*/ outs,
		/*signers=*/ nil,
	)
	if err := tx.verifyOuts(); err != nil {
		return err
	}

	reply.Bytes.Bytes = tx.Bytes()
	return err
}
