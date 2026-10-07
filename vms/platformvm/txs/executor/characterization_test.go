// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

// This file pins the observable behavior of P-Chain tx verification (issue
// #5517): fork gates, the per-tx placement of the Bootstrapped
// short-circuit, and flow-check error identity.
//
// If a test in this file fails after a change, either the change is not
// behavior-preserving (fix the change), or the behavior change is
// deliberate, in which case the updated expectations in this file are the
// audit trail of that change and must be called out in the PR description.

import (
	"errors"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// charForks are the forks at which P-Chain executor gating changes. Forks
// not listed (Cortina, Fortuna, Granite, ...) introduce no executor-level
// gates.
var charForks = []upgradetest.Fork{
	upgradetest.ApricotPhase5,
	upgradetest.Banff,
	upgradetest.Durango,
	upgradetest.Etna,
	upgradetest.Helicon,
}

// executeStandardCharTx runs tx through the StandardTx entry point on a
// fresh diff off the last accepted state.
func executeStandardCharTx(t *testing.T, env *environment, tx *platform.Tx) error {
	t.Helper()

	onAcceptState, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(t, err)

	feeCalculator := state.PickFeeCalculator(env.config, onAcceptState)
	_, _, _, err = StandardTx(
		&env.backend,
		feeCalculator,
		tx,
		onAcceptState,
	)
	return err
}

// executeProposalCharTx runs tx through the ProposalTx entry point on
// fresh commit/abort diffs off the last accepted state.
func executeProposalCharTx(t *testing.T, env *environment, tx *platform.Tx) error {
	t.Helper()

	onCommitState, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(t, err)
	onAbortState, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(t, err)

	feeCalculator := state.PickFeeCalculator(env.config, onCommitState)
	return ProposalTx(
		&env.backend,
		feeCalculator,
		tx,
		onCommitState,
		onAbortState,
	)
}

// TestCharacterizationForkGates pins every executor-level upgrade gate: the
// exact sentinel returned at forks where a tx type is disabled, and that the
// same sentinel is NOT returned at forks where it is enabled.
//
// The txs used here are minimal (mostly zero-valued) because every gate
// under test fires before syntactic verification; at non-gated forks the txs
// fail later for unrelated reasons, which is fine — the assertion there is
// only that the gate sentinel is not returned.
func TestCharacterizationForkGates(t *testing.T) {
	type gateTest struct {
		txType      string // platform.TxVisitor method name
		buildTx     func(t *testing.T, env *environment) *platform.Tx
		execute     func(t *testing.T, env *environment, tx *platform.Tx) error
		proposal    bool // executed via ProposalTx; used for subtest naming
		wantGateErr error
		gatedForks  []upgradetest.Fork
	}

	minimal := func(unsigned platform.UnsignedTx) func(*testing.T, *environment) *platform.Tx {
		return func(*testing.T, *environment) *platform.Tx {
			return &platform.Tx{Unsigned: unsigned}
		}
	}

	preDurango := []upgradetest.Fork{upgradetest.ApricotPhase5, upgradetest.Banff}
	postDurango := []upgradetest.Fork{upgradetest.Durango, upgradetest.Etna, upgradetest.Helicon}
	preEtna := []upgradetest.Fork{upgradetest.ApricotPhase5, upgradetest.Banff, upgradetest.Durango}
	postEtna := []upgradetest.Fork{upgradetest.Etna, upgradetest.Helicon}
	preHelicon := []upgradetest.Fork{upgradetest.ApricotPhase5, upgradetest.Banff, upgradetest.Durango, upgradetest.Etna}
	postBanff := []upgradetest.Fork{upgradetest.Banff, upgradetest.Durango, upgradetest.Etna, upgradetest.Helicon}

	tests := []gateTest{
		// Pre-Durango staker txs are disabled post-Durango.
		{
			txType:      "AddValidatorTx",
			buildTx:     minimal(&platform.AddValidatorTx{}),
			execute:     executeStandardCharTx,
			wantGateErr: durango.errDeprecated,
			gatedForks:  postDurango,
		},
		{
			txType:      "AddDelegatorTx",
			buildTx:     minimal(&platform.AddDelegatorTx{}),
			execute:     executeStandardCharTx,
			wantGateErr: durango.errDeprecated,
			gatedForks:  postDurango,
		},
		// Durango txs are disabled pre-Durango.
		{
			txType:      "BaseTx",
			buildTx:     minimal(&platform.BaseTx{}),
			execute:     executeStandardCharTx,
			wantGateErr: durango.errNotActive,
			gatedForks:  preDurango,
		},
		{
			txType:      "TransferSubnetOwnershipTx",
			buildTx:     minimal(&platform.TransferSubnetOwnershipTx{}),
			execute:     executeStandardCharTx,
			wantGateErr: durango.errNotActive,
			gatedForks:  preDurango,
		},
		// TransformSubnetTx is disabled post-Etna.
		{
			txType:      "TransformSubnetTx",
			buildTx:     minimal(&platform.TransformSubnetTx{}),
			execute:     executeStandardCharTx,
			wantGateErr: etna.errDeprecated,
			gatedForks:  postEtna,
		},
		// L1 txs are disabled pre-Etna.
		{
			txType:      "ConvertSubnetToL1Tx",
			buildTx:     minimal(&platform.ConvertSubnetToL1Tx{}),
			execute:     executeStandardCharTx,
			wantGateErr: etna.errNotActive,
			gatedForks:  preEtna,
		},
		{
			txType:      "RegisterL1ValidatorTx",
			buildTx:     minimal(&platform.RegisterL1ValidatorTx{}),
			execute:     executeStandardCharTx,
			wantGateErr: etna.errNotActive,
			gatedForks:  preEtna,
		},
		{
			txType:      "SetL1ValidatorWeightTx",
			buildTx:     minimal(&platform.SetL1ValidatorWeightTx{}),
			execute:     executeStandardCharTx,
			wantGateErr: etna.errNotActive,
			gatedForks:  preEtna,
		},
		{
			txType:      "IncreaseL1ValidatorBalanceTx",
			buildTx:     minimal(&platform.IncreaseL1ValidatorBalanceTx{}),
			execute:     executeStandardCharTx,
			wantGateErr: etna.errNotActive,
			gatedForks:  preEtna,
		},
		{
			txType:      "DisableL1ValidatorTx",
			buildTx:     minimal(&platform.DisableL1ValidatorTx{}),
			execute:     executeStandardCharTx,
			wantGateErr: etna.errNotActive,
			gatedForks:  preEtna,
		},
		// Auto-renewed validator txs are disabled pre-Helicon.
		{
			txType:      "AddAutoRenewedValidatorTx",
			buildTx:     minimal(&platform.AddAutoRenewedValidatorTx{}),
			execute:     executeStandardCharTx,
			wantGateErr: helicon.errNotActive,
			gatedForks:  preHelicon,
		},
		{
			txType:      "SetAutoRenewedValidatorConfigTx",
			buildTx:     minimal(&platform.SetAutoRenewedValidatorConfigTx{}),
			execute:     executeStandardCharTx,
			wantGateErr: helicon.errNotActive,
			gatedForks:  preHelicon,
		},
		{
			txType:      "RewardAutoRenewedValidatorTx",
			buildTx:     minimal(&platform.RewardAutoRenewedValidatorTx{}),
			execute:     executeProposalCharTx,
			proposal:    true,
			wantGateErr: helicon.errNotActive,
			gatedForks:  preHelicon,
		},
		// Staker txs are also rejected by the PROPOSAL executor post-Banff.
		// They are not listed here: that gate fires inside the execution
		// handlers, after the shared verification pipeline, so the minimal
		// (uninitialized) txs used by this table fail verification before
		// reaching it. The pre-Banff proposal window can no longer occur on
		// mainnet.
		// AdvanceTimeTx is rejected once the proposed time is >= the Banff
		// activation time. The advanced-to time (not the chain time) is what
		// is gated, so the tx must carry a real timestamp.
		{
			txType: "AdvanceTimeTx",
			buildTx: func(t *testing.T, env *environment) *platform.Tx {
				tx := newAdvanceTimeTx(t, env.clk.Time())
				return tx
			},
			execute:     executeProposalCharTx,
			proposal:    true,
			wantGateErr: ErrAdvanceTimeTxIssuedAfterBanff,
			gatedForks:  postBanff,
		},
	}

	gated := func(tt gateTest, fork upgradetest.Fork) bool {
		for _, f := range tt.gatedForks {
			if f == fork {
				return true
			}
		}
		return false
	}

	for _, tt := range tests {
		entryName := "standard"
		if tt.proposal {
			entryName = "proposal"
		}
		for _, fork := range charForks {
			t.Run(tt.txType+"/"+entryName+"/"+fork.String(), func(t *testing.T) {
				env := newEnvironment(t, fork)
				env.ctx.Lock.Lock()
				defer env.ctx.Lock.Unlock()

				tx := tt.buildTx(t, env)
				err := tt.execute(t, env, tx)
				if gated(tt, fork) {
					require.ErrorIs(t, err, tt.wantGateErr)
				} else {
					require.NotErrorIs(t, err, tt.wantGateErr)
				}
			})
		}
	}
}

// TestCharacterizationWrongTxType pins that txs dispatched to the wrong
// executor fail with errWrongTxType. The executor dispatches on the tx type
// before running verifyTx, so even txs that would fail verifyTx report the
// wrong executor.
func TestCharacterizationWrongTxType(t *testing.T) {
	tests := []struct {
		name    string
		buildTx func(t *testing.T, env *environment) *platform.Tx
		execute func(t *testing.T, env *environment, tx *platform.Tx) error
	}{
		{
			name: "AdvanceTimeTx_via_StandardTx",
			buildTx: func(t *testing.T, _ *environment) *platform.Tx {
				tx := &platform.Tx{Unsigned: &platform.AdvanceTimeTx{}}
				require.NoError(t, tx.Initialize(platform.Codec))
				return tx
			},
			execute: executeStandardCharTx,
		},
		{
			name: "RewardValidatorTx_via_StandardTx",
			buildTx: func(t *testing.T, _ *environment) *platform.Tx {
				tx := &platform.Tx{Unsigned: &platform.RewardValidatorTx{TxID: ids.GenerateTestID()}}
				require.NoError(t, tx.Initialize(platform.Codec))
				return tx
			},
			execute: executeStandardCharTx,
		},
		{
			name: "CreateSubnetTx_via_ProposalTx",
			buildTx: func(t *testing.T, env *environment) *platform.Tx {
				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueCreateSubnetTx(&secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{ids.ShortEmpty},
				})
				require.NoError(t, err)
				return tx
			},
			execute: executeProposalCharTx,
		},
		{
			name: "BaseTx_via_ProposalTx",
			buildTx: func(t *testing.T, env *environment) *platform.Tx {
				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueBaseTx([]*avax.TransferableOutput{{
					Asset: avax.Asset{ID: env.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{ids.ShortEmpty},
						},
					},
				}})
				require.NoError(t, err)
				return tx
			},
			execute: executeProposalCharTx,
		},
		{
			name: "uninitialized_RewardValidatorTx_via_StandardTx",
			buildTx: func(*testing.T, *environment) *platform.Tx {
				return &platform.Tx{Unsigned: &platform.RewardValidatorTx{TxID: ids.GenerateTestID()}}
			},
			execute: executeStandardCharTx,
		},
		{
			name: "uninitialized_BaseTx_via_ProposalTx",
			buildTx: func(*testing.T, *environment) *platform.Tx {
				return &platform.Tx{Unsigned: &platform.BaseTx{}}
			},
			execute: executeProposalCharTx,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newEnvironment(t, upgradetest.Latest)
			env.ctx.Lock.Lock()
			defer env.ctx.Lock.Unlock()

			err := tt.execute(t, env, tt.buildTx(t, env))
			require.ErrorIs(t, err, errWrongTxType)
		})
	}
}

