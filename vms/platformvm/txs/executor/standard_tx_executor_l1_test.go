// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

// TestStandardExecutorConvertSubnetToL1TxErrors verifies the failure cases of
// [txs.ConvertSubnetToL1Tx] execution.
func TestStandardExecutorConvertSubnetToL1TxErrors(t *testing.T) {
	env := newEnvironment(t, upgradetest.Latest)
	// Charge non-zero fees and allow two active L1 validators.
	env.config.DynamicFeeConfig = genesis.LocalParams.DynamicFeeConfig
	env.config.ValidatorFeeConfig = genesis.LocalParams.ValidatorFeeConfig
	env.config.ValidatorFeeConfig.Capacity = 2

	sk, err := localsigner.New()
	require.NoError(t, err)

	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(t, err)

	subnetID := testSubnet1.ID()
	nodeID := ids.GenerateTestNodeID()
	owner := message.PChainOwner{
		Threshold: 1,
		Addresses: []ids.ShortID{genesistest.DefaultFundedKeys[0].Address()},
	}

	tests := []struct {
		name        string
		want        error
		updateTx    func(*testing.T, *txs.Tx)
		updateState func(*testing.T, *state.Diff)
	}{
		{
			name: "invalid_prior_to_etna",
			updateState: func(_ *testing.T, diff *state.Diff) {
				diff.SetTimestamp(env.config.UpgradeConfig.EtnaTime.Add(-1 * time.Second))
			},
			want: errEtnaUpgradeNotActive,
		},
		{
			name: "tx_fails_syntactic_verification",
			updateTx: func(_ *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.ConvertSubnetToL1Tx).BaseTx.BlockchainID = ids.GenerateTestID()
			},
			want: avax.ErrWrongChainID,
		},
		{
			name: "invalid_memo_length",
			updateTx: func(_ *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.ConvertSubnetToL1Tx).Memo = []byte("memo!")
			},
			want: avax.ErrMemoTooLarge,
		},
		{
			name: "fail_subnet_authorization",
			updateState: func(_ *testing.T, diff *state.Diff) {
				diff.SetSubnetOwner(subnetID, &secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
				})
			},
			want: errUnauthorizedModification,
		},
		{
			name: "invalid_if_subnet_is_transformed",
			updateState: func(_ *testing.T, diff *state.Diff) {
				diff.AddSubnetTransformation(&txs.Tx{Unsigned: &txs.TransformSubnetTx{
					Subnet: subnetID,
				}})
			},
			want: errIsImmutable,
		},
		{
			name: "invalid_if_subnet_is_converted",
			updateState: func(_ *testing.T, diff *state.Diff) {
				diff.SetSubnetToL1Conversion(subnetID, state.SubnetToL1Conversion{
					ConversionID: ids.GenerateTestID(),
					ChainID:      ids.GenerateTestID(),
					Addr:         utils.RandomBytes(32),
				})
			},
			want: errIsImmutable,
		},
		{
			name: "too_many_active_validators",
			updateState: func(t *testing.T, diff *state.Diff) {
				// Fill the active validator capacity with L1 validators of
				// another subnet
				for range env.config.ValidatorFeeConfig.Capacity {
					require.NoError(t, diff.PutL1Validator(state.L1Validator{
						ValidationID:      ids.GenerateTestID(),
						SubnetID:          ids.GenerateTestID(),
						NodeID:            ids.GenerateTestNodeID(),
						Weight:            1,
						EndAccumulatedFee: 1, // Active
					}))
				}
			},
			want: errMaxNumActiveValidators,
		},
		{
			name: "duplicate_l1_validator",
			updateState: func(t *testing.T, diff *state.Diff) {
				require.NoError(t, diff.PutL1Validator(state.L1Validator{
					ValidationID: ids.GenerateTestID(),
					SubnetID:     subnetID,
					NodeID:       nodeID,
					Weight:       1,
				}))
			},
			want: state.ErrDuplicateL1Validator,
		},
		{
			name: "flow_checker_failed",
			updateTx: func(_ *testing.T, tx *txs.Tx) {
				// Produce more AVAX than the tx consumes
				unsignedTx := tx.Unsigned.(*txs.ConvertSubnetToL1Tx)
				unsignedTx.Outs = append(unsignedTx.Outs, &avax.TransferableOutput{
					Asset: avax.Asset{ID: env.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: math.MaxUint64 / 2,
					},
				})
			},
			want: utxo.ErrInsufficientUnlockedFunds,
		},
		{
			name: "validators_balance_overflow",
			updateTx: func(_ *testing.T, tx *txs.Tx) {
				// The validator balances sum to more than math.MaxUint64
				unsignedTx := tx.Unsigned.(*txs.ConvertSubnetToL1Tx)
				unsignedTx.Validators = append(unsignedTx.Validators, &txs.ConvertSubnetToL1Validator{
					NodeID:                ids.GenerateTestNodeID().Bytes(),
					Weight:                1,
					Balance:               math.MaxUint64,
					Signer:                *pop,
					RemainingBalanceOwner: owner,
					DeactivationOwner:     owner,
				})
				utils.Sort(unsignedTx.Validators)
			},
			want: safemath.ErrOverflow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			// Fees are non-zero, so issue each tx from a fresh wallet to keep
			// the wallet's UTXO view consistent with env.state.
			wallet := newWallet(t, env, walletConfig{})
			tx, err := wallet.IssueConvertSubnetToL1Tx(
				subnetID,
				ids.GenerateTestID(),
				utils.RandomBytes(32),
				[]*txs.ConvertSubnetToL1Validator{{
					NodeID:                nodeID.Bytes(),
					Weight:                1,
					Balance:               1,
					Signer:                *pop,
					RemainingBalanceOwner: owner,
					DeactivationOwner:     owner,
				}},
			)
			require.NoError(err)

			diff, got := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(got)

			if tt.updateTx != nil {
				tt.updateTx(t, tx)
			}

			if tt.updateState != nil {
				tt.updateState(t, diff)
			}

			feeCalculator := state.PickFeeCalculator(env.config, env.state)
			_, _, _, got = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				diff,
			)

			require.ErrorIs(got, tt.want)
		})
	}
}

