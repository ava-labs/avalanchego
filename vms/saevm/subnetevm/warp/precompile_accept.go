// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"fmt"

	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
)

// HandlePrecompileAccept invokes [precompileconfig.Accepter.Accept] for each log
// emitted to an address listed in [params.RulesExtra.AccepterPrecompiles].
func HandlePrecompileAccept(
	rules params.Rules,
	acceptCtx *precompileconfig.AcceptContext,
	receipts types.Receipts,
) error {
	rulesExtra := params.GetRulesExtra(rules)
	if len(rulesExtra.AccepterPrecompiles) == 0 {
		return nil
	}

	for _, receipt := range receipts {
		for logIdx, lg := range receipt.Logs {
			accepter, ok := rulesExtra.AccepterPrecompiles[lg.Address]
			if !ok {
				continue
			}
			if err := accepter.Accept(acceptCtx, lg.BlockHash, lg.BlockNumber, lg.TxHash, logIdx, lg.Topics, lg.Data); err != nil {
				return fmt.Errorf("precompile accept (tx %s, log %d): %w", lg.TxHash, logIdx, err)
			}
		}
	}
	return nil
}