// deleteInputUTXOs removes every UTXO consumed by tx from diff, so that
// the flow check MUST fail with database.ErrNotFound if (and only if) it
// runs against diff.
func deleteInputUTXOs(diff *state.Diff, tx *platform.Tx) {
	for inputID := range tx.Unsigned.InputIDs() {
		diff.DeleteUTXO(inputID)
	}
}

// findPrimaryValidator returns the genesis primary-network validator from
// env's state.
func findPrimaryValidator(t *testing.T, env *environment) *state.Staker {
	t.Helper()

	staker, err := env.state.GetCurrentValidator(constants.PrimaryNetworkID, genesistest.DefaultNodeIDs[0])
	require.NoError(t, err)
	return staker
}

// charUnfundedInput returns an input referencing a UTXO that does not exist
// in state.
func charUnfundedInput(env *environment) *avax.TransferableInput {
	return &avax.TransferableInput{
		UTXOID: avax.UTXOID{
			TxID: ids.GenerateTestID(),
		},
		Asset: avax.Asset{
			ID: env.ctx.AVAXAssetID,
		},
		In: &secp256k1fx.TransferInput{
			Amt: 1,
			Input: secp256k1fx.Input{
				SigIndices: []uint32{0},
			},
		},
	}
}

// charInvalidateSubnetAuth replaces the subnet-auth credential of tx, which
// is always its last credential, with an invalid signature.
func charInvalidateSubnetAuth(t *testing.T, _ *environment, tx *platform.Tx) {
	numCreds := len(tx.Creds)
	require.NotZero(t, numCreds)
	tx.Creds[numCreds-1] = &secp256k1fx.Credential{Sigs: make([][65]byte, 1)}
}