// TestStandardExecutorConvertSubnetToL1Tx verifies the successful execution
// of a [txs.ConvertSubnetToL1Tx].
func TestStandardExecutorConvertSubnetToL1Tx(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t, upgradetest.Latest)
	// Charge non-zero fees so the tx is funded with real inputs and outputs.
	env.config.DynamicFeeConfig = genesis.LocalParams.DynamicFeeConfig
	env.config.ValidatorFeeConfig = genesis.LocalParams.ValidatorFeeConfig

	wallet := newWallet(t, env, walletConfig{})

	sk, err := localsigner.New()
	require.NoError(err)

	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(err)

	subnetID := testSubnet1.ID()
	nodeID := ids.GenerateTestNodeID()
	chainID := ids.GenerateTestID()
	address := utils.RandomBytes(32)
	owner := message.PChainOwner{
		Threshold: 1,
		Addresses: []ids.ShortID{genesistest.DefaultFundedKeys[0].Address()},
	}

	const weight = 1

	validator := &txs.ConvertSubnetToL1Validator{
		NodeID:                nodeID.Bytes(),
		Weight:                weight,
		Balance:               1,
		Signer:                *pop,
		RemainingBalanceOwner: owner,
		DeactivationOwner:     owner,
	}

	stx, err := wallet.IssueConvertSubnetToL1Tx(
		subnetID,
		chainID,
		address,
		[]*txs.ConvertSubnetToL1Validator{validator},
	)
	require.NoError(err)

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, env.state)
	_, _, _, err = StandardTx(
		&env.backend,
		feeCalculator,
		stx,
		diff,
	)
	require.NoError(err)

	requireBaseTxApplied(t, env, diff, feeCalculator, stx)

	// assert that the subnet-to-L1 conversion was recorded
	wantConversionID, err := message.SubnetToL1ConversionID(message.SubnetToL1ConversionData{
		SubnetID:       subnetID,
		ManagerChainID: chainID,
		ManagerAddress: address,
		Validators: []message.SubnetToL1ConversionValidatorData{
			{
				NodeID:       nodeID.Bytes(),
				BLSPublicKey: pop.PublicKey,
				Weight:       weight,
			},
		},
	})
	require.NoError(err)

	gotConversion, err := diff.GetSubnetToL1Conversion(subnetID)
	require.NoError(err)
	require.Equal(
		state.SubnetToL1Conversion{
			ConversionID: wantConversionID,
			ChainID:      chainID,
			Addr:         address,
		},
		gotConversion,
	)

	// assert that the L1 validator was added
	remainingBalanceOwner, err := txs.Codec.Marshal(txs.CodecVersion, &validator.RemainingBalanceOwner)
	require.NoError(err)

	deactivationOwner, err := txs.Codec.Marshal(txs.CodecVersion, &validator.DeactivationOwner)
	require.NoError(err)

	validationID := subnetID.Append(0)
	gotL1Validator, err := diff.GetL1Validator(validationID)
	require.NoError(err)
	require.Equal(
		state.L1Validator{
			ValidationID:          validationID,
			SubnetID:              subnetID,
			NodeID:                nodeID,
			PublicKey:             bls.PublicKeyToUncompressedBytes(sk.PublicKey()),
			RemainingBalanceOwner: remainingBalanceOwner,
			DeactivationOwner:     deactivationOwner,
			StartTime:             uint64(diff.GetTimestamp().Unix()),
			Weight:                weight,
			MinNonce:              0,
			EndAccumulatedFee:     validator.Balance + diff.GetAccruedFees(),
		},
		gotL1Validator,
	)
}

