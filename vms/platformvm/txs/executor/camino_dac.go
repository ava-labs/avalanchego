// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"github.com/ava-labs/avalanchego/vms/platformvm/dac"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ dac.VerifierVisitor = (*proposalVerifier)(nil)
	_ dac.ExecutorVisitor = (*proposalExecutor)(nil)
)

type proposalVerifier struct {
	state               state.Chain
	fx                  fx.Fx
	signedAddProposalTx *txs.Tx
	addProposalTx       *txs.AddProposalTx
}

// Executor calls should never error.
// We should always mind possible proposals conflict, when implementing proposal execution logic.
// Because when proposal is semantically verified, state doesn't know about changes
// that already existing proposals will bring into state on their execution.
// And proposal execution is a system tx, so it should always succeed.
type proposalExecutor struct {
	state state.Chain
	fx    fx.Fx
}

func (e *CaminoStandardTxExecutor) proposalVerifier(tx *txs.AddProposalTx) *proposalVerifier {
	return &proposalVerifier{
		state:               e.State,
		fx:                  e.Fx,
		signedAddProposalTx: e.Tx,
		addProposalTx:       tx,
	}
}

func (e *CaminoStandardTxExecutor) proposalExecutor() *proposalExecutor {
	return &proposalExecutor{state: e.State, fx: e.Fx}
}