// charRegisterL1ValidatorTx records a conversion of testSubnet1 to an L1 in
// diff and returns a wallet-issued RegisterL1ValidatorTx whose warp message
// registers a validator with publicKey on that L1. The warp signature is left
// empty: it is verified by block verification, not by the tx executor.
func charRegisterL1ValidatorTx(
	t *testing.T,
	env *environment,
	diff *state.Diff,
	publicKey [bls.PublicKeyLen]byte,
	proofOfPossession [bls.SignatureLen]byte,
) *platform.Tx {
	var (
		subnetID = testSubnet1.ID()
		chainID  = ids.GenerateTestID()
		address  = []byte{'a', 'd', 'd', 'r'}
	)
	diff.SetSubnetToL1Conversion(subnetID, state.SubnetToL1Conversion{
		ChainID: chainID,
		Addr:    address,
	})

	registerPayload := must[*message.RegisterL1Validator](t)(message.NewRegisterL1Validator(
		subnetID,
		ids.GenerateTestNodeID(),
		publicKey,
		uint64(env.state.GetTimestamp().Add(5*time.Minute).Unix()), // expiry
		message.PChainOwner{},
		message.PChainOwner{},
		1, // weight
	))
	warpMessage := newWarpMessageBytes(t, env, chainID, address, &warp.BitSetSignature{}, registerPayload.Bytes())

	wallet := newWallet(t, env, walletConfig{})
	tx, err := wallet.IssueRegisterL1ValidatorTx(
		0, // balance: the test environment has no validator fee capacity
		proofOfPossession,
		warpMessage,
	)
	require.NoError(t, err)
	return tx
}

// charBaseTx returns a hand-built BaseTx that passes syntactic verification
// but references UTXOs that do not exist in state.
func charBaseTx(env *environment) platform.BaseTx {
	return platform.BaseTx{
		BaseTx: avax.BaseTx{
			NetworkID:    env.ctx.NetworkID,
			BlockchainID: env.ctx.ChainID,
			Ins: []*avax.TransferableInput{
				charUnfundedInput(env),
			},
			Outs: []*avax.TransferableOutput{{
				Asset: avax.Asset{
					ID: env.ctx.AVAXAssetID,
				},
				Out: &secp256k1fx.TransferOutput{
					Amt:          1,
					OutputOwners: *newOwner(),
				},
			}},
		},
	}
}

// charSignedTx wraps unsigned with numCreds placeholder credentials and
// initializes the tx.
func charSignedTx(t *testing.T, unsigned platform.UnsignedTx, numCreds int) *platform.Tx {
	t.Helper()

	creds := make([]verify.Verifiable, numCreds)
	for i := range creds {
		creds[i] = &secp256k1fx.Credential{
			Sigs: make([][65]byte, 1),
		}
	}
	tx := &platform.Tx{
		Unsigned: unsigned,
		Creds:    creds,
	}
	require.NoError(t, tx.Initialize(platform.Codec))
	return tx
}

// charFlowTest characterizes one tx type's behavior when the UTXOs it
// consumes are missing from state: whether the failure is skipped while
// bootstrapping (pinning the per-tx placement of the Bootstrapped
// short-circuit) and the exact identity of the flow-check error (pinning the
// per-file error-wrapping drift).
type charFlowTest struct {
	name     string
	txType   string // platform.TxVisitor method name, for coverage accounting
	fork     upgradetest.Fork
	proposal bool // execute via ProposalTx instead of StandardTx

	// buildTx returns a fully valid tx. It may execute setup txs against
	// diff or commit setup state to env.state.
	buildTx func(t *testing.T, env *environment, diff *state.Diff) *platform.Tx

	// skipValid skips the valid-tx baseline for rows whose builder
	// intentionally produces an invalid tx (their happy path is covered by
	// dedicated tests elsewhere in this package).
	skipValid bool

	// updateTx, if set, corrupts the valid tx before execution. Used where
	// deleting the consumed UTXOs is not enough — e.g. pre-Etna fee-only
	// txs are built with NO inputs at all (fees are zero), so an unfunded
	// input must be injected for the flow check to have anything to fail
	// on.
	updateTx func(t *testing.T, env *environment, tx *platform.Tx)

	// Expected error (via errors.Is) with the consumed UTXOs deleted:
	wantNotBootstrappedErr error // when !Bootstrapped
	wantBootstrappedErr    error // when Bootstrapped

	// wantWrapsFlowCheck is whether the returned error is wrapped with
	// errFlowCheckFailed: true whenever the failure IS the flow check,
	// false when it is a state lookup or authorization check that runs
	// before the flow check.
	wantWrapsFlowCheck bool
}