// TestStandardExecutorRegisterL1ValidatorTxErrors verifies the failure cases
// of [txs.RegisterL1ValidatorTx] execution.
func TestStandardExecutorRegisterL1ValidatorTxErrors(t *testing.T) {
	env := newEnvironment(t, upgradetest.Latest)
	// Charge non-zero fees and allow two active L1 validators.
	env.config.DynamicFeeConfig = genesis.LocalParams.DynamicFeeConfig
	env.config.ValidatorFeeConfig = genesis.LocalParams.ValidatorFeeConfig
	env.config.ValidatorFeeConfig.Capacity = 2

	initialSK, err := localsigner.New()
	require.NoError(t, err)

	initialPoP, err := signer.NewProofOfPossession(initialSK)
	require.NoError(t, err)

	subnetID := testSubnet1.ID()
	chainID := ids.GenerateTestID()
	address := utils.RandomBytes(32)

	// Convert the subnet to an L1 with one active validator
	convertWallet := newWallet(t, env, walletConfig{})
	convertSubnetToL1Tx, err := convertWallet.IssueConvertSubnetToL1Tx(
		subnetID,
		chainID,
		address,
		[]*txs.ConvertSubnetToL1Validator{{
			NodeID:                ids.GenerateTestNodeID().Bytes(),
			Weight:                1,
			Balance:               units.Avax,
			Signer:                *initialPoP,
			RemainingBalanceOwner: message.PChainOwner{},
			DeactivationOwner:     message.PChainOwner{},
		}},
	)
	require.NoError(t, err)

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(t, err)

	_, _, _, err = StandardTx(
		&env.backend,
		state.PickFeeCalculator(env.config, env.state),
		convertSubnetToL1Tx,
		diff,
	)
	require.NoError(t, err)
	require.NoError(t, diff.Apply(env.state))
	require.NoError(t, env.state.Commit())

	var (
		nodeID           = ids.GenerateTestNodeID()
		lastAcceptedTime = env.state.GetTimestamp()
		expiryTime       = lastAcceptedTime.Add(5 * time.Minute)
		expiry           = uint64(expiryTime.Unix()) // The warp message will expire in 5 minutes
	)

	const weight = 1

	// Create the Warp message
	sk, err := localsigner.New()
	require.NoError(t, err)

	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(t, err)

	var (
		remainingBalanceOwner = message.PChainOwner{}
		deactivationOwner     = message.PChainOwner{}
	)

	addressedCallPayload := must[*message.RegisterL1Validator](t)(message.NewRegisterL1Validator(
		subnetID,
		nodeID,
		pop.PublicKey,
		expiry,
		remainingBalanceOwner,
		deactivationOwner,
		weight,
	))
	unsignedWarp := must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
		env.ctx.NetworkID,
		chainID,
		must[*payload.AddressedCall](t)(payload.NewAddressedCall(
			address,
			addressedCallPayload.Bytes(),
		)).Bytes(),
	))
	sig, err := sk.Sign(unsignedWarp.Bytes())
	require.NoError(t, err)

	warpSignature := &warp.BitSetSignature{
		Signers:   set.NewBits(0).Bytes(),
		Signature: ([bls.SignatureLen]byte)(bls.SignatureToBytes(sig)),
	}
	warpMessage := must[*warp.Message](t)(warp.NewMessage(
		unsignedWarp,
		warpSignature,
	))

	// newWarpMessageBytes wraps an addressed-call payload into warp message
	// bytes. The executor doesn't verify the warp signature, so reusing
	// warpSignature for every payload is fine.
	newWarpMessageBytes := func(t *testing.T, payloadBytes []byte) []byte {
		return must[*warp.Message](t)(warp.NewMessage(
			must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
				env.ctx.NetworkID,
				chainID,
				must[*payload.AddressedCall](t)(payload.NewAddressedCall(
					address,
					payloadBytes,
				)).Bytes(),
			)),
			warpSignature,
		)).Bytes()
	}

	validationID := addressedCallPayload.ValidationID()
	tests := []struct {
		name        string
		balance     uint64
		want        error
		updateTx    func(*testing.T, *txs.Tx)
		updateState func(*testing.T, *state.Diff)
	}{
		{
			name: "invalid_prior_to_etna",
			updateState: func(_ *testing.T, diff *state.Diff) {
				diff.SetTimestamp(env.config.UpgradeConfig.EtnaTime.Add(-1 * time.Second))
			},
			want: errEtnaUpgradeNotActive,
		},
		{
			name: "tx_fails_syntactic_verification",
			updateTx: func(_ *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.RegisterL1ValidatorTx).BaseTx.BlockchainID = ids.GenerateTestID()
			},
			want: avax.ErrWrongChainID,
		},
		{
			name: "invalid_memo_length",
			updateTx: func(_ *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.RegisterL1ValidatorTx).Memo = []byte("memo!")
			},
			want: avax.ErrMemoTooLarge,
		},
		{
			name: "fee_calculation_overflow",
			updateTx: func(_ *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.RegisterL1ValidatorTx).Balance = math.MaxUint64
			},
			want: safemath.ErrOverflow,
		},
		{
			name: "flow_checker_failed",
			updateTx: func(_ *testing.T, tx *txs.Tx) {
				// Produce more AVAX than the tx consumes
				unsignedTx := tx.Unsigned.(*txs.RegisterL1ValidatorTx)
				unsignedTx.Outs = append(unsignedTx.Outs, &avax.TransferableOutput{
					Asset: avax.Asset{ID: env.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: math.MaxUint64 / 2,
					},
				})
			},
			want: utxo.ErrInsufficientUnlockedFunds,
		},
		{
			name: "invalid_warp_message",
			updateTx: func(_ *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.RegisterL1ValidatorTx).Message = []byte{}
			},
			want: codec.ErrCantUnpackVersion,
		},
		{
			name: "invalid_warp_payload",
			updateTx: func(t *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.RegisterL1ValidatorTx).Message = must[*warp.Message](t)(warp.NewMessage(
					must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
						env.ctx.NetworkID,
						chainID,
						must[*payload.Hash](t)(payload.NewHash(ids.Empty)).Bytes(),
					)),
					warpSignature,
				)).Bytes()
			},
			want: payload.ErrWrongType,
		},
		{
			name: "invalid_addressed_call",
			updateTx: func(t *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.RegisterL1ValidatorTx).Message = newWarpMessageBytes(
					t,
					must[*message.SubnetToL1Conversion](t)(message.NewSubnetToL1Conversion(ids.Empty)).Bytes(),
				)
			},
			want: message.ErrWrongType,
		},
		{
			name: "invalid_addressed_call_payload",
			updateTx: func(t *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.RegisterL1ValidatorTx).Message = newWarpMessageBytes(
					t,
					must[*message.RegisterL1Validator](t)(message.NewRegisterL1Validator(
						subnetID,
						nodeID,
						pop.PublicKey,
						expiry,
						remainingBalanceOwner,
						deactivationOwner,
						0, // weight = 0 is invalid
					)).Bytes(),
				)
			},
			want: message.ErrInvalidWeight,
		},
		{
			name: "subnet_conversion_not_found",
			updateTx: func(t *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.RegisterL1ValidatorTx).Message = newWarpMessageBytes(
					t,
					must[*message.RegisterL1Validator](t)(message.NewRegisterL1Validator(
						ids.GenerateTestID(), // invalid subnetID
						nodeID,
						pop.PublicKey,
						expiry,
						remainingBalanceOwner,
						deactivationOwner,
						weight,
					)).Bytes(),
				)
			},
			want: errCouldNotLoadSubnetToL1Conversion,
		},
		{
			name: "invalid_source_chain",
			updateState: func(_ *testing.T, diff *state.Diff) {
				diff.SetSubnetToL1Conversion(subnetID, state.SubnetToL1Conversion{})
			},
			want: errWrongWarpMessageSourceChainID,
		},
		{
			name: "invalid_source_address",
			updateState: func(_ *testing.T, diff *state.Diff) {
				diff.SetSubnetToL1Conversion(subnetID, state.SubnetToL1Conversion{
					ChainID: chainID,
				})
			},
			want: errWrongWarpMessageSourceAddress,
		},
		{
			name: "message_expired",
			updateState: func(_ *testing.T, diff *state.Diff) {
				diff.SetTimestamp(expiryTime)
			},
			want: errWarpMessageExpired,
		},
		{
			name: "message_expiry_too_far_in_the_future",
			updateTx: func(t *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.RegisterL1ValidatorTx).Message = newWarpMessageBytes(
					t,
					must[*message.RegisterL1Validator](t)(message.NewRegisterL1Validator(
						subnetID,
						nodeID,
						pop.PublicKey,
						math.MaxUint64, // expiry too far in the future
						remainingBalanceOwner,
						deactivationOwner,
						weight,
					)).Bytes(),
				)
			},
			want: errWarpMessageNotYetAllowed,
		},
		{
			name:    "l1_validator_previously_registered",
			balance: 1,
			updateState: func(_ *testing.T, diff *state.Diff) {
				diff.PutExpiry(state.ExpiryEntry{
					Timestamp:    expiry,
					ValidationID: validationID,
				})
			},
			want: errWarpMessageAlreadyIssued,
		},
		{
			name: "invalid_pop",
			updateTx: func(t *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.RegisterL1ValidatorTx).Message = newWarpMessageBytes(
					t,
					must[*message.RegisterL1Validator](t)(message.NewRegisterL1Validator(
						subnetID,
						nodeID,
						initialPoP.PublicKey, // Wrong public key
						expiry,
						remainingBalanceOwner,
						deactivationOwner,
						weight,
					)).Bytes(),
				)
			},
			want: signer.ErrInvalidProofOfPossession,
		},
		{
			name:    "too_many_active_validators",
			balance: 1,
			updateState: func(t *testing.T, diff *state.Diff) {
				// Fill the active validator capacity with L1 validators of
				// another subnet
				for range env.config.ValidatorFeeConfig.Capacity {
					require.NoError(t, diff.PutL1Validator(state.L1Validator{
						ValidationID:      ids.GenerateTestID(),
						SubnetID:          ids.GenerateTestID(),
						NodeID:            ids.GenerateTestNodeID(),
						Weight:            1,
						EndAccumulatedFee: 1, // Active
					}))
				}
			},
			want: errMaxNumActiveValidators,
		},
		{
			name:    "accrued_fees_overflow",
			balance: 1,
			updateState: func(_ *testing.T, diff *state.Diff) {
				diff.SetAccruedFees(math.MaxUint64)
			},
			want: safemath.ErrOverflow,
		},
		{
			name: "duplicate_subnetid_nodeid_pair",
			updateState: func(t *testing.T, diff *state.Diff) {
				require.NoError(t, diff.PutL1Validator(state.L1Validator{
					ValidationID: ids.GenerateTestID(),
					SubnetID:     subnetID,
					NodeID:       nodeID,
					PublicKey:    bls.PublicKeyToUncompressedBytes(initialSK.PublicKey()),
					Weight:       1,
				}))
			},
			want: state.ErrDuplicateL1Validator,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			// Fees are non-zero, so issue each tx from a fresh wallet to keep
			// the wallet's UTXO view consistent with env.state.
			wallet := newWallet(t, env, walletConfig{})
			tx, err := wallet.IssueRegisterL1ValidatorTx(
				tt.balance,
				pop.ProofOfPossession,
				warpMessage.Bytes(),
			)
			require.NoError(err)

			diff, got := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(got)

			if tt.updateTx != nil {
				tt.updateTx(t, tx)
			}

			if tt.updateState != nil {
				tt.updateState(t, diff)
			}

			feeCalculator := state.PickFeeCalculator(env.config, env.state)
			_, _, _, got = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				diff,
			)

			require.ErrorIs(got, tt.want)
		})
	}
}

