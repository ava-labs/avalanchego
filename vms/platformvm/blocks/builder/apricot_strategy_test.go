// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateful"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/stretchr/testify/assert"
)

func TestApricotPickingOrder(t *testing.T) {
	assert := assert.New(t)

	// mock ResetBlockTimer to control timing of block formation
	h := newTestHelpersCollection(t, true /*mockResetBlockTimer*/)
	defer func() {
		if err := internalStateShutdown(h); err != nil {
			t.Fatal(err)
		}
	}()
	h.cfg.BlueberryTime = mockable.MaxTime // Blueberry not active yet

	chainTime := h.fullState.GetTimestamp()
	now := chainTime.Add(time.Second)
	h.clk.Set(now)

	nextChainTime := chainTime.Add(h.cfg.MinStakeDuration).Add(time.Hour)

	// create validator
	validatorStartTime := now.Add(time.Second)
	validatorTx, err := createTestValidatorTx(h, validatorStartTime, nextChainTime)
	assert.NoError(err)

	// accept validator as pending
	txExecutor := executor.ProposalTxExecutor{
		Backend:     &h.txExecBackend,
		ParentState: h.fullState,
		Tx:          validatorTx,
	}
	assert.NoError(validatorTx.Unsigned.Visit(&txExecutor))
	txExecutor.OnCommit.Apply(h.fullState)
	assert.NoError(h.fullState.Commit())

	// promote validator to current
	advanceTime, err := h.txBuilder.NewAdvanceTimeTx(validatorStartTime)
	assert.NoError(err)
	txExecutor.Tx = advanceTime
	assert.NoError(advanceTime.Unsigned.Visit(&txExecutor))
	txExecutor.OnCommit.Apply(h.fullState)
	assert.NoError(h.fullState.Commit())

	// move chain time to current validator's
	// end of staking time, so that it may be rewarded
	h.fullState.SetTimestamp(nextChainTime)
	now = nextChainTime
	h.clk.Set(now)

	// add decisionTx and stakerTxs to mempool
	decisionTxs, err := createTestDecisionTxes(2)
	assert.NoError(err)
	for _, dt := range decisionTxs {
		assert.NoError(h.mempool.Add(dt))
	}

	starkerTxStartTime := nextChainTime.Add(executor.SyncBound).Add(time.Second)
	stakerTx, err := createTestValidatorTx(h, starkerTxStartTime, starkerTxStartTime.Add(time.Hour))
	assert.NoError(err)
	assert.NoError(h.mempool.Add(stakerTx))

	// test: decisionTxs must be picked first
	blk, err := h.BlockBuilder.BuildBlock()
	assert.NoError(err)
	stdBlk, ok := blk.(*stateful.StandardBlock)
	assert.True(ok)
	assert.Equal(decisionTxs, stdBlk.DecisionTxs())
	assert.False(h.mempool.HasDecisionTxs())

	// test: reward validator blocks must follow, one per endingValidator
	blk, err = h.BlockBuilder.BuildBlock()
	assert.NoError(err)
	rewardBlk, ok := blk.(*stateful.ProposalBlock)
	assert.True(ok)
	rewardTx, ok := rewardBlk.ProposalTx().Unsigned.(*txs.RewardValidatorTx)
	assert.True(ok)
	assert.Equal(validatorTx.ID(), rewardTx.TxID)

	// accept reward validator tx so that current validator is removed
	assert.NoError(blk.Verify())
	assert.NoError(blk.Accept())
	options, err := blk.(snowman.OracleBlock).Options()
	assert.NoError(err)
	commitBlk := options[0]
	assert.NoError(commitBlk.Verify())
	assert.NoError(commitBlk.Accept())

	// finally mempool addValidatorTx must be picked
	blk, err = h.BlockBuilder.BuildBlock()
	assert.NoError(err)
	propBlk, ok := blk.(*stateful.ProposalBlock)
	assert.True(ok)
	assert.Equal(stakerTx, propBlk.ProposalTx())
}

func createTestDecisionTxes(count int) ([]*txs.Tx, error) {
	res := make([]*txs.Tx, 0, count)
	for i := uint32(0); i < uint32(count); i++ {
		utx := &txs.CreateChainTx{
			BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    10,
				BlockchainID: ids.Empty.Prefix(uint64(i)),
				Ins: []*avax.TransferableInput{{
					UTXOID: avax.UTXOID{
						TxID:        ids.ID{'t', 'x', 'I', 'D'},
						OutputIndex: i,
					},
					Asset: avax.Asset{ID: ids.ID{'a', 's', 's', 'e', 'r', 't'}},
					In: &secp256k1fx.TransferInput{
						Amt:   uint64(5678),
						Input: secp256k1fx.Input{SigIndices: []uint32{i}},
					},
				}},
				Outs: []*avax.TransferableOutput{{
					Asset: avax.Asset{ID: ids.ID{'a', 's', 's', 'e', 'r', 't'}},
					Out: &secp256k1fx.TransferOutput{
						Amt: uint64(1234),
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{preFundedKeys[0].PublicKey().Address()},
						},
					},
				}},
			}},
			SubnetID:    ids.GenerateTestID(),
			ChainName:   "chainName",
			VMID:        ids.GenerateTestID(),
			FxIDs:       []ids.ID{ids.GenerateTestID()},
			GenesisData: []byte{'g', 'e', 'n', 'D', 'a', 't', 'a'},
			SubnetAuth:  &secp256k1fx.Input{SigIndices: []uint32{1}},
		}

		tx, err := txs.NewSigned(utx, txs.Codec, nil)
		if err != nil {
			return nil, err
		}
		res = append(res, tx)
	}
	return res, nil
}

func createTestValidatorTx(h *testHelpersCollection, startTime, endTime time.Time) (*txs.Tx, error) {
	return h.txBuilder.NewAddValidatorTx(
		h.cfg.MinValidatorStake,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		h.ctx.NodeID,  // node ID
		ids.ShortID{}, // reward address
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0]},
		preFundedKeys[0].PublicKey().Address(), // change addr
	)
}