// charFlowTests is the per-tx-type characterization table. THE EXPECTATIONS
// ENCODE CURRENT BEHAVIOR, INCONSISTENCIES INCLUDED — see the file header.
func charFlowTests() []charFlowTest {
	rewardsOwner := newOwner()

	return []charFlowTest{
		{
			// Bootstrap short-circuit placement: right after verifyTx, before
			// the staking rules, duplicate-validator check, and flow check.
			name:   "AddValidatorTx",
			txType: "AddValidatorTx",
			fork:   upgradetest.Cortina,
			buildTx: func(t *testing.T, env *environment, _ *state.Diff) *platform.Tx {
				chainTime := env.state.GetTimestamp()
				startTime := chainTime.Add(time.Hour)
				endTime := startTime.Add(defaultMinStakingDuration + 2*time.Hour)

				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueAddValidatorTx(
					&platform.Validator{
						NodeID: ids.GenerateTestNodeID(),
						Start:  uint64(startTime.Unix()),
						End:    uint64(endTime.Unix()),
						Wght:   env.config.MinValidatorStake,
					},
					rewardsOwner,
					reward.PercentDenominator,
				)
				require.NoError(t, err)
				return tx
			},
			wantNotBootstrappedErr: nil,
			wantBootstrappedErr:    database.ErrNotFound,
			wantWrapsFlowCheck:     true,
		},
		{
			// Same tx and expectations through the pre-Banff proposal path,
			// which reuses verifyAddValidatorTx against onCommitState.
			name:     "AddValidatorTx_pre-Banff_proposal",
			txType:   "AddValidatorTx",
			fork:     upgradetest.ApricotPhase5,
			proposal: true,
			buildTx: func(t *testing.T, env *environment, _ *state.Diff) *platform.Tx {
				chainTime := env.state.GetTimestamp()
				startTime := chainTime.Add(time.Hour)
				endTime := startTime.Add(defaultMinStakingDuration + 2*time.Hour)

				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueAddValidatorTx(
					&platform.Validator{
						NodeID: ids.GenerateTestNodeID(),
						Start:  uint64(startTime.Unix()),
						End:    uint64(endTime.Unix()),
						Wght:   env.config.MinValidatorStake,
					},
					rewardsOwner,
					reward.PercentDenominator,
				)
				require.NoError(t, err)
				return tx
			},
			wantNotBootstrappedErr: nil,
			wantBootstrappedErr:    database.ErrNotFound,
			wantWrapsFlowCheck:     true,
		},
		{
			// Bootstrap short-circuit placement: right after verifyTx, before
			// the duration checks, duplicate-validator check, subnet auth,
			// and flow check.
			name:   "AddSubnetValidatorTx",
			txType: "AddSubnetValidatorTx",
			fork:   upgradetest.Durango,
			buildTx: func(t *testing.T, env *environment, _ *state.Diff) *platform.Tx {
				primaryValidator := findPrimaryValidator(t, env)

				subnetID := testSubnet1.ID()
				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueAddSubnetValidatorTx(
					&platform.SubnetValidator{
						Validator: platform.Validator{
							NodeID: primaryValidator.NodeID,
							Start:  0,
							End:    uint64(primaryValidator.EndTime.Unix()),
							Wght:   genesistest.DefaultValidatorWeight,
						},
						Subnet: subnetID,
					},
				)
				require.NoError(t, err)
				return tx
			},
			wantNotBootstrappedErr: nil,
			wantBootstrappedErr:    database.ErrNotFound,
			wantWrapsFlowCheck:     true,
		},
		{
			// Bootstrap short-circuit placement: right after verifyTx, before
			// the staking rules, validator lookup, and delegation-limit
			// checks.
			name:   "AddDelegatorTx",
			txType: "AddDelegatorTx",
			fork:   upgradetest.Cortina,
			buildTx: func(t *testing.T, env *environment, _ *state.Diff) *platform.Tx {
				primaryValidator := findPrimaryValidator(t, env)

				chainTime := env.state.GetTimestamp()
				startTime := chainTime.Add(time.Hour)
				endTime := startTime.Add(defaultMinStakingDuration + 2*time.Hour)

				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueAddDelegatorTx(
					&platform.Validator{
						NodeID: primaryValidator.NodeID,
						Start:  uint64(startTime.Unix()),
						End:    uint64(endTime.Unix()),
						Wght:   env.config.MinDelegatorStake,
					},
					rewardsOwner,
				)
				require.NoError(t, err)
				return tx
			},
			wantNotBootstrappedErr: nil,
			wantBootstrappedErr:    database.ErrNotFound,
			wantWrapsFlowCheck:     true,
		},
		{
			// Bootstrap short-circuit placement: right after verifyTx, before
			// the staking rules and duration checks, like every staker tx.
			name:   "AddPermissionlessValidatorTx",
			txType: "AddPermissionlessValidatorTx",
			fork:   upgradetest.Latest,
			buildTx: func(t *testing.T, env *environment, _ *state.Diff) *platform.Tx {
				chainTime := env.state.GetTimestamp()
				endTime := chainTime.Add(defaultMaxStakingDuration)

				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueAddPermissionlessValidatorTx(
					&platform.SubnetValidator{
						Validator: platform.Validator{
							NodeID: ids.GenerateTestNodeID(),
							Start:  0,
							End:    uint64(endTime.Unix()),
							Wght:   env.config.MinValidatorStake,
						},
						Subnet: constants.PrimaryNetworkID,
					},
					newProofOfPossession(t),
					env.ctx.AVAXAssetID,
					rewardsOwner,
					rewardsOwner,
					reward.PercentDenominator,
				)
				require.NoError(t, err)
				return tx
			},
			wantNotBootstrappedErr: nil,
			wantBootstrappedErr:    database.ErrNotFound,
			wantWrapsFlowCheck:     true,
		},
		{
			// Bootstrap short-circuit placement: EARLY, like the
			// permissionless validator.
			name:   "AddPermissionlessDelegatorTx",
			txType: "AddPermissionlessDelegatorTx",
			fork:   upgradetest.Latest,
			buildTx: func(t *testing.T, env *environment, _ *state.Diff) *platform.Tx {
				primaryValidator := findPrimaryValidator(t, env)

				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueAddPermissionlessDelegatorTx(
					&platform.SubnetValidator{
						Validator: platform.Validator{
							NodeID: primaryValidator.NodeID,
							Start:  0,
							End:    uint64(primaryValidator.EndTime.Unix()),
							Wght:   env.config.MinDelegatorStake,
						},
						Subnet: constants.PrimaryNetworkID,
					},
					env.ctx.AVAXAssetID,
					rewardsOwner,
				)
				require.NoError(t, err)
				return tx
			},
			wantNotBootstrappedErr: nil,
			wantBootstrappedErr:    database.ErrNotFound,
			wantWrapsFlowCheck:     true,
		},
		{
			// Bootstrap short-circuit placement: AFTER the validator lookup,
			// before subnet auth and the flow check.
			name:   "RemoveSubnetValidatorTx",
			txType: "RemoveSubnetValidatorTx",
			fork:   upgradetest.Latest,
			buildTx: func(t *testing.T, env *environment, diff *state.Diff) *platform.Tx {
				primaryValidator := findPrimaryValidator(t, env)

				subnetID := testSubnet1.ID()
				wallet := newWallet(t, env, walletConfig{})
				subnetValTx, err := wallet.IssueAddSubnetValidatorTx(
					&platform.SubnetValidator{
						Validator: platform.Validator{
							NodeID: primaryValidator.NodeID,
							Start:  0,
							End:    uint64(primaryValidator.EndTime.Unix()),
							Wght:   genesistest.DefaultValidatorWeight,
						},
						Subnet: subnetID,
					},
				)
				require.NoError(t, err)

				feeCalculator := state.PickFeeCalculator(env.config, diff)
				_, _, _, err = StandardTx(&env.backend, feeCalculator, subnetValTx, diff)
				require.NoError(t, err)

				tx, err := wallet.IssueRemoveSubnetValidatorTx(
					primaryValidator.NodeID,
					subnetID,
				)
				require.NoError(t, err)
				return tx
			},
			wantNotBootstrappedErr: nil,
			wantBootstrappedErr:    database.ErrNotFound,
			wantWrapsFlowCheck:     true,
		},
		{
			// The validator lookup happens BEFORE the bootstrap
			// short-circuit: a tx for a nonexistent validator fails with
			// errNotValidator even while bootstrapping.
			name:   "RemoveSubnetValidatorTx_missing_validator",
			txType: "RemoveSubnetValidatorTx",
			fork:   upgradetest.Latest,
			buildTx: func(t *testing.T, env *environment, _ *state.Diff) *platform.Tx {
				return charSignedTx(t, &platform.RemoveSubnetValidatorTx{
					BaseTx: charBaseTx(env),
					Subnet: ids.GenerateTestID(),
					NodeID: ids.GenerateTestNodeID(),
					SubnetAuth: &secp256k1fx.Input{
						SigIndices: []uint32{0},
					},
				}, 2)
			},
			skipValid:              true,
			wantNotBootstrappedErr: errNotValidator,
			wantBootstrappedErr:    errNotValidator,
			wantWrapsFlowCheck:     false,
		},
		{
			// Bootstrap short-circuit placement: after syntactic and memo
			// checks, before subnet auth and the flow check.
			name:   "TransferSubnetOwnershipTx",
			txType: "TransferSubnetOwnershipTx",
			fork:   upgradetest.Latest,
			buildTx: func(t *testing.T, env *environment, _ *state.Diff) *platform.Tx {
				subnetID := testSubnet1.ID()
				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueTransferSubnetOwnershipTx(
					subnetID,
					rewardsOwner,
				)
				require.NoError(t, err)
				return tx
			},
			wantNotBootstrappedErr: nil,
			wantBootstrappedErr:    database.ErrNotFound,
			wantWrapsFlowCheck:     true,
		},
		{
			// No semantic checks: the flow check, performed during execution
			// by applySpend, is skipped while bootstrapping.
			name:   "BaseTx",
			txType: "BaseTx",
			fork:   upgradetest.Latest,
			buildTx: func(t *testing.T, env *environment, _ *state.Diff) *platform.Tx {
				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueBaseTx([]*avax.TransferableOutput{{
					Asset: avax.Asset{ID: env.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{ids.ShortEmpty},
						},
					},
				}})
				require.NoError(t, err)
				return tx
			},
			wantNotBootstrappedErr: nil,
			wantBootstrappedErr:    database.ErrNotFound,
			wantWrapsFlowCheck:     true,
		},
		{
			// Subnet auth and the flow check, performed during execution by
			// applySpend, are both skipped while bootstrapping.
			name:   "CreateChainTx",
			txType: "CreateChainTx",
			fork:   upgradetest.Latest,
			buildTx: func(t *testing.T, env *environment, _ *state.Diff) *platform.Tx {
				subnetID := testSubnet1.ID()
				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueCreateChainTx(
					subnetID,
					[]byte{},
					ids.GenerateTestID(),
					[]ids.ID{},
					"chain name",
				)
				require.NoError(t, err)
				return tx
			},
			wantNotBootstrappedErr: nil,
			wantBootstrappedErr:    database.ErrNotFound,
			wantWrapsFlowCheck:     true,
		},
		{
			// Subnet auth runs BEFORE the flow check and is skipped while
			// bootstrapping.
			name:   "CreateChainTx_invalid_subnet_auth",
			txType: "CreateChainTx",
			fork:   upgradetest.Latest,
			buildTx: func(t *testing.T, env *environment, _ *state.Diff) *platform.Tx {
				subnetID := testSubnet1.ID()
				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueCreateChainTx(
					subnetID,
					[]byte{},
					ids.GenerateTestID(),
					[]ids.ID{},
					"chain name",
				)
				require.NoError(t, err)
				return tx
			},
			updateTx:               charInvalidateSubnetAuth,
			wantNotBootstrappedErr: nil,
			wantBootstrappedErr:    errUnauthorizedModification,
			wantWrapsFlowCheck:     false,
		},
		{
			// No semantic checks: the flow check, performed during execution
			// by applySpend, is skipped while bootstrapping.
			name:   "CreateSubnetTx",
			txType: "CreateSubnetTx",
			fork:   upgradetest.Latest,
			buildTx: func(t *testing.T, env *environment, _ *state.Diff) *platform.Tx {
				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueCreateSubnetTx(rewardsOwner)
				require.NoError(t, err)
				return tx
			},
			wantNotBootstrappedErr: nil,
			wantBootstrappedErr:    database.ErrNotFound,
			wantWrapsFlowCheck:     true,
		},
		{
			// Subnet auth and the flow check, performed during execution by
			// applySpend, are both skipped while bootstrapping.
			name:   "TransformSubnetTx",
			txType: "TransformSubnetTx",
			fork:   upgradetest.Durango,
			buildTx: func(t *testing.T, env *environment, _ *state.Diff) *platform.Tx {
				subnetID := testSubnet1.ID()
				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueTransformSubnetTx(
					subnetID,                  // subnetID
					ids.GenerateTestID(),      // assetID
					10,                        // initial supply
					10,                        // max supply
					0,                         // min consumption rate
					reward.PercentDenominator, // max consumption rate
					2,                         // min validator stake
					10,                        // max validator stake
					time.Minute,               // min stake duration
					time.Hour,                 // max stake duration
					1,                         // min delegation fees
					10,                        // min delegator stake
					1,                         // max validator weight factor
					80,                        // uptime requirement
				)
				require.NoError(t, err)
				return tx
			},
			// Pre-Etna fees are zero, so the wallet builds this tx with no
			// inputs and the flow check is vacuous. Injecting an unfunded
			// input invalidates the subnet-auth signature (it signed the
			// original bytes), which pins two facts instead: the subnet-auth
			// check runs BEFORE the flow check, and it is skipped while
			// bootstrapping — the tampered tx is only rejected once
			// bootstrapped.
			updateTx: func(t *testing.T, env *environment, tx *platform.Tx) {
				unsigned, ok := tx.Unsigned.(*platform.TransformSubnetTx)
				require.True(t, ok)
				unsigned.Ins = append(unsigned.Ins, charUnfundedInput(env))

				numCreds := len(tx.Creds)
				require.NotZero(t, numCreds)
				authCred := tx.Creds[numCreds-1]
				tx.Creds = append(tx.Creds[:numCreds-1],
					&secp256k1fx.Credential{Sigs: make([][65]byte, 1)},
					authCred,
				)
				require.NoError(t, tx.Initialize(platform.Codec))
			},
			wantNotBootstrappedErr: nil,
			wantBootstrappedErr:    errUnauthorizedModification,
			wantWrapsFlowCheck:     false,
		},
		{
			// verify.SameSubnet is gated on bootstrapping, and so is the flow
			// check performed during execution by applySpend.
			name:   "ExportTx",
			txType: "ExportTx",
			fork:   upgradetest.Latest,
			buildTx: func(t *testing.T, env *environment, _ *state.Diff) *platform.Tx {
				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueExportTx(
					env.ctx.XChainID,
					[]*avax.TransferableOutput{{
						Asset: avax.Asset{ID: env.ctx.AVAXAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt:          units.Avax,
							OutputOwners: *newOwner(),
						},
					}},
				)
				require.NoError(t, err)
				return tx
			},
			wantNotBootstrappedErr: nil,
			wantBootstrappedErr:    database.ErrNotFound,
			wantWrapsFlowCheck:     true,
		},
		{
			// The L1-validator capacity checks have no bootstrap
			// short-circuit; subnet auth and the flow check, performed during
			// execution by applySpend, are skipped while bootstrapping.
			name:   "ConvertSubnetToL1Tx",
			txType: "ConvertSubnetToL1Tx",
			fork:   upgradetest.Latest,
			buildTx: func(t *testing.T, env *environment, _ *state.Diff) *platform.Tx {
				subnetID := testSubnet1.ID()
				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueConvertSubnetToL1Tx(
					subnetID,
					ids.GenerateTestID(),
					[]byte{'a', 'd', 'd', 'r'},
					[]*platform.ConvertSubnetToL1Validator{{
						NodeID: ids.GenerateTestNodeID().Bytes(),
						Weight: 1,
						// Balance is 0 so the validator is not activated:
						// the test environment has no validator fee
						// capacity configured.
						Balance:               0,
						Signer:                *newProofOfPossession(t),
						RemainingBalanceOwner: message.PChainOwner{},
						DeactivationOwner:     message.PChainOwner{},
					}},
				)
				require.NoError(t, err)
				return tx
			},
			wantNotBootstrappedErr: nil,
			wantBootstrappedErr:    database.ErrNotFound,
			wantWrapsFlowCheck:     true,
		},
		{
			// Subnet auth runs BEFORE the L1-validator capacity checks and the
			// flow check, and is skipped while bootstrapping.
			name:   "ConvertSubnetToL1Tx_invalid_subnet_auth",
			txType: "ConvertSubnetToL1Tx",
			fork:   upgradetest.Latest,
			buildTx: func(t *testing.T, env *environment, _ *state.Diff) *platform.Tx {
				subnetID := testSubnet1.ID()
				wallet := newWallet(t, env, walletConfig{})
				tx, err := wallet.IssueConvertSubnetToL1Tx(
					subnetID,
					ids.GenerateTestID(),
					[]byte{'a', 'd', 'd', 'r'},
					[]*platform.ConvertSubnetToL1Validator{{
						NodeID:                ids.GenerateTestNodeID().Bytes(),
						Weight:                1,
						Balance:               0,
						Signer:                *newProofOfPossession(t),
						RemainingBalanceOwner: message.PChainOwner{},
						DeactivationOwner:     message.PChainOwner{},
					}},
				)
				require.NoError(t, err)
				return tx
			},
			updateTx:               charInvalidateSubnetAuth,
			wantNotBootstrappedErr: nil,
			wantBootstrappedErr:    errUnauthorizedModification,
			wantWrapsFlowCheck:     false,
		},
		{
			// NO bootstrap short-circuit for the semantic verification of the
			// warp message, which runs BEFORE the flow check performed during
			// execution: a tx whose warp message is parseable but references
			// a nonexistent subnet conversion fails on that lookup in both
			// bootstrap states. (The message must PARSE: fee complexity
			// calculation parses it before any state is read.)
			name:   "RegisterL1ValidatorTx_missing_subnet_conversion",
			txType: "RegisterL1ValidatorTx",
			fork:   upgradetest.Latest,
			buildTx: func(t *testing.T, env *environment, _ *state.Diff) *platform.Tx {
				registerPayload := must[*message.RegisterL1Validator](t)(message.NewRegisterL1Validator(
					ids.GenerateTestID(),     // subnetID
					ids.GenerateTestNodeID(), // nodeID
					newProofOfPossession(t).PublicKey,
					uint64(env.state.GetTimestamp().Add(5*time.Minute).Unix()), // expiry
					message.PChainOwner{},
					message.PChainOwner{},
					1, // weight
				))
				warpMessage := newWarpMessageBytes(
					t,
					env,
					ids.GenerateTestID(), // source chain
					[]byte{'a', 'd', 'd', 'r'},
					&warp.BitSetSignature{},
					registerPayload.Bytes(),
				)
				return charSignedTx(t, &platform.RegisterL1ValidatorTx{
					BaseTx:  charBaseTx(env),
					Balance: 1,
					Message: warpMessage,
				}, 1)
			},
			skipValid:              true,
			wantNotBootstrappedErr: database.ErrNotFound,
			wantBootstrappedErr:    database.ErrNotFound,
			wantWrapsFlowCheck:     false,
		},
		{
			// The proof of possession is verified in both bootstrap states:
			// it is what parses the BLS public key stored on the new L1
			// validator. The flow check, performed during execution by
			// applySpend, is skipped while bootstrapping.
			name:   "RegisterL1ValidatorTx",
			txType: "RegisterL1ValidatorTx",
			fork:   upgradetest.Latest,
			buildTx: func(t *testing.T, env *environment, diff *state.Diff) *platform.Tx {
				pop := newProofOfPossession(t)
				return charRegisterL1ValidatorTx(t, env, diff, pop.PublicKey, pop.ProofOfPossession)
			},
			wantNotBootstrappedErr: nil,
			wantBootstrappedErr:    database.ErrNotFound,
			wantWrapsFlowCheck:     true,
		},
		{
			// NO bootstrap short-circuit for the proof of possession, which
			// is verified BEFORE the flow check.
			name:   "RegisterL1ValidatorTx_invalid_proof_of_possession",
			txType: "RegisterL1ValidatorTx",
			fork:   upgradetest.Latest,
			buildTx: func(t *testing.T, env *environment, diff *state.Diff) *platform.Tx {
				pop := newProofOfPossession(t)
				otherPoP := newProofOfPossession(t)
				return charRegisterL1ValidatorTx(t, env, diff, pop.PublicKey, otherPoP.ProofOfPossession)
			},
			skipValid:              true,
			wantNotBootstrappedErr: signer.ErrInvalidProofOfPossession,
			wantBootstrappedErr:    signer.ErrInvalidProofOfPossession,
			wantWrapsFlowCheck:     false,
		},
		{
			// Same shape as RegisterL1ValidatorTx_missing_subnet_conversion:
			// the L1 validator lookup runs before the flow check, with no
			// bootstrap short-circuit.
			name:   "SetL1ValidatorWeightTx",
			txType: "SetL1ValidatorWeightTx",
			fork:   upgradetest.Latest,
			buildTx: func(t *testing.T, env *environment, _ *state.Diff) *platform.Tx {
				weightPayload := must[*message.L1ValidatorWeight](t)(message.NewL1ValidatorWeight(
					ids.GenerateTestID(), // validationID
					1,                    // nonce
					0,                    // weight
				))
				warpMessage := newWarpMessageBytes(
					t,
					env,
					ids.GenerateTestID(), // source chain
					[]byte{'a', 'd', 'd', 'r'},
					&warp.BitSetSignature{},
					weightPayload.Bytes(),
				)
				return charSignedTx(t, &platform.SetL1ValidatorWeightTx{
					BaseTx:  charBaseTx(env),
					Message: warpMessage,
				}, 1)
			},
			skipValid:              true,
			wantNotBootstrappedErr: database.ErrNotFound,
			wantBootstrappedErr:    database.ErrNotFound,
			wantWrapsFlowCheck:     false,
		},
		{
			// The L1 validator lookup runs BEFORE the flow check performed
			// during execution, with no bootstrap short-circuit.
			name:   "IncreaseL1ValidatorBalanceTx",
			txType: "IncreaseL1ValidatorBalanceTx",
			fork:   upgradetest.Latest,
			buildTx: func(t *testing.T, env *environment, _ *state.Diff) *platform.Tx {
				return charSignedTx(t, &platform.IncreaseL1ValidatorBalanceTx{
					BaseTx:       charBaseTx(env),
					ValidationID: ids.GenerateTestID(),
					Balance:      1,
				}, 1)
			},
			skipValid:              true,
			wantNotBootstrappedErr: database.ErrNotFound,
			wantBootstrappedErr:    database.ErrNotFound,
			wantWrapsFlowCheck:     false,
		},
		{
			// The L1 validator lookup happens BEFORE the flow check, so a tx
			// for a nonexistent validation ID fails on the lookup — in both
			// bootstrap states. (The lookup miss and the flow check both
			// surface database.ErrNotFound; the pinned fact is the absence
			// of any bootstrap gate.)
			name:   "DisableL1ValidatorTx",
			txType: "DisableL1ValidatorTx",
			fork:   upgradetest.Latest,
			buildTx: func(t *testing.T, env *environment, _ *state.Diff) *platform.Tx {
				return charSignedTx(t, &platform.DisableL1ValidatorTx{
					BaseTx:       charBaseTx(env),
					ValidationID: ids.GenerateTestID(),
					DisableAuth:  &secp256k1fx.Input{},
				}, 2)
			},
			skipValid:              true,
			wantNotBootstrappedErr: database.ErrNotFound,
			wantBootstrappedErr:    database.ErrNotFound,
			wantWrapsFlowCheck:     false,
		},
		{
			// Bootstrap short-circuit placement: right after verifyTx (the
			// Helicon gate, syntactic, and memo checks), before the staking
			// rules and flow check.
			name:   "AddAutoRenewedValidatorTx",
			txType: "AddAutoRenewedValidatorTx",
			fork:   upgradetest.Latest,
			buildTx: func(t *testing.T, env *environment, _ *state.Diff) *platform.Tx {
				return newAddAutoRenewedValidatorTx(t, env, 2*env.config.MinValidatorStake, 100_000, 200_000)
			},
			wantNotBootstrappedErr: nil,
			wantBootstrappedErr:    database.ErrNotFound,
			wantWrapsFlowCheck:     true,
		},
		{
			// The staker-tx and current-validator lookups happen BEFORE the
			// bootstrap short-circuit; with valid setup they pass and the
			// short-circuit then skips the flow check.
			name:   "SetAutoRenewedValidatorConfigTx",
			txType: "SetAutoRenewedValidatorConfigTx",
			fork:   upgradetest.Latest,
			buildTx: func(t *testing.T, env *environment, diff *state.Diff) *platform.Tx {
				// The same wallet must issue both txs: it resolves the
				// validator's config authority from its own issued platform.
				wallet := newWallet(t, env, walletConfig{})
				addTx, err := wallet.IssueAddAutoRenewedValidatorTx(
					ids.GenerateTestNodeID(),
					2*env.config.MinValidatorStake,
					newProofOfPossession(t),
					newOwner(),
					newOwner(),
					&secp256k1fx.OutputOwners{},
					100_000,
					200_000,
					env.config.MinStakeDuration,
				)
				require.NoError(t, err)

				feeCalculator := state.PickFeeCalculator(env.config, diff)
				_, _, _, err = StandardTx(&env.backend, feeCalculator, addTx, diff)
				require.NoError(t, err)
				// Record the tx so the config tx's staker-tx lookup finds it.
				diff.AddTx(addTx, status.Committed)

				tx, err := wallet.IssueSetAutoRenewedValidatorConfigTx(
					addTx.ID(),
					100_000,
					env.config.MinStakeDuration,
				)
				require.NoError(t, err)
				return tx
			},
			wantNotBootstrappedErr: nil,
			wantBootstrappedErr:    database.ErrNotFound,
			wantWrapsFlowCheck:     true,
		},
	}
}