// TestStandardExecutorRegisterL1ValidatorTx verifies the successful execution
// of a [txs.RegisterL1ValidatorTx].
func TestStandardExecutorRegisterL1ValidatorTx(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t, upgradetest.Latest)
	// Charge non-zero fees so the tx is funded with real inputs and outputs.
	env.config.DynamicFeeConfig = genesis.LocalParams.DynamicFeeConfig
	env.config.ValidatorFeeConfig = genesis.LocalParams.ValidatorFeeConfig

	initialSK, err := localsigner.New()
	require.NoError(err)

	initialPoP, err := signer.NewProofOfPossession(initialSK)
	require.NoError(err)

	var (
		subnetID = testSubnet1.ID()
		chainID  = ids.GenerateTestID()
		address  = utils.RandomBytes(32)
	)

	// Convert the subnet to an L1 with one active validator
	convertWallet := newWallet(t, env, walletConfig{})
	convertSubnetToL1Tx, err := convertWallet.IssueConvertSubnetToL1Tx(
		subnetID,
		chainID,
		address,
		[]*txs.ConvertSubnetToL1Validator{{
			NodeID:                ids.GenerateTestNodeID().Bytes(),
			Weight:                1,
			Balance:               units.Avax,
			Signer:                *initialPoP,
			RemainingBalanceOwner: message.PChainOwner{},
			DeactivationOwner:     message.PChainOwner{},
		}},
	)
	require.NoError(err)

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(err)

	_, _, _, err = StandardTx(
		&env.backend,
		state.PickFeeCalculator(env.config, env.state),
		convertSubnetToL1Tx,
		diff,
	)
	require.NoError(err)
	require.NoError(diff.Apply(env.state))
	require.NoError(env.state.Commit())

	var (
		nodeID           = ids.GenerateTestNodeID()
		lastAcceptedTime = env.state.GetTimestamp()
		expiryTime       = lastAcceptedTime.Add(5 * time.Minute)
		expiry           = uint64(expiryTime.Unix()) // The warp message will expire in 5 minutes
	)

	const weight = 1

	// Create the Warp message
	sk, err := localsigner.New()
	require.NoError(err)

	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(err)

	var (
		remainingBalanceOwner = message.PChainOwner{}
		deactivationOwner     = message.PChainOwner{}
	)

	addressedCallPayload := must[*message.RegisterL1Validator](t)(message.NewRegisterL1Validator(
		subnetID,
		nodeID,
		pop.PublicKey,
		expiry,
		remainingBalanceOwner,
		deactivationOwner,
		weight,
	))
	unsignedWarp := must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
		env.ctx.NetworkID,
		chainID,
		must[*payload.AddressedCall](t)(payload.NewAddressedCall(
			address,
			addressedCallPayload.Bytes(),
		)).Bytes(),
	))
	sig, err := sk.Sign(unsignedWarp.Bytes())
	require.NoError(err)

	warpMessage := must[*warp.Message](t)(warp.NewMessage(
		unsignedWarp,
		&warp.BitSetSignature{
			Signers:   set.NewBits(0).Bytes(),
			Signature: ([bls.SignatureLen]byte)(bls.SignatureToBytes(sig)),
		},
	))

	wallet := newWallet(t, env, walletConfig{})
	stx, err := wallet.IssueRegisterL1ValidatorTx(
		0, // balance
		pop.ProofOfPossession,
		warpMessage.Bytes(),
	)
	require.NoError(err)

	diff, err = state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, env.state)
	_, _, _, err = StandardTx(
		&env.backend,
		feeCalculator,
		stx,
		diff,
	)
	require.NoError(err)

	requireBaseTxApplied(t, env, diff, feeCalculator, stx)

	// assert that the L1 validator was added
	remainingBalanceOwnerBytes, err := txs.Codec.Marshal(txs.CodecVersion, &remainingBalanceOwner)
	require.NoError(err)

	deactivationOwnerBytes, err := txs.Codec.Marshal(txs.CodecVersion, &deactivationOwner)
	require.NoError(err)

	validationID := addressedCallPayload.ValidationID()
	gotL1Validator, err := diff.GetL1Validator(validationID)
	require.NoError(err)
	require.Equal(
		state.L1Validator{
			ValidationID:          validationID,
			SubnetID:              subnetID,
			NodeID:                nodeID,
			PublicKey:             bls.PublicKeyToUncompressedBytes(sk.PublicKey()),
			RemainingBalanceOwner: remainingBalanceOwnerBytes,
			DeactivationOwner:     deactivationOwnerBytes,
			StartTime:             uint64(diff.GetTimestamp().Unix()),
			Weight:                weight,
			MinNonce:              0,
			EndAccumulatedFee:     0, // The validator was registered with no balance
		},
		gotL1Validator,
	)

	// assert that the warp message can't be replayed
	hasExpiry, err := diff.HasExpiry(state.ExpiryEntry{
		Timestamp:    expiry,
		ValidationID: validationID,
	})
	require.NoError(err)
	require.True(hasExpiry)
}

