// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ block.Visitor = (*warpVerifier)(nil)

// warpVerifier handles the logic for verifying the warp signatures in P-Chain txs.
type warpVerifier struct{}

func (*warpVerifier) BanffAbortBlock(*block.BanffAbortBlock) error           { return nil }
func (*warpVerifier) BanffCommitBlock(*block.BanffCommitBlock) error         { return nil }
func (*warpVerifier) ApricotAbortBlock(*block.ApricotAbortBlock) error       { return nil }
func (*warpVerifier) ApricotCommitBlock(*block.ApricotCommitBlock) error     { return nil }
func (*warpVerifier) ApricotProposalBlock(*block.ApricotProposalBlock) error { return nil }
func (*warpVerifier) ApricotStandardBlock(*block.ApricotStandardBlock) error { return nil }
func (*warpVerifier) ApricotAtomicBlock(*block.ApricotAtomicBlock) error     { return nil }

func (v *warpVerifier) BanffProposalBlock(b *block.BanffProposalBlock) error {
	return v.verifyStandardTxs(b.Transactions)
}

func (v *warpVerifier) BanffStandardBlock(b *block.BanffStandardBlock) error {
	return v.verifyStandardTxs(b.Transactions)
}

func (*warpVerifier) verifyStandardTxs([]*txs.Tx) error {
	// If any tx contains a warp message, that must be checked here.
	return nil
}