// runCharFlowTest executes one charFlowTest row in the given bootstrap state
// with the tx's consumed UTXOs deleted, and asserts the expected error and
// its errFlowCheckFailed wrapping.
func runCharFlowTest(t *testing.T, tt charFlowTest, bootstrapped bool) {
	env := newEnvironment(t, tt.fork)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	// Charge non-zero (dynamic, so post-Etna only) fees so that fee-only txs
	// are funded with real inputs; with the zero fees the test environment
	// defaults to, such txs have no inputs and their flow check is vacuous.
	env.config.DynamicFeeConfig = genesis.LocalParams.DynamicFeeConfig

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(t, err)

	tx := tt.buildTx(t, env, diff)
	if tt.updateTx != nil {
		tt.updateTx(t, env, tx)
	}
	deleteInputUTXOs(diff, tx)

	env.backend.Bootstrapped.Set(bootstrapped)

	feeCalculator := state.PickFeeCalculator(env.config, diff)
	if tt.proposal {
		onAbortState, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
		require.NoError(t, err)
		deleteInputUTXOs(onAbortState, tx)
		err = ProposalTx(&env.backend, feeCalculator, tx, diff, onAbortState)
		assertCharFlowErr(t, tt, bootstrapped, err)
		return
	}
	_, _, _, err = StandardTx(&env.backend, feeCalculator, tx, diff)
	assertCharFlowErr(t, tt, bootstrapped, err)
}