// TestStandardExecutorSetL1ValidatorWeightTxErrors verifies the failure cases
// of [txs.SetL1ValidatorWeightTx] execution.
func TestStandardExecutorSetL1ValidatorWeightTxErrors(t *testing.T) {
	env := newEnvironment(t, upgradetest.Latest)
	// Charge non-zero fees so the txs are funded with real inputs and outputs.
	env.config.DynamicFeeConfig = genesis.LocalParams.DynamicFeeConfig
	env.config.ValidatorFeeConfig = genesis.LocalParams.ValidatorFeeConfig

	sk, err := localsigner.New()
	require.NoError(t, err)

	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(t, err)

	const (
		initialWeight = 1
		balance       = units.Avax
	)
	var (
		subnetID     = testSubnet1.ID()
		chainID      = ids.GenerateTestID()
		address      = utils.RandomBytes(32)
		validationID = subnetID.Append(0)
	)

	// Convert the subnet to an L1 with one active validator
	convertWallet := newWallet(t, env, walletConfig{})
	convertSubnetToL1Tx, err := convertWallet.IssueConvertSubnetToL1Tx(
		subnetID,
		chainID,
		address,
		[]*txs.ConvertSubnetToL1Validator{{
			NodeID:                ids.GenerateTestNodeID().Bytes(),
			Weight:                initialWeight,
			Balance:               balance,
			Signer:                *pop,
			RemainingBalanceOwner: message.PChainOwner{},
			DeactivationOwner:     message.PChainOwner{},
		}},
	)
	require.NoError(t, err)

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(t, err)

	_, _, _, err = StandardTx(
		&env.backend,
		state.PickFeeCalculator(env.config, env.state),
		convertSubnetToL1Tx,
		diff,
	)
	require.NoError(t, err)
	require.NoError(t, diff.Apply(env.state))
	require.NoError(t, env.state.Commit())

	initialL1Validator, err := env.state.GetL1Validator(validationID)
	require.NoError(t, err)

	// Create the Warp messages
	const (
		nonce  = 1
		weight = initialWeight + 1
	)
	unsignedIncreaseWeightWarpMessage := must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
		env.ctx.NetworkID,
		chainID,
		must[*payload.AddressedCall](t)(payload.NewAddressedCall(
			address,
			must[*message.L1ValidatorWeight](t)(message.NewL1ValidatorWeight(
				validationID,
				nonce,
				weight,
			)).Bytes(),
		)).Bytes(),
	))
	sig, err := sk.Sign(unsignedIncreaseWeightWarpMessage.Bytes())
	require.NoError(t, err)

	warpSignature := &warp.BitSetSignature{
		Signers:   set.NewBits(0).Bytes(),
		Signature: ([bls.SignatureLen]byte)(bls.SignatureToBytes(sig)),
	}
	increaseWeightWarpMessage := must[*warp.Message](t)(warp.NewMessage(
		unsignedIncreaseWeightWarpMessage,
		warpSignature,
	))

	// newWarpMessageBytes wraps an addressed-call payload into warp message
	// bytes. The executor doesn't verify the warp signature, so reusing
	// warpSignature for every payload is fine.
	newWarpMessageBytes := func(t *testing.T, payloadBytes []byte) []byte {
		return must[*warp.Message](t)(warp.NewMessage(
			must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
				env.ctx.NetworkID,
				chainID,
				must[*payload.AddressedCall](t)(payload.NewAddressedCall(
					address,
					payloadBytes,
				)).Bytes(),
			)),
			warpSignature,
		)).Bytes()
	}
	newL1ValidatorWeightMessageBytes := func(t *testing.T, validationID ids.ID, nonce, weight uint64) []byte {
		return newWarpMessageBytes(
			t,
			must[*message.L1ValidatorWeight](t)(message.NewL1ValidatorWeight(
				validationID,
				nonce,
				weight,
			)).Bytes(),
		)
	}
	removeValidatorWarpMessageBytes := newL1ValidatorWeightMessageBytes(t, validationID, nonce, 0)

	// putL1Validator adds another L1 validator to the subnet with the given
	// weight.
	putL1Validator := func(t *testing.T, diff *state.Diff, weight uint64) {
		l1Validator := initialL1Validator
		l1Validator.ValidationID = ids.GenerateTestID()
		l1Validator.NodeID = ids.GenerateTestNodeID()
		l1Validator.Weight = weight
		require.NoError(t, diff.PutL1Validator(l1Validator))
	}

	tests := []struct {
		name        string
		want        error
		updateTx    func(*testing.T, *txs.Tx)
		updateState func(*testing.T, *state.Diff)
	}{
		{
			name: "invalid_prior_to_etna",
			updateState: func(_ *testing.T, diff *state.Diff) {
				diff.SetTimestamp(env.config.UpgradeConfig.EtnaTime.Add(-1 * time.Second))
			},
			want: errEtnaUpgradeNotActive,
		},
		{
			name: "tx_fails_syntactic_verification",
			updateTx: func(_ *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.SetL1ValidatorWeightTx).BaseTx.BlockchainID = ids.GenerateTestID()
			},
			want: avax.ErrWrongChainID,
		},
		{
			name: "invalid_memo_length",
			updateTx: func(_ *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.SetL1ValidatorWeightTx).Memo = []byte("memo!")
			},
			want: avax.ErrMemoTooLarge,
		},
		{
			name: "flow_checker_failed",
			updateTx: func(_ *testing.T, tx *txs.Tx) {
				// Produce more AVAX than the tx consumes
				unsignedTx := tx.Unsigned.(*txs.SetL1ValidatorWeightTx)
				unsignedTx.Outs = append(unsignedTx.Outs, &avax.TransferableOutput{
					Asset: avax.Asset{ID: env.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: math.MaxUint64 / 2,
					},
				})
			},
			want: utxo.ErrInsufficientUnlockedFunds,
		},
		{
			name: "invalid_warp_message",
			updateTx: func(_ *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.SetL1ValidatorWeightTx).Message = []byte{}
			},
			want: codec.ErrCantUnpackVersion,
		},
		{
			name: "invalid_warp_payload",
			updateTx: func(t *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.SetL1ValidatorWeightTx).Message = must[*warp.Message](t)(warp.NewMessage(
					must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
						env.ctx.NetworkID,
						chainID,
						must[*payload.Hash](t)(payload.NewHash(ids.Empty)).Bytes(),
					)),
					warpSignature,
				)).Bytes()
			},
			want: payload.ErrWrongType,
		},
		{
			name: "invalid_addressed_call",
			updateTx: func(t *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.SetL1ValidatorWeightTx).Message = newWarpMessageBytes(
					t,
					must[*message.SubnetToL1Conversion](t)(message.NewSubnetToL1Conversion(ids.Empty)).Bytes(),
				)
			},
			want: message.ErrWrongType,
		},
		{
			name: "invalid_addressed_call_payload",
			updateTx: func(t *testing.T, tx *txs.Tx) {
				// A non-zero weight can't use the nonce reserved for removal
				tx.Unsigned.(*txs.SetL1ValidatorWeightTx).Message = newL1ValidatorWeightMessageBytes(t, validationID, math.MaxUint64, 1)
			},
			want: message.ErrNonceReservedForRemoval,
		},
		{
			name: "l1_validator_not_found",
			updateTx: func(t *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.SetL1ValidatorWeightTx).Message = newL1ValidatorWeightMessageBytes(t, ids.GenerateTestID(), nonce, weight)
			},
			want: errCouldNotLoadL1Validator,
		},
		{
			name: "nonce_too_low",
			updateState: func(t *testing.T, diff *state.Diff) {
				l1Validator := initialL1Validator
				l1Validator.MinNonce = nonce + 1
				require.NoError(t, diff.PutL1Validator(l1Validator))
			},
			want: errWarpMessageContainsStaleNonce,
		},
		{
			name: "invalid_source_chain",
			updateState: func(_ *testing.T, diff *state.Diff) {
				diff.SetSubnetToL1Conversion(subnetID, state.SubnetToL1Conversion{})
			},
			want: errWrongWarpMessageSourceChainID,
		},
		{
			name: "invalid_source_address",
			updateState: func(_ *testing.T, diff *state.Diff) {
				diff.SetSubnetToL1Conversion(subnetID, state.SubnetToL1Conversion{
					ChainID: chainID,
				})
			},
			want: errWrongWarpMessageSourceAddress,
		},
		{
			name: "remove_last_validator",
			updateTx: func(_ *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.SetL1ValidatorWeightTx).Message = removeValidatorWarpMessageBytes
			},
			want: errRemovingLastValidator,
		},
		{
			name: "should_have_been_previously_deactivated",
			updateTx: func(_ *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.SetL1ValidatorWeightTx).Message = removeValidatorWarpMessageBytes
			},
			updateState: func(t *testing.T, diff *state.Diff) {
				// Add another validator to allow removal
				putL1Validator(t, diff, 1)
				// The validator's balance has been fully consumed by fees
				diff.SetAccruedFees(initialL1Validator.EndAccumulatedFee)
			},
			want: errStateCorruption,
		},
		{
			name: "l1_weight_overflow",
			updateState: func(t *testing.T, diff *state.Diff) {
				putL1Validator(t, diff, math.MaxUint64-initialWeight)
			},
			want: safemath.ErrOverflow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			// Fees are non-zero, so issue each tx from a fresh wallet to keep
			// the wallet's UTXO view consistent with env.state.
			wallet := newWallet(t, env, walletConfig{})
			tx, err := wallet.IssueSetL1ValidatorWeightTx(
				increaseWeightWarpMessage.Bytes(),
			)
			require.NoError(err)

			diff, got := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(got)

			if tt.updateTx != nil {
				tt.updateTx(t, tx)
			}

			if tt.updateState != nil {
				tt.updateState(t, diff)
			}

			feeCalculator := state.PickFeeCalculator(env.config, env.state)
			_, _, _, got = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				diff,
			)

			require.ErrorIs(got, tt.want)
		})
	}
}

