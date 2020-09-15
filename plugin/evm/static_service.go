// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"encoding/json"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/avalanchego/utils/formatting"
)

// StaticService defines the static API services exposed by the evm
type StaticService struct{}

// BuildGenesis returns the UTXOs such that at least one address in [args.Addresses] is
// referenced in the UTXO.
func (*StaticService) BuildGenesis(_ context.Context, args *core.Genesis) (formatting.CB58, error) {
	bytes, err := json.Marshal(args)
	return formatting.CB58{Bytes: bytes}, err
}