func assertCharFlowErr(t *testing.T, tt charFlowTest, bootstrapped bool, err error) {
	t.Helper()

	expected := tt.wantNotBootstrappedErr
	if bootstrapped {
		expected = tt.wantBootstrappedErr
	}
	require.ErrorIs(t, err, expected)
	if expected != nil {
		require.Equal(
			t,
			tt.wantWrapsFlowCheck,
			errors.Is(err, errFlowCheckFailed),
			"unexpected errFlowCheckFailed wrapping: %v",
			err,
		)
	}
}

// TestCharacterizationValidTxs sanity-checks the builders: each valid tx
// passes verification at its home fork.
func TestCharacterizationValidTxs(t *testing.T) {
	for _, tt := range charFlowTests() {
		if tt.skipValid {
			continue
		}
		t.Run(tt.name, func(t *testing.T) {
			env := newEnvironment(t, tt.fork)
			env.ctx.Lock.Lock()
			defer env.ctx.Lock.Unlock()

			env.config.DynamicFeeConfig = genesis.LocalParams.DynamicFeeConfig

			diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(t, err)

			tx := tt.buildTx(t, env, diff)

			feeCalculator := state.PickFeeCalculator(env.config, diff)
			if tt.proposal {
				onAbortState, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
				require.NoError(t, err)
				require.NoError(t, ProposalTx(&env.backend, feeCalculator, tx, diff, onAbortState))
				return
			}
			_, _, _, err = StandardTx(&env.backend, feeCalculator, tx, diff)
			require.NoError(t, err)
		})
	}
}