// TestStandardExecutorSetL1ValidatorWeightTx verifies the successful
// execution of a [txs.SetL1ValidatorWeightTx].
func TestStandardExecutorSetL1ValidatorWeightTx(t *testing.T) {
	env := newEnvironment(t, upgradetest.Latest)
	// Charge non-zero fees so the txs are funded with real inputs and outputs.
	env.config.DynamicFeeConfig = genesis.LocalParams.DynamicFeeConfig
	env.config.ValidatorFeeConfig = genesis.LocalParams.ValidatorFeeConfig

	sk, err := localsigner.New()
	require.NoError(t, err)

	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(t, err)

	const (
		initialWeight = 1
		balance       = units.Avax
	)
	var (
		subnetID  = testSubnet1.ID()
		chainID   = ids.GenerateTestID()
		address   = utils.RandomBytes(32)
		validator = &txs.ConvertSubnetToL1Validator{
			NodeID:  ids.GenerateTestNodeID().Bytes(),
			Weight:  initialWeight,
			Balance: balance,
			Signer:  *pop,
			// RemainingBalanceOwner and DeactivationOwner are initialized so
			// that later reflect based equality checks pass.
			RemainingBalanceOwner: message.PChainOwner{
				Threshold: 0,
				Addresses: []ids.ShortID{},
			},
			DeactivationOwner: message.PChainOwner{
				Threshold: 0,
				Addresses: []ids.ShortID{},
			},
		}
		validationID = subnetID.Append(0)
	)

	// Convert the subnet to an L1 with one active validator
	convertWallet := newWallet(t, env, walletConfig{})
	convertSubnetToL1Tx, err := convertWallet.IssueConvertSubnetToL1Tx(
		subnetID,
		chainID,
		address,
		[]*txs.ConvertSubnetToL1Validator{validator},
	)
	require.NoError(t, err)

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(t, err)

	_, _, _, err = StandardTx(
		&env.backend,
		state.PickFeeCalculator(env.config, env.state),
		convertSubnetToL1Tx,
		diff,
	)
	require.NoError(t, err)
	require.NoError(t, diff.Apply(env.state))
	require.NoError(t, env.state.Commit())

	initialL1Validator, err := env.state.GetL1Validator(validationID)
	require.NoError(t, err)

	// Create the Warp messages
	const (
		nonce  = 1
		weight = initialWeight + 1
	)
	unsignedIncreaseWeightWarpMessage := must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
		env.ctx.NetworkID,
		chainID,
		must[*payload.AddressedCall](t)(payload.NewAddressedCall(
			address,
			must[*message.L1ValidatorWeight](t)(message.NewL1ValidatorWeight(
				validationID,
				nonce,
				weight,
			)).Bytes(),
		)).Bytes(),
	))
	sig, err := sk.Sign(unsignedIncreaseWeightWarpMessage.Bytes())
	require.NoError(t, err)

	warpSignature := &warp.BitSetSignature{
		Signers:   set.NewBits(0).Bytes(),
		Signature: ([bls.SignatureLen]byte)(bls.SignatureToBytes(sig)),
	}
	increaseWeightWarpMessage := must[*warp.Message](t)(warp.NewMessage(
		unsignedIncreaseWeightWarpMessage,
		warpSignature,
	))

	// newRemoveValidatorWarpMessageBytes builds a warp message that removes
	// the validator with the given nonce. The executor doesn't verify the warp
	// signature, so reusing warpSignature is fine.
	newRemoveValidatorWarpMessageBytes := func(t *testing.T, nonce uint64) []byte {
		return must[*warp.Message](t)(warp.NewMessage(
			must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
				env.ctx.NetworkID,
				chainID,
				must[*payload.AddressedCall](t)(payload.NewAddressedCall(
					address,
					must[*message.L1ValidatorWeight](t)(message.NewL1ValidatorWeight(
						validationID,
						nonce,
						0,
					)).Bytes(),
				)).Bytes(),
			)),
			warpSignature,
		)).Bytes()
	}

	// putL1Validator adds another L1 validator to the subnet to allow the
	// initial validator to be removed.
	putL1Validator := func(t *testing.T, diff *state.Diff) {
		l1Validator := initialL1Validator
		l1Validator.ValidationID = ids.GenerateTestID()
		l1Validator.NodeID = ids.GenerateTestNodeID()
		require.NoError(t, diff.PutL1Validator(l1Validator))
	}
	// deactivateInitialL1Validator marks the initial validator as inactive.
	deactivateInitialL1Validator := func(t *testing.T, diff *state.Diff) {
		l1Validator := initialL1Validator
		l1Validator.EndAccumulatedFee = 0
		require.NoError(t, diff.PutL1Validator(l1Validator))
	}

	tests := []struct {
		name                   string
		updateTx               func(*testing.T, *txs.Tx)
		updateState            func(*testing.T, *state.Diff)
		wantNonce              uint64
		wantWeight             uint64
		wantRemainingFundsUTXO *avax.UTXO
	}{
		{
			name: "remove_deactivated_validator",
			updateTx: func(t *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.SetL1ValidatorWeightTx).Message = newRemoveValidatorWarpMessageBytes(t, nonce)
			},
			updateState: func(t *testing.T, diff *state.Diff) {
				putL1Validator(t, diff)
				deactivateInitialL1Validator(t, diff)
			},
		},
		{
			name: "remove_deactivated_validator_with_nonce_overflow",
			updateTx: func(t *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.SetL1ValidatorWeightTx).Message = newRemoveValidatorWarpMessageBytes(t, math.MaxUint64)
			},
			updateState: func(t *testing.T, diff *state.Diff) {
				putL1Validator(t, diff)
				deactivateInitialL1Validator(t, diff)
			},
		},
		{
			name: "remove_active_validator",
			updateTx: func(t *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.SetL1ValidatorWeightTx).Message = newRemoveValidatorWarpMessageBytes(t, nonce)
			},
			updateState: putL1Validator,
			wantRemainingFundsUTXO: &avax.UTXO{
				Asset: avax.Asset{
					ID: env.ctx.AVAXAssetID,
				},
				Out: &secp256k1fx.TransferOutput{
					Amt: balance,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: validator.RemainingBalanceOwner.Threshold,
						Addrs:     validator.RemainingBalanceOwner.Addresses,
					},
				},
			},
		},
		{
			name:       "update_validator",
			wantNonce:  nonce + 1,
			wantWeight: weight,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			// Fees are non-zero, so issue each tx from a fresh wallet to keep
			// the wallet's UTXO view consistent with env.state.
			wallet := newWallet(t, env, walletConfig{})
			tx, err := wallet.IssueSetL1ValidatorWeightTx(
				increaseWeightWarpMessage.Bytes(),
			)
			require.NoError(err)

			diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(err)

			if tt.updateTx != nil {
				tt.updateTx(t, tx)
			}

			if tt.updateState != nil {
				tt.updateState(t, diff)
			}

			feeCalculator := state.PickFeeCalculator(env.config, env.state)
			_, _, _, err = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				diff,
			)
			require.NoError(err)

			requireBaseTxApplied(t, env, diff, feeCalculator, tx)

			baseTxOutputUTXOs := tx.UTXOs()

			gotL1Validator, err := diff.GetL1Validator(validationID)
			if tt.wantWeight != 0 {
				// assert the validator's weight and nonce were updated
				require.NoError(err)

				wantL1Validator := initialL1Validator
				wantL1Validator.MinNonce = tt.wantNonce
				wantL1Validator.Weight = tt.wantWeight
				require.Equal(wantL1Validator, gotL1Validator)
				return
			}

			// assert the validator was removed
			require.ErrorIs(err, database.ErrNotFound)

			// assert the remaining balance was refunded only if the validator
			// was active
			utxoID := avax.UTXOID{
				TxID:        tx.ID(),
				OutputIndex: uint32(len(baseTxOutputUTXOs)),
			}
			gotUTXO, err := diff.GetUTXO(utxoID.InputID())
			if tt.wantRemainingFundsUTXO == nil {
				require.ErrorIs(err, database.ErrNotFound)
				return
			}
			require.NoError(err)

			wantUTXO := *tt.wantRemainingFundsUTXO
			wantUTXO.UTXOID = utxoID
			require.Equal(&wantUTXO, gotUTXO)
		})
	}
}