// TestCharacterizationBootstrapGates pins where each tx type's Bootstrapped
// short-circuit sits (or that it has none): with the consumed UTXOs missing
// and bootstrapping in progress, gated txs pass and ungated txs fail.
func TestCharacterizationBootstrapGates(t *testing.T) {
	for _, tt := range charFlowTests() {
		t.Run(tt.name, func(t *testing.T) {
			runCharFlowTest(t, tt, false /*=bootstrapped*/)
		})
	}
}

// TestCharacterizationFlowCheckErrors pins the identity and wrapping of the
// error returned when the flow check (or a state lookup preceding it) fails
// on a bootstrapped node.
func TestCharacterizationFlowCheckErrors(t *testing.T) {
	for _, tt := range charFlowTests() {
		t.Run(tt.name, func(t *testing.T) {
			runCharFlowTest(t, tt, true /*=bootstrapped*/)
		})
	}
}

// TestCharacterizationImportTx pins ImportTx's unique bootstrap behavior:
// the ENTIRE semantic check block — verify.SameSubnet, the shared-memory
// UTXO fetch, and the flow check — is skipped when not bootstrapped OR when
// the node is partially syncing the primary network.
func TestCharacterizationImportTx(t *testing.T) {
	tests := []struct {
		name         string
		bootstrapped bool
		partialSync  bool
		wantErr      error
	}{
		{
			name:         "bootstrapped",
			bootstrapped: true,
			wantErr:      database.ErrNotFound,
		},
		{
			name:         "not_bootstrapped_skips_all_semantic_checks",
			bootstrapped: false,
			wantErr:      nil,
		},
		{
			name:         "partial_sync_skips_all_semantic_checks",
			bootstrapped: true,
			partialSync:  true,
			wantErr:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newEnvironment(t, upgradetest.Latest)
			env.ctx.Lock.Lock()
			defer env.ctx.Lock.Unlock()

			var (
				sourceChain       = env.ctx.XChainID
				sourceKey         = genesistest.DefaultFundedKeys[1]
				emptySharedMemory = env.msm.SharedMemory
			)
			env.msm.SharedMemory = fundedSharedMemory(
				t,
				env,
				sourceKey,
				sourceChain,
				map[ids.ID]uint64{
					env.ctx.AVAXAssetID: 10 * units.Avax,
				},
				rand.NewSource(0),
			)

			wallet := newWallet(t, env, walletConfig{
				chainIDs: []ids.ID{sourceChain},
			})
			tx, err := wallet.IssueImportTx(
				sourceChain,
				newOwner(),
			)
			require.NoError(t, err)

			// Take the imported UTXOs away again so the shared-memory fetch
			// must fail if it runs.
			env.msm.SharedMemory = emptySharedMemory

			env.backend.Bootstrapped.Set(tt.bootstrapped)
			env.config.PartialSyncPrimaryNetwork = tt.partialSync

			err = executeStandardCharTx(t, env, tx)
			require.ErrorIs(t, err, tt.wantErr)
			require.NotErrorIs(t, err, errFlowCheckFailed)
		})
	}
}