// TestStandardExecutorIncreaseL1ValidatorBalanceTxErrors verifies the failure
// cases of [txs.IncreaseL1ValidatorBalanceTx] execution.
func TestStandardExecutorIncreaseL1ValidatorBalanceTxErrors(t *testing.T) {
	env := newEnvironment(t, upgradetest.Latest)
	// Charge non-zero fees and allow a single active L1 validator.
	env.config.DynamicFeeConfig = genesis.LocalParams.DynamicFeeConfig
	env.config.ValidatorFeeConfig = genesis.LocalParams.ValidatorFeeConfig
	env.config.ValidatorFeeConfig.Capacity = 1

	sk, err := localsigner.New()
	require.NoError(t, err)

	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(t, err)

	var (
		subnetID     = testSubnet1.ID()
		validationID = subnetID.Append(0)
	)

	// Convert the subnet to an L1 with one inactive validator
	convertWallet := newWallet(t, env, walletConfig{})
	convertSubnetToL1Tx, err := convertWallet.IssueConvertSubnetToL1Tx(
		subnetID,
		ids.GenerateTestID(),
		utils.RandomBytes(32),
		[]*txs.ConvertSubnetToL1Validator{{
			NodeID:                ids.GenerateTestNodeID().Bytes(),
			Weight:                1,
			Balance:               0,
			Signer:                *pop,
			RemainingBalanceOwner: message.PChainOwner{},
			DeactivationOwner:     message.PChainOwner{},
		}},
	)
	require.NoError(t, err)

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(t, err)

	_, _, _, err = StandardTx(
		&env.backend,
		state.PickFeeCalculator(env.config, env.state),
		convertSubnetToL1Tx,
		diff,
	)
	require.NoError(t, err)
	require.NoError(t, diff.Apply(env.state))
	require.NoError(t, env.state.Commit())

	const balanceIncrease = units.NanoAvax
	tests := []struct {
		name        string
		want        error
		updateTx    func(*testing.T, *txs.Tx)
		updateState func(*testing.T, *state.Diff)
	}{
		{
			name: "invalid_prior_to_etna",
			updateState: func(_ *testing.T, diff *state.Diff) {
				diff.SetTimestamp(env.config.UpgradeConfig.EtnaTime.Add(-1 * time.Second))
			},
			want: errEtnaUpgradeNotActive,
		},
		{
			name: "tx_fails_syntactic_verification",
			updateTx: func(_ *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.IncreaseL1ValidatorBalanceTx).BaseTx.BlockchainID = ids.GenerateTestID()
			},
			want: avax.ErrWrongChainID,
		},
		{
			name: "invalid_memo_length",
			updateTx: func(_ *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.IncreaseL1ValidatorBalanceTx).Memo = []byte("memo!")
			},
			want: avax.ErrMemoTooLarge,
		},
		{
			name: "fee_overflow",
			updateTx: func(_ *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.IncreaseL1ValidatorBalanceTx).Balance = math.MaxUint64
			},
			want: safemath.ErrOverflow,
		},
		{
			name: "flow_checker_failed",
			updateTx: func(_ *testing.T, tx *txs.Tx) {
				// Produce more AVAX than the tx consumes
				unsignedTx := tx.Unsigned.(*txs.IncreaseL1ValidatorBalanceTx)
				unsignedTx.Outs = append(unsignedTx.Outs, &avax.TransferableOutput{
					Asset: avax.Asset{ID: env.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: math.MaxUint64 / 2,
					},
				})
			},
			want: utxo.ErrInsufficientUnlockedFunds,
		},
		{
			name: "unknown_validation_id",
			updateTx: func(_ *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.IncreaseL1ValidatorBalanceTx).ValidationID = ids.GenerateTestID()
			},
			want: database.ErrNotFound,
		},
		{
			name: "too_many_active_validators",
			updateState: func(t *testing.T, diff *state.Diff) {
				// Fill the active validator capacity with L1 validators of
				// another subnet
				for range env.config.ValidatorFeeConfig.Capacity {
					require.NoError(t, diff.PutL1Validator(state.L1Validator{
						ValidationID:      ids.GenerateTestID(),
						SubnetID:          ids.GenerateTestID(),
						NodeID:            ids.GenerateTestNodeID(),
						Weight:            1,
						EndAccumulatedFee: 1, // Active
					}))
				}
			},
			want: errMaxNumActiveValidators,
		},
		{
			name: "accumulated_fees_overflow",
			updateState: func(_ *testing.T, diff *state.Diff) {
				diff.SetAccruedFees(math.MaxUint64)
			},
			want: safemath.ErrOverflow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			// Fees are non-zero, so issue each tx from a fresh wallet to keep
			// the wallet's UTXO view consistent with env.state.
			wallet := newWallet(t, env, walletConfig{})
			tx, err := wallet.IssueIncreaseL1ValidatorBalanceTx(
				validationID,
				balanceIncrease,
			)
			require.NoError(err)

			diff, got := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(got)

			if tt.updateTx != nil {
				tt.updateTx(t, tx)
			}

			if tt.updateState != nil {
				tt.updateState(t, diff)
			}

			feeCalculator := state.PickFeeCalculator(env.config, env.state)
			_, _, _, got = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				diff,
			)

			require.ErrorIs(got, tt.want)
		})
	}
}

// TestStandardExecutorIncreaseL1ValidatorBalanceTx verifies the successful
// execution of a [txs.IncreaseL1ValidatorBalanceTx].
func TestStandardExecutorIncreaseL1ValidatorBalanceTx(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t, upgradetest.Latest)
	// Charge non-zero fees so the txs are funded with real inputs and outputs.
	env.config.DynamicFeeConfig = genesis.LocalParams.DynamicFeeConfig
	env.config.ValidatorFeeConfig = genesis.LocalParams.ValidatorFeeConfig

	sk, err := localsigner.New()
	require.NoError(err)

	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(err)

	var (
		subnetID     = testSubnet1.ID()
		validationID = subnetID.Append(0)
	)

	// Convert the subnet to an L1 with one inactive validator
	convertWallet := newWallet(t, env, walletConfig{})
	convertSubnetToL1Tx, err := convertWallet.IssueConvertSubnetToL1Tx(
		subnetID,
		ids.GenerateTestID(),
		utils.RandomBytes(32),
		[]*txs.ConvertSubnetToL1Validator{{
			NodeID:  ids.GenerateTestNodeID().Bytes(),
			Weight:  1,
			Balance: 0,
			Signer:  *pop,
			// RemainingBalanceOwner and DeactivationOwner are initialized so
			// that later reflect based equality checks pass.
			RemainingBalanceOwner: message.PChainOwner{
				Threshold: 0,
				Addresses: []ids.ShortID{},
			},
			DeactivationOwner: message.PChainOwner{
				Threshold: 0,
				Addresses: []ids.ShortID{},
			},
		}},
	)
	require.NoError(err)

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(err)

	_, _, _, err = StandardTx(
		&env.backend,
		state.PickFeeCalculator(env.config, env.state),
		convertSubnetToL1Tx,
		diff,
	)
	require.NoError(err)
	require.NoError(diff.Apply(env.state))
	require.NoError(env.state.Commit())

	initialL1Validator, err := env.state.GetL1Validator(validationID)
	require.NoError(err)

	const balanceIncrease = units.NanoAvax
	wallet := newWallet(t, env, walletConfig{})
	stx, err := wallet.IssueIncreaseL1ValidatorBalanceTx(
		validationID,
		balanceIncrease,
	)
	require.NoError(err)

	diff, err = state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, env.state)
	_, _, _, err = StandardTx(
		&env.backend,
		feeCalculator,
		stx,
		diff,
	)
	require.NoError(err)

	requireBaseTxApplied(t, env, diff, feeCalculator, stx)

	// assert the validator was activated with the increased balance
	gotL1Validator, err := diff.GetL1Validator(validationID)
	require.NoError(err)

	wantL1Validator := initialL1Validator
	wantL1Validator.EndAccumulatedFee = env.state.GetAccruedFees() + balanceIncrease
	require.Equal(wantL1Validator, gotL1Validator)
}

// TestStandardExecutorDisableL1ValidatorTxErrors verifies the failure cases of
// [txs.DisableL1ValidatorTx] execution.
func TestStandardExecutorDisableL1ValidatorTxErrors(t *testing.T) {
	env := newEnvironment(t, upgradetest.Latest)
	// Charge non-zero fees so the txs are funded with real inputs and outputs.
	env.config.DynamicFeeConfig = genesis.LocalParams.DynamicFeeConfig
	env.config.ValidatorFeeConfig = genesis.LocalParams.ValidatorFeeConfig

	sk, err := localsigner.New()
	require.NoError(t, err)

	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(t, err)

	var (
		subnetID     = testSubnet1.ID()
		validationID = subnetID.Append(0)
	)

	// Convert the subnet to an L1 with one active validator
	convertWallet := newWallet(t, env, walletConfig{})
	convertSubnetToL1Tx, err := convertWallet.IssueConvertSubnetToL1Tx(
		subnetID,
		ids.GenerateTestID(),
		utils.RandomBytes(32),
		[]*txs.ConvertSubnetToL1Validator{{
			NodeID:  ids.GenerateTestNodeID().Bytes(),
			Weight:  1,
			Balance: units.Avax,
			Signer:  *pop,
			RemainingBalanceOwner: message.PChainOwner{
				Threshold: 1,
				Addresses: []ids.ShortID{ids.GenerateTestShortID()},
			},
			DeactivationOwner: message.PChainOwner{
				Threshold: 1,
				Addresses: []ids.ShortID{genesistest.DefaultFundedKeys[0].Address()},
			},
		}},
	)
	require.NoError(t, err)

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(t, err)

	_, _, _, err = StandardTx(
		&env.backend,
		state.PickFeeCalculator(env.config, env.state),
		convertSubnetToL1Tx,
		diff,
	)
	require.NoError(t, err)
	require.NoError(t, diff.Apply(env.state))
	require.NoError(t, env.state.Commit())

	tests := []struct {
		name        string
		want        error
		updateTx    func(*testing.T, *txs.Tx)
		updateState func(*testing.T, *state.Diff)
	}{
		{
			name: "invalid_prior_to_etna",
			updateState: func(_ *testing.T, diff *state.Diff) {
				diff.SetTimestamp(env.config.UpgradeConfig.EtnaTime.Add(-1 * time.Second))
			},
			want: errEtnaUpgradeNotActive,
		},
		{
			name: "tx_fails_syntactic_verification",
			updateTx: func(_ *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.DisableL1ValidatorTx).BaseTx.BlockchainID = ids.GenerateTestID()
			},
			want: avax.ErrWrongChainID,
		},
		{
			name: "invalid_memo_length",
			updateTx: func(_ *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.DisableL1ValidatorTx).Memo = []byte("memo!")
			},
			want: avax.ErrMemoTooLarge,
		},
		{
			name: "l1_validator_not_found",
			updateTx: func(_ *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.DisableL1ValidatorTx).ValidationID = ids.GenerateTestID()
			},
			want: errCouldNotLoadL1Validator,
		},
		{
			name: "not_authorized",
			updateTx: func(_ *testing.T, tx *txs.Tx) {
				tx.Unsigned.(*txs.DisableL1ValidatorTx).DisableAuth.(*secp256k1fx.Input).SigIndices[0] = 123456789
			},
			want: errUnauthorizedModification,
		},
		{
			name: "state_corruption",
			updateState: func(_ *testing.T, diff *state.Diff) {
				// The validator's balance has been fully consumed by fees
				diff.SetAccruedFees(math.MaxUint64)
			},
			want: errStateCorruption,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			// Fees are non-zero, so issue each tx from a fresh wallet to keep
			// the wallet's UTXO view consistent with env.state.
			wallet := newWallet(t, env, walletConfig{
				validationIDs: []ids.ID{validationID},
			})
			tx, err := wallet.IssueDisableL1ValidatorTx(validationID)
			require.NoError(err)

			diff, got := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(got)

			if tt.updateTx != nil {
				tt.updateTx(t, tx)
			}

			if tt.updateState != nil {
				tt.updateState(t, diff)
			}

			feeCalculator := state.PickFeeCalculator(env.config, env.state)
			_, _, _, got = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				diff,
			)

			require.ErrorIs(got, tt.want)
		})
	}
}

// TestStandardExecutorDisableL1ValidatorTx verifies the successful execution
// of a [txs.DisableL1ValidatorTx].
func TestStandardExecutorDisableL1ValidatorTx(t *testing.T) {
	env := newEnvironment(t, upgradetest.Latest)
	// Charge non-zero fees so the txs are funded with real inputs and outputs.
	env.config.DynamicFeeConfig = genesis.LocalParams.DynamicFeeConfig
	env.config.ValidatorFeeConfig = genesis.LocalParams.ValidatorFeeConfig

	sk, err := localsigner.New()
	require.NoError(t, err)

	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(t, err)

	const initialBalance = units.Avax
	var (
		subnetID  = testSubnet1.ID()
		validator = &txs.ConvertSubnetToL1Validator{
			NodeID:  ids.GenerateTestNodeID().Bytes(),
			Weight:  1,
			Balance: initialBalance,
			Signer:  *pop,
			// RemainingBalanceOwner and DeactivationOwner are initialized so
			// that later reflect based equality checks pass.
			RemainingBalanceOwner: message.PChainOwner{
				Threshold: 1,
				Addresses: []ids.ShortID{ids.GenerateTestShortID()},
			},
			DeactivationOwner: message.PChainOwner{
				Threshold: 1,
				Addresses: []ids.ShortID{genesistest.DefaultFundedKeys[0].Address()},
			},
		}
		validationID = subnetID.Append(0)
	)

	// Convert the subnet to an L1 with one active validator
	convertWallet := newWallet(t, env, walletConfig{})
	convertSubnetToL1Tx, err := convertWallet.IssueConvertSubnetToL1Tx(
		subnetID,
		ids.GenerateTestID(),
		utils.RandomBytes(32),
		[]*txs.ConvertSubnetToL1Validator{validator},
	)
	require.NoError(t, err)

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(t, err)

	_, _, _, err = StandardTx(
		&env.backend,
		state.PickFeeCalculator(env.config, env.state),
		convertSubnetToL1Tx,
		diff,
	)
	require.NoError(t, err)
	require.NoError(t, diff.Apply(env.state))
	require.NoError(t, env.state.Commit())

	initialL1Validator, err := env.state.GetL1Validator(validationID)
	require.NoError(t, err)

	tests := []struct {
		name                 string
		updateState          func(*testing.T, *state.Diff)
		wantRemainingBalance uint64
	}{
		{
			name: "already_deactivated",
			updateState: func(t *testing.T, diff *state.Diff) {
				l1Validator := initialL1Validator
				l1Validator.EndAccumulatedFee = 0
				require.NoError(t, diff.PutL1Validator(l1Validator))
			},
			wantRemainingBalance: 0,
		},
		{
			name:                 "deactivate_validator",
			wantRemainingBalance: initialBalance,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			// Fees are non-zero, so issue each tx from a fresh wallet to keep
			// the wallet's UTXO view consistent with env.state.
			wallet := newWallet(t, env, walletConfig{
				validationIDs: []ids.ID{validationID},
			})
			tx, err := wallet.IssueDisableL1ValidatorTx(validationID)
			require.NoError(err)

			diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(err)

			if tt.updateState != nil {
				tt.updateState(t, diff)
			}

			feeCalculator := state.PickFeeCalculator(env.config, env.state)
			_, _, _, err = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				diff,
			)
			require.NoError(err)

			requireBaseTxApplied(t, env, diff, feeCalculator, tx)

			baseTxOutputUTXOs := tx.UTXOs()

			// assert the validator was deactivated
			gotL1Validator, err := diff.GetL1Validator(validationID)
			require.NoError(err)

			wantL1Validator := initialL1Validator
			wantL1Validator.EndAccumulatedFee = 0
			require.Equal(wantL1Validator, gotL1Validator)

			// assert the remaining balance was refunded only if the validator
			// was active
			utxoID := avax.UTXOID{
				TxID:        tx.ID(),
				OutputIndex: uint32(len(baseTxOutputUTXOs)),
			}
			gotUTXO, err := diff.GetUTXO(utxoID.InputID())
			if tt.wantRemainingBalance == 0 {
				require.ErrorIs(err, database.ErrNotFound)
				return
			}
			require.NoError(err)
			require.Equal(
				&avax.UTXO{
					UTXOID: utxoID,
					Asset: avax.Asset{
						ID: env.ctx.AVAXAssetID,
					},
					Out: &secp256k1fx.TransferOutput{
						Amt: tt.wantRemainingBalance,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: validator.RemainingBalanceOwner.Threshold,
							Addrs:     validator.RemainingBalanceOwner.Addresses,
						},
					},
				},
				gotUTXO,
			)
		})
	}
}