// TestCharacterizationProposalInvariants pins the cheap invariants of the
// proposal-only txs that carry no inputs and perform no flow check.
func TestCharacterizationProposalInvariants(t *testing.T) {
	tests := []struct {
		name    string
		fork    upgradetest.Fork
		buildTx func(t *testing.T, env *environment) *platform.Tx
		wantErr error
	}{
		{
			name: "AdvanceTimeTx_with_credentials",
			fork: upgradetest.ApricotPhase5,
			buildTx: func(t *testing.T, env *environment) *platform.Tx {
				tx := newAdvanceTimeTx(t, env.state.GetTimestamp().Add(time.Second))
				tx.Creds = []verify.Verifiable{
					&secp256k1fx.Credential{},
				}
				return tx
			},
			wantErr: errWrongNumberOfCredentials,
		},
		{
			name: "RewardValidatorTx_empty_txID",
			fork: upgradetest.ApricotPhase5,
			buildTx: func(t *testing.T, _ *environment) *platform.Tx {
				// Built by hand: the constructor itself rejects an empty
				// txID.
				tx, err := platform.NewSignedTx(&platform.RewardValidatorTx{}, platform.Codec, nil)
				require.NoError(t, err)
				return tx
			},
			// The sentinel is unexported by the platform package, so it is
			// obtained from the self-check the executor is expected to run.
			wantErr: (&platform.RewardValidatorTx{}).SyntacticVerify(nil),
		},
		{
			name: "RewardValidatorTx_with_credentials",
			fork: upgradetest.ApricotPhase5,
			buildTx: func(t *testing.T, _ *environment) *platform.Tx {
				tx := newRewardValidatorTx(t, ids.GenerateTestID())
				tx.Creds = []verify.Verifiable{
					&secp256k1fx.Credential{},
				}
				return tx
			},
			wantErr: errWrongNumberOfCredentials,
		},
		{
			name: "RewardAutoRenewedValidatorTx_with_credentials",
			fork: upgradetest.Latest,
			buildTx: func(t *testing.T, env *environment) *platform.Tx {
				tx := newRewardAutoRenewedValidatorTx(t, ids.GenerateTestID(), env.state.GetTimestamp())
				tx.Creds = []verify.Verifiable{
					&secp256k1fx.Credential{},
				}
				return tx
			},
			wantErr: errWrongNumberOfCredentials,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newEnvironment(t, tt.fork)
			env.ctx.Lock.Lock()
			defer env.ctx.Lock.Unlock()

			tx := tt.buildTx(t, env)
			err := executeProposalCharTx(t, env, tx)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

// TestCharacterizationTxTypeCoverage fails when a tx type is added to
// platform.TxVisitor without extending the characterization tables in this file.
func TestCharacterizationTxTypeCoverage(t *testing.T) {
	covered := set.Of(
		// TestCharacterizationForkGates + charFlowTests:
		"AddValidatorTx",
		"AddSubnetValidatorTx",
		"AddDelegatorTx",
		"AddPermissionlessValidatorTx",
		"AddPermissionlessDelegatorTx",
		"RemoveSubnetValidatorTx",
		"TransferSubnetOwnershipTx",
		"BaseTx",
		"CreateChainTx",
		"CreateSubnetTx",
		"TransformSubnetTx",
		"ExportTx",
		"ConvertSubnetToL1Tx",
		"RegisterL1ValidatorTx",
		"SetL1ValidatorWeightTx",
		"IncreaseL1ValidatorBalanceTx",
		"DisableL1ValidatorTx",
		"AddAutoRenewedValidatorTx",
		"SetAutoRenewedValidatorConfigTx",
		// TestCharacterizationImportTx:
		"ImportTx",
		// TestCharacterizationProposalInvariants:
		"AdvanceTimeTx",
		"RewardValidatorTx",
		"RewardAutoRenewedValidatorTx",
	)

	for _, tt := range charFlowTests() {
		require.Contains(t, covered, tt.txType, "charFlowTests row %q has unknown txType", tt.name)
	}

	visitorType := reflect.TypeOf((*platform.TxVisitor)(nil)).Elem()
	for i := 0; i < visitorType.NumMethod(); i++ {
		method := visitorType.Method(i).Name
		require.Contains(
			t,
			covered,
			method,
			"tx type %s has no characterization coverage; extend the tables in this file",
			method,
		)
	}
	require.Equal(t, visitorType.NumMethod(), covered.Len(), "characterization table covers tx types that no longer exist")
}
