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
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/statetest"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/txstest"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/vms/types"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	safemath "github.com/ava-labs/avalanchego/utils/math"
	txfee "github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	validatorfee "github.com/ava-labs/avalanchego/vms/platformvm/validators/fee"
)

func TestStandardExecutorConvertSubnetToL1Tx(t *testing.T) {
	var (
		fx = &secp256k1fx.Fx{}
		vm = &secp256k1fx.TestVM{
			Log: logging.NoLog{},
		}
	)
	require.NoError(t, fx.InitializeVM(vm))

	var (
		ctx           = snowtest.Context(t, constants.PlatformChainID)
		defaultConfig = &config.Internal{
			DynamicFeeConfig:   genesis.LocalParams.DynamicFeeConfig,
			ValidatorFeeConfig: genesis.LocalParams.ValidatorFeeConfig,
			UpgradeConfig:      upgradetest.GetConfig(upgradetest.Latest),
		}
		baseState = statetest.New(t, statetest.Config{
			Upgrades: defaultConfig.UpgradeConfig,
		})
		wallet = txstest.NewWallet(
			t,
			ctx,
			defaultConfig,
			baseState,
			secp256k1fx.NewKeychain(genesistest.DefaultFundedKeys...),
			nil, // subnetIDs
			nil, // validationIDs
			nil, // chainIDs
		)
		flowChecker = utxo.NewVerifier(
			ctx,
			&vm.Clk,
			fx,
		)
	)

	testAddresses := make([]ids.ShortID, len(genesistest.DefaultFundedKeys))
	for i, testKey := range genesistest.DefaultFundedKeys {
		testAddresses[i] = testKey.Address()
	}

	// Create the subnet
	createSubnetTx, err := wallet.IssueCreateSubnetTx(
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     testAddresses,
		},
	)
	require.NoError(t, err)

	diff, err := state.NewDiffOn(baseState, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(t, err)

	_, _, _, err = StandardTx(
		&Backend{
			Config:       defaultConfig,
			Bootstrapped: utils.NewAtomic(true),
			Fx:           fx,
			FlowChecker:  flowChecker,
			Ctx:          ctx,
		},
		state.PickFeeCalculator(defaultConfig, baseState),
		createSubnetTx,
		diff,
	)
	require.NoError(t, err)
	require.NoError(t, diff.Apply(baseState))
	require.NoError(t, baseState.Commit())

	subnetID := createSubnetTx.ID()
	nodeID := ids.GenerateTestNodeID()
	chainID := ids.GenerateTestID()
	address := utils.RandomBytes(32)

	sk, err := localsigner.New()
	require.NoError(t, err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(t, err)

	const weight = 1

	validator := &platform.ConvertSubnetToL1Validator{
		NodeID:  nodeID.Bytes(),
		Weight:  weight,
		Balance: 1,
		Signer:  *pop,
		RemainingBalanceOwner: message.PChainOwner{
			Threshold: 1,
			Addresses: testAddresses,
		},
		DeactivationOwner: message.PChainOwner{
			Threshold: 1,
			Addresses: testAddresses,
		},
	}

	convertSubnetToL1Tx, err := wallet.IssueConvertSubnetToL1Tx(
		subnetID,
		chainID,
		address,
		[]*platform.ConvertSubnetToL1Validator{validator},
	)
	require.NoError(t, err)

	tests := []struct {
		name           string
		tx             *platform.Tx
		updateExecutor func(executor *standardTxExecutor) error
		expectedErr    error
	}{
		{
			name: "invalid prior to E-Upgrade",
			tx:   convertSubnetToL1Tx,
			updateExecutor: func(e *standardTxExecutor) error {
				e.backend.Config = &config.Internal{
					UpgradeConfig: upgradetest.GetConfig(upgradetest.Durango),
				}
				return nil
			},
			expectedErr: errEtnaUpgradeNotActive,
		},
		{
			name: "tx fails syntactic verification",
			tx:   convertSubnetToL1Tx,
			updateExecutor: func(e *standardTxExecutor) error {
				e.backend.Ctx = snowtest.Context(t, ids.GenerateTestID())
				return nil
			},
			expectedErr: avax.ErrWrongChainID,
		},
		{
			name: "invalid memo length",
			tx: func() *platform.Tx {
				tx, err := wallet.IssueConvertSubnetToL1Tx(
					subnetID,
					chainID,
					address,
					[]*platform.ConvertSubnetToL1Validator{validator},
					common.WithMemo([]byte("memo!")),
				)
				require.NoError(t, err)

				return tx
			}(),
			expectedErr: avax.ErrMemoTooLarge,
		},
		{
			name: "fail subnet authorization",
			tx:   convertSubnetToL1Tx,
			updateExecutor: func(e *standardTxExecutor) error {
				e.state.SetSubnetOwner(subnetID, &secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs: []ids.ShortID{
						ids.GenerateTestShortID(),
					},
				})
				return nil
			},
			expectedErr: errUnauthorizedModification,
		},
		{
			name: "invalid if subnet is transformed",
			tx:   convertSubnetToL1Tx,
			updateExecutor: func(e *standardTxExecutor) error {
				e.state.AddSubnetTransformation(&platform.Tx{Unsigned: &platform.TransformSubnetTx{
					Subnet: subnetID,
				}})
				return nil
			},
			expectedErr: errIsImmutable,
		},
		{
			name: "invalid if subnet is converted",
			tx:   convertSubnetToL1Tx,
			updateExecutor: func(e *standardTxExecutor) error {
				e.state.SetSubnetToL1Conversion(
					subnetID,
					state.SubnetToL1Conversion{
						ConversionID: ids.GenerateTestID(),
						ChainID:      ids.GenerateTestID(),
						Addr:         utils.RandomBytes(32),
					},
				)
				return nil
			},
			expectedErr: errIsImmutable,
		},
		{
			name: "too many active validators",
			tx:   convertSubnetToL1Tx,
			updateExecutor: func(e *standardTxExecutor) error {
				e.backend.Config = &config.Internal{
					DynamicFeeConfig: genesis.LocalParams.DynamicFeeConfig,
					ValidatorFeeConfig: validatorfee.Config{
						Capacity:                 0,
						Target:                   genesis.LocalParams.ValidatorFeeConfig.Target,
						MinPrice:                 genesis.LocalParams.ValidatorFeeConfig.MinPrice,
						ExcessConversionConstant: genesis.LocalParams.ValidatorFeeConfig.ExcessConversionConstant,
					},
					UpgradeConfig: upgradetest.GetConfig(upgradetest.Latest),
				}
				return nil
			},
			expectedErr: errMaxNumActiveValidators,
		},
		{
			name: "invalid L1 validator",
			tx:   convertSubnetToL1Tx,
			updateExecutor: func(e *standardTxExecutor) error {
				return e.state.PutL1Validator(state.L1Validator{
					ValidationID: ids.GenerateTestID(),
					SubnetID:     subnetID,
					NodeID:       nodeID,
					Weight:       1,
				})
			},
			expectedErr: state.ErrDuplicateL1Validator,
		},
		{
			name: "insufficient fee",
			tx:   convertSubnetToL1Tx,
			updateExecutor: func(e *standardTxExecutor) error {
				e.feeCalculator = txfee.NewDynamicCalculator(
					e.backend.Config.DynamicFeeConfig.Weights,
					100*genesis.LocalParams.DynamicFeeConfig.MinPrice,
				)
				return nil
			},
			expectedErr: utxo.ErrInsufficientUnlockedFunds,
		},
		{
			name: "validators balance overflow",
			tx: func() *platform.Tx {
				convertSubnetToL1Tx := &platform.ConvertSubnetToL1Tx{
					BaseTx: platform.BaseTx{
						BaseTx: avax.BaseTx{
							NetworkID:    ctx.NetworkID,
							BlockchainID: ctx.ChainID,
							Ins: []*avax.TransferableInput{
								{
									Asset: avax.Asset{
										ID: ctx.AVAXAssetID,
									},
									In: &secp256k1fx.TransferInput{
										Amt: units.NanoAvax,
									},
								},
							},
						},
					},
					Subnet: subnetID,
					Validators: []*platform.ConvertSubnetToL1Validator{
						{
							NodeID:  types.JSONByteSlice(ids.ShortID{1}.Bytes()),
							Weight:  1,
							Balance: math.MaxUint64,
							Signer:  *pop,
						},
						validator,
					},
					SubnetAuth: &secp256k1fx.Input{
						SigIndices: []uint32{0},
					},
				}

				// Guarantee the validators are sorted by node id so that this tx is
				// valid.
				utils.Sort(convertSubnetToL1Tx.Validators)

				tx, err := platform.NewSignedTx(
					convertSubnetToL1Tx,
					platform.Codec,
					[][]*secp256k1.PrivateKey{{genesistest.DefaultFundedKeys[3]}},
				)
				require.NoError(t, err)

				return tx
			}(),
			expectedErr: safemath.ErrOverflow,
		},
		{
			name: "valid tx",
			tx:   convertSubnetToL1Tx,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			diff, err := state.NewDiffOn(baseState, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(err)

			executor := &standardTxExecutor{
				backend: &Backend{
					Config:       defaultConfig,
					Bootstrapped: utils.NewAtomic(true),
					Fx:           fx,
					FlowChecker:  flowChecker,
					Ctx:          ctx,
				},
				feeCalculator: state.PickFeeCalculator(defaultConfig, baseState),
				tx:            test.tx,
				state:         diff,
			}
			require.NoError(executor.backend.Fx.Bootstrapped())
			if test.updateExecutor != nil {
				require.NoError(test.updateExecutor(executor))
			}

			err = test.tx.Unsigned.Visit(executor)
			require.ErrorIs(err, test.expectedErr)
			if err != nil {
				return
			}

			for utxoID := range test.tx.InputIDs() {
				_, err := diff.GetUTXO(utxoID)
				require.ErrorIs(err, database.ErrNotFound)
			}

			for _, expectedUTXO := range test.tx.UTXOs() {
				utxoID := expectedUTXO.InputID()
				utxo, err := diff.GetUTXO(utxoID)
				require.NoError(err)
				require.Equal(expectedUTXO, utxo)
			}

			expectedConversionID, err := message.SubnetToL1ConversionID(message.SubnetToL1ConversionData{
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

			stateConversion, err := diff.GetSubnetToL1Conversion(subnetID)
			require.NoError(err)
			require.Equal(
				state.SubnetToL1Conversion{
					ConversionID: expectedConversionID,
					ChainID:      chainID,
					Addr:         address,
				},
				stateConversion,
			)

			var (
				validationID = subnetID.Append(0)
				pkBytes      = bls.PublicKeyToUncompressedBytes(sk.PublicKey())
			)
			remainingBalanceOwner, err := platform.Codec.Marshal(platform.CodecVersion, &validator.RemainingBalanceOwner)
			require.NoError(err)

			deactivationOwner, err := platform.Codec.Marshal(platform.CodecVersion, &validator.DeactivationOwner)
			require.NoError(err)

			l1Validator, err := diff.GetL1Validator(validationID)
			require.NoError(err)
			require.Equal(
				state.L1Validator{
					ValidationID:          validationID,
					SubnetID:              subnetID,
					NodeID:                nodeID,
					PublicKey:             pkBytes,
					RemainingBalanceOwner: remainingBalanceOwner,
					DeactivationOwner:     deactivationOwner,
					StartTime:             uint64(diff.GetTimestamp().Unix()),
					Weight:                validator.Weight,
					MinNonce:              0,
					EndAccumulatedFee:     validator.Balance + diff.GetAccruedFees(),
				},
				l1Validator,
			)
		})
	}
}

func TestStandardExecutorRegisterL1ValidatorTx(t *testing.T) {
	var (
		fx = &secp256k1fx.Fx{}
		vm = &secp256k1fx.TestVM{
			Log: logging.NoLog{},
		}
	)
	require.NoError(t, fx.InitializeVM(vm))

	var (
		ctx           = snowtest.Context(t, constants.PlatformChainID)
		defaultConfig = &config.Internal{
			DynamicFeeConfig:   genesis.LocalParams.DynamicFeeConfig,
			ValidatorFeeConfig: genesis.LocalParams.ValidatorFeeConfig,
			UpgradeConfig:      upgradetest.GetConfig(upgradetest.Latest),
		}
		baseState = statetest.New(t, statetest.Config{
			Upgrades: defaultConfig.UpgradeConfig,
			Context:  ctx,
		})
		wallet = txstest.NewWallet(
			t,
			ctx,
			defaultConfig,
			baseState,
			secp256k1fx.NewKeychain(genesistest.DefaultFundedKeys...),
			nil, // subnetIDs
			nil, // validationIDs
			nil, // chainIDs
		)
		flowChecker = utxo.NewVerifier(
			ctx,
			&vm.Clk,
			fx,
		)

		backend = &Backend{
			Config:       defaultConfig,
			Bootstrapped: utils.NewAtomic(true),
			Fx:           fx,
			FlowChecker:  flowChecker,
			Ctx:          ctx,
		}
		feeCalculator = state.PickFeeCalculator(defaultConfig, baseState)
	)

	// Create the initial state
	diff, err := state.NewDiffOn(baseState, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(t, err)

	// Create the subnet
	createSubnetTx, err := wallet.IssueCreateSubnetTx(
		&secp256k1fx.OutputOwners{},
	)
	require.NoError(t, err)

	// Execute the subnet creation
	_, _, _, err = StandardTx(
		backend,
		feeCalculator,
		createSubnetTx,
		diff,
	)
	require.NoError(t, err)

	// Create the subnet conversion
	initialSK, err := localsigner.New()
	require.NoError(t, err)
	initialPoP, err := signer.NewProofOfPossession(initialSK)
	require.NoError(t, err)

	const (
		initialWeight  = 1
		initialBalance = units.Avax
	)
	var (
		subnetID      = createSubnetTx.ID()
		chainID       = ids.GenerateTestID()
		address       = utils.RandomBytes(32)
		initialNodeID = ids.GenerateTestNodeID()
		validator     = &platform.ConvertSubnetToL1Validator{
			NodeID:                initialNodeID.Bytes(),
			Weight:                initialWeight,
			Balance:               initialBalance,
			Signer:                *initialPoP,
			RemainingBalanceOwner: message.PChainOwner{},
			DeactivationOwner:     message.PChainOwner{},
		}
	)
	convertSubnetToL1Tx, err := wallet.IssueConvertSubnetToL1Tx(
		subnetID,
		chainID,
		address,
		[]*platform.ConvertSubnetToL1Validator{
			validator,
		},
	)
	require.NoError(t, err)

	// Execute the subnet conversion
	_, _, _, err = StandardTx(
		backend,
		feeCalculator,
		convertSubnetToL1Tx,
		diff,
	)
	require.NoError(t, err)
	require.NoError(t, diff.Apply(baseState))
	require.NoError(t, baseState.Commit())

	var (
		nodeID           = ids.GenerateTestNodeID()
		lastAcceptedTime = baseState.GetTimestamp()
		expiryTime       = lastAcceptedTime.Add(5 * time.Minute)
		expiry           = uint64(expiryTime.Unix()) // The warp message will expire in 5 minutes
	)

	const weight = 1

	// Create the Warp message
	sk, err := localsigner.New()
	require.NoError(t, err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(t, err)
	pk := sk.PublicKey()
	pkBytes := bls.PublicKeyToUncompressedBytes(pk)

	remainingBalanceOwner := message.PChainOwner{}
	remainingBalanceOwnerBytes, err := platform.Codec.Marshal(platform.CodecVersion, &remainingBalanceOwner)
	require.NoError(t, err)

	deactivationOwner := message.PChainOwner{}
	deactivationOwnerBytes, err := platform.Codec.Marshal(platform.CodecVersion, &deactivationOwner)
	require.NoError(t, err)

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
		ctx.NetworkID,
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

	validationID := addressedCallPayload.ValidationID()
	tests := []struct {
		name           string
		balance        uint64
		message        []byte
		builderOptions []common.Option
		updateTx       func(*platform.RegisterL1ValidatorTx)
		updateExecutor func(*standardTxExecutor) error
		expectedErr    error
	}{
		{
			name: "invalid prior to E-Upgrade",
			updateExecutor: func(e *standardTxExecutor) error {
				e.backend.Config = &config.Internal{
					UpgradeConfig: upgradetest.GetConfig(upgradetest.Durango),
				}
				return nil
			},
			expectedErr: errEtnaUpgradeNotActive,
		},
		{
			name: "tx fails syntactic verification",
			updateExecutor: func(e *standardTxExecutor) error {
				e.backend.Ctx = snowtest.Context(t, ids.GenerateTestID())
				return nil
			},
			expectedErr: avax.ErrWrongChainID,
		},
		{
			name: "invalid memo length",
			builderOptions: []common.Option{
				common.WithMemo([]byte("memo!")),
			},
			expectedErr: avax.ErrMemoTooLarge,
		},
		{
			name: "fee calculation overflow",
			updateTx: func(tx *platform.RegisterL1ValidatorTx) {
				tx.Balance = math.MaxUint64
			},
			expectedErr: safemath.ErrOverflow,
		},
		{
			name: "insufficient fee",
			updateExecutor: func(e *standardTxExecutor) error {
				e.feeCalculator = txfee.NewDynamicCalculator(
					e.backend.Config.DynamicFeeConfig.Weights,
					100*genesis.LocalParams.DynamicFeeConfig.MinPrice,
				)
				return nil
			},
			expectedErr: utxo.ErrInsufficientUnlockedFunds,
		},
		{
			name:        "invalid warp message",
			message:     []byte{},
			expectedErr: codec.ErrCantUnpackVersion,
		},
		{
			name: "invalid warp payload",
			message: must[*warp.Message](t)(warp.NewMessage(
				must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
					ctx.NetworkID,
					chainID,
					must[*payload.Hash](t)(payload.NewHash(ids.Empty)).Bytes(),
				)),
				warpSignature,
			)).Bytes(),
			expectedErr: payload.ErrWrongType,
		},
		{
			name: "invalid addressed call",
			message: must[*warp.Message](t)(warp.NewMessage(
				must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
					ctx.NetworkID,
					chainID,
					must[*payload.AddressedCall](t)(payload.NewAddressedCall(
						address,
						must[*message.SubnetToL1Conversion](t)(message.NewSubnetToL1Conversion(ids.Empty)).Bytes(),
					)).Bytes(),
				)),
				warpSignature,
			)).Bytes(),
			expectedErr: message.ErrWrongType,
		},
		{
			name: "invalid addressed call payload",
			message: must[*warp.Message](t)(warp.NewMessage(
				must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
					ctx.NetworkID,
					chainID,
					must[*payload.AddressedCall](t)(payload.NewAddressedCall(
						address,
						must[*message.RegisterL1Validator](t)(message.NewRegisterL1Validator(
							subnetID,
							nodeID,
							pop.PublicKey,
							expiry,
							remainingBalanceOwner,
							deactivationOwner,
							0, // weight = 0 is invalid
						)).Bytes(),
					)).Bytes(),
				)),
				warpSignature,
			)).Bytes(),
			expectedErr: message.ErrInvalidWeight,
		},
		{
			name: "subnet conversion not found",
			message: must[*warp.Message](t)(warp.NewMessage(
				must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
					ctx.NetworkID,
					chainID,
					must[*payload.AddressedCall](t)(payload.NewAddressedCall(
						address,
						must[*message.RegisterL1Validator](t)(message.NewRegisterL1Validator(
							ids.GenerateTestID(), // invalid subnetID
							nodeID,
							pop.PublicKey,
							expiry,
							remainingBalanceOwner,
							deactivationOwner,
							weight,
						)).Bytes(),
					)).Bytes(),
				)),
				warpSignature,
			)).Bytes(),
			expectedErr: errCouldNotLoadSubnetToL1Conversion,
		},
		{
			name: "invalid source chain",
			updateExecutor: func(e *standardTxExecutor) error {
				e.state.SetSubnetToL1Conversion(subnetID, state.SubnetToL1Conversion{})
				return nil
			},
			expectedErr: errWrongWarpMessageSourceChainID,
		},
		{
			name: "invalid source address",
			updateExecutor: func(e *standardTxExecutor) error {
				e.state.SetSubnetToL1Conversion(subnetID, state.SubnetToL1Conversion{
					ChainID: chainID,
				})
				return nil
			},
			expectedErr: errWrongWarpMessageSourceAddress,
		},
		{
			name: "message expired",
			updateExecutor: func(e *standardTxExecutor) error {
				e.state.SetTimestamp(expiryTime)
				return nil
			},
			expectedErr: errWarpMessageExpired,
		},
		{
			name: "message expiry too far in the future",
			message: must[*warp.Message](t)(warp.NewMessage(
				must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
					ctx.NetworkID,
					chainID,
					must[*payload.AddressedCall](t)(payload.NewAddressedCall(
						address,
						must[*message.RegisterL1Validator](t)(message.NewRegisterL1Validator(
							subnetID,
							nodeID,
							pop.PublicKey,
							math.MaxUint64, // expiry too far in the future
							remainingBalanceOwner,
							deactivationOwner,
							weight,
						)).Bytes(),
					)).Bytes(),
				)),
				warpSignature,
			)).Bytes(),
			expectedErr: errWarpMessageNotYetAllowed,
		},
		{
			name:    "L1 Validator previously registered",
			balance: 1,
			updateExecutor: func(e *standardTxExecutor) error {
				e.state.PutExpiry(state.ExpiryEntry{
					Timestamp:    expiry,
					ValidationID: validationID,
				})
				return nil
			},
			expectedErr: errWarpMessageAlreadyIssued,
		},
		{
			name: "invalid PoP",
			message: must[*warp.Message](t)(warp.NewMessage(
				must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
					ctx.NetworkID,
					chainID,
					must[*payload.AddressedCall](t)(payload.NewAddressedCall(
						address,
						must[*message.RegisterL1Validator](t)(message.NewRegisterL1Validator(
							subnetID,
							nodeID,
							initialPoP.PublicKey, // Wrong public key
							expiry,
							remainingBalanceOwner,
							deactivationOwner,
							weight,
						)).Bytes(),
					)).Bytes(),
				)),
				warpSignature,
			)).Bytes(),
			expectedErr: signer.ErrInvalidProofOfPossession,
		},
		{
			name:    "too many active validators",
			balance: 1,
			updateExecutor: func(e *standardTxExecutor) error {
				e.backend.Config = &config.Internal{
					DynamicFeeConfig: genesis.LocalParams.DynamicFeeConfig,
					ValidatorFeeConfig: validatorfee.Config{
						Capacity:                 0,
						Target:                   genesis.LocalParams.ValidatorFeeConfig.Target,
						MinPrice:                 genesis.LocalParams.ValidatorFeeConfig.MinPrice,
						ExcessConversionConstant: genesis.LocalParams.ValidatorFeeConfig.ExcessConversionConstant,
					},
					UpgradeConfig: upgradetest.GetConfig(upgradetest.Latest),
				}
				return nil
			},
			expectedErr: errMaxNumActiveValidators,
		},
		{
			name:    "accrued fees overflow",
			balance: 1,
			updateExecutor: func(e *standardTxExecutor) error {
				e.state.SetAccruedFees(math.MaxUint64)
				return nil
			},
			expectedErr: safemath.ErrOverflow,
		},
		{
			name: "duplicate subnetID + nodeID pair",
			updateExecutor: func(e *standardTxExecutor) error {
				return e.state.PutL1Validator(state.L1Validator{
					ValidationID: ids.GenerateTestID(),
					SubnetID:     subnetID,
					NodeID:       nodeID,
					PublicKey:    bls.PublicKeyToUncompressedBytes(initialSK.PublicKey()),
					Weight:       1,
				})
			},
			expectedErr: state.ErrDuplicateL1Validator,
		},
		{
			name: "valid tx",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			// Create the RegisterL1ValidatorTx
			wallet := txstest.NewWallet(
				t,
				ctx,
				defaultConfig,
				baseState,
				secp256k1fx.NewKeychain(genesistest.DefaultFundedKeys...),
				nil, // subnetIDs
				nil, // validationIDs
				nil, // chainIDs
			)

			registerL1ValidatorTx, err := wallet.IssueRegisterL1ValidatorTx(
				test.balance,
				pop.ProofOfPossession,
				warpMessage.Bytes(),
				test.builderOptions...,
			)
			require.NoError(err)

			unsignedTx := registerL1ValidatorTx.Unsigned.(*platform.RegisterL1ValidatorTx)
			if test.message != nil {
				unsignedTx.Message = test.message
			}
			if test.updateTx != nil {
				test.updateTx(unsignedTx)
			}

			diff, err := state.NewDiffOn(baseState, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(err)

			executor := &standardTxExecutor{
				backend: &Backend{
					Config:       defaultConfig,
					Bootstrapped: utils.NewAtomic(true),
					Fx:           fx,
					FlowChecker:  flowChecker,
					Ctx:          ctx,
				},
				feeCalculator: state.PickFeeCalculator(defaultConfig, baseState),
				tx:            registerL1ValidatorTx,
				state:         diff,
			}
			if test.updateExecutor != nil {
				require.NoError(test.updateExecutor(executor))
			}

			err = registerL1ValidatorTx.Unsigned.Visit(executor)
			require.ErrorIs(err, test.expectedErr)
			if err != nil {
				return
			}

			for utxoID := range registerL1ValidatorTx.InputIDs() {
				_, err := diff.GetUTXO(utxoID)
				require.ErrorIs(err, database.ErrNotFound)
			}

			for _, expectedUTXO := range registerL1ValidatorTx.UTXOs() {
				utxoID := expectedUTXO.InputID()
				utxo, err := diff.GetUTXO(utxoID)
				require.NoError(err)
				require.Equal(expectedUTXO, utxo)
			}

			l1Validator, err := diff.GetL1Validator(validationID)
			require.NoError(err)

			var expectedEndAccumulatedFee uint64
			if test.balance != 0 {
				expectedEndAccumulatedFee = test.balance + diff.GetAccruedFees()
			}
			require.Equal(
				state.L1Validator{
					ValidationID:          validationID,
					SubnetID:              subnetID,
					NodeID:                nodeID,
					PublicKey:             pkBytes,
					RemainingBalanceOwner: remainingBalanceOwnerBytes,
					DeactivationOwner:     deactivationOwnerBytes,
					StartTime:             uint64(diff.GetTimestamp().Unix()),
					Weight:                weight,
					MinNonce:              0,
					EndAccumulatedFee:     expectedEndAccumulatedFee,
				},
				l1Validator,
			)

			hasExpiry, err := diff.HasExpiry(state.ExpiryEntry{
				Timestamp:    expiry,
				ValidationID: validationID,
			})
			require.NoError(err)
			require.True(hasExpiry)
		})
	}
}

func TestStandardExecutorSetL1ValidatorWeightTx(t *testing.T) {
	var (
		fx = &secp256k1fx.Fx{}
		vm = &secp256k1fx.TestVM{
			Log: logging.NoLog{},
		}
	)
	require.NoError(t, fx.InitializeVM(vm))

	var (
		ctx           = snowtest.Context(t, constants.PlatformChainID)
		defaultConfig = &config.Internal{
			DynamicFeeConfig:   genesis.LocalParams.DynamicFeeConfig,
			ValidatorFeeConfig: genesis.LocalParams.ValidatorFeeConfig,
			UpgradeConfig:      upgradetest.GetConfig(upgradetest.Latest),
		}
		baseState = statetest.New(t, statetest.Config{
			Upgrades: defaultConfig.UpgradeConfig,
			Context:  ctx,
		})
		wallet = txstest.NewWallet(
			t,
			ctx,
			defaultConfig,
			baseState,
			secp256k1fx.NewKeychain(genesistest.DefaultFundedKeys...),
			nil, // subnetIDs
			nil, // validationIDs
			nil, // chainIDs
		)
		flowChecker = utxo.NewVerifier(
			ctx,
			&vm.Clk,
			fx,
		)

		backend = &Backend{
			Config:       defaultConfig,
			Bootstrapped: utils.NewAtomic(true),
			Fx:           fx,
			FlowChecker:  flowChecker,
			Ctx:          ctx,
		}
		feeCalculator = state.PickFeeCalculator(defaultConfig, baseState)
	)

	// Create the initial state
	diff, err := state.NewDiffOn(baseState, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(t, err)

	// Create the subnet
	createSubnetTx, err := wallet.IssueCreateSubnetTx(
		&secp256k1fx.OutputOwners{},
	)
	require.NoError(t, err)

	// Execute the subnet creation
	_, _, _, err = StandardTx(
		backend,
		feeCalculator,
		createSubnetTx,
		diff,
	)
	require.NoError(t, err)

	// Create the subnet conversion
	sk, err := localsigner.New()
	require.NoError(t, err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(t, err)

	const (
		initialWeight = 1
		balance       = units.Avax
	)
	var (
		subnetID  = createSubnetTx.ID()
		chainID   = ids.GenerateTestID()
		address   = utils.RandomBytes(32)
		validator = &platform.ConvertSubnetToL1Validator{
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

	convertSubnetToL1Tx, err := wallet.IssueConvertSubnetToL1Tx(
		subnetID,
		chainID,
		address,
		[]*platform.ConvertSubnetToL1Validator{
			validator,
		},
	)
	require.NoError(t, err)

	// Execute the subnet conversion
	_, _, _, err = StandardTx(
		backend,
		feeCalculator,
		convertSubnetToL1Tx,
		diff,
	)
	require.NoError(t, err)
	require.NoError(t, diff.Apply(baseState))
	require.NoError(t, baseState.Commit())

	initialL1Validator, err := baseState.GetL1Validator(validationID)
	require.NoError(t, err)

	// Create the Warp messages
	const (
		nonce  = 1
		weight = initialWeight + 1
	)
	unsignedIncreaseWeightWarpMessage := must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
		ctx.NetworkID,
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
	removeValidatorWarpMessage := must[*warp.Message](t)(warp.NewMessage(
		must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
			ctx.NetworkID,
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
	))

	increaseL1Weight := func(weight uint64) func(*standardTxExecutor) error {
		return func(e *standardTxExecutor) error {
			l1Validator := initialL1Validator
			l1Validator.ValidationID = ids.GenerateTestID()
			l1Validator.NodeID = ids.GenerateTestNodeID()
			l1Validator.Weight = weight
			return e.state.PutL1Validator(l1Validator)
		}
	}

	tests := []struct {
		name                       string
		message                    []byte
		builderOptions             []common.Option
		updateExecutor             func(*standardTxExecutor) error
		expectedNonce              uint64
		expectedWeight             uint64
		expectedRemainingFundsUTXO *avax.UTXO
		expectedErr                error
	}{
		{
			name: "invalid prior to E-Upgrade",
			updateExecutor: func(e *standardTxExecutor) error {
				e.backend.Config = &config.Internal{
					UpgradeConfig: upgradetest.GetConfig(upgradetest.Durango),
				}
				return nil
			},
			expectedErr: errEtnaUpgradeNotActive,
		},
		{
			name: "tx fails syntactic verification",
			updateExecutor: func(e *standardTxExecutor) error {
				e.backend.Ctx = snowtest.Context(t, ids.GenerateTestID())
				return nil
			},
			expectedErr: avax.ErrWrongChainID,
		},
		{
			name: "invalid memo length",
			builderOptions: []common.Option{
				common.WithMemo([]byte("memo!")),
			},
			expectedErr: avax.ErrMemoTooLarge,
		},
		{
			name: "insufficient fee",
			updateExecutor: func(e *standardTxExecutor) error {
				e.feeCalculator = txfee.NewDynamicCalculator(
					e.backend.Config.DynamicFeeConfig.Weights,
					100*genesis.LocalParams.DynamicFeeConfig.MinPrice,
				)
				return nil
			},
			expectedErr: utxo.ErrInsufficientUnlockedFunds,
		},
		{
			name:        "invalid warp message",
			message:     []byte{},
			expectedErr: codec.ErrCantUnpackVersion,
		},
		{
			name: "invalid warp payload",
			message: must[*warp.Message](t)(warp.NewMessage(
				must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
					ctx.NetworkID,
					chainID,
					must[*payload.Hash](t)(payload.NewHash(ids.Empty)).Bytes(),
				)),
				warpSignature,
			)).Bytes(),
			expectedErr: payload.ErrWrongType,
		},
		{
			name: "invalid addressed call",
			message: must[*warp.Message](t)(warp.NewMessage(
				must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
					ctx.NetworkID,
					chainID,
					must[*payload.AddressedCall](t)(payload.NewAddressedCall(
						address,
						must[*message.SubnetToL1Conversion](t)(message.NewSubnetToL1Conversion(ids.Empty)).Bytes(),
					)).Bytes(),
				)),
				warpSignature,
			)).Bytes(),
			expectedErr: message.ErrWrongType,
		},
		{
			name: "invalid addressed call payload",
			message: must[*warp.Message](t)(warp.NewMessage(
				must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
					ctx.NetworkID,
					chainID,
					must[*payload.AddressedCall](t)(payload.NewAddressedCall(
						address,
						must[*message.L1ValidatorWeight](t)(message.NewL1ValidatorWeight(
							validationID,
							math.MaxUint64,
							1,
						)).Bytes(),
					)).Bytes(),
				)),
				warpSignature,
			)).Bytes(),
			expectedErr: message.ErrNonceReservedForRemoval,
		},
		{
			name: "L1 validator not found",
			message: must[*warp.Message](t)(warp.NewMessage(
				must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
					ctx.NetworkID,
					chainID,
					must[*payload.AddressedCall](t)(payload.NewAddressedCall(
						address,
						must[*message.L1ValidatorWeight](t)(message.NewL1ValidatorWeight(
							ids.GenerateTestID(), // invalid initialValidationID
							nonce,
							weight,
						)).Bytes(),
					)).Bytes(),
				)),
				warpSignature,
			)).Bytes(),
			expectedErr: errCouldNotLoadL1Validator,
		},
		{
			name: "nonce too low",
			updateExecutor: func(e *standardTxExecutor) error {
				l1Validator := initialL1Validator
				l1Validator.MinNonce = nonce + 1
				return e.state.PutL1Validator(l1Validator)
			},
			expectedErr: errWarpMessageContainsStaleNonce,
		},
		{
			name: "invalid source chain",
			updateExecutor: func(e *standardTxExecutor) error {
				e.state.SetSubnetToL1Conversion(subnetID, state.SubnetToL1Conversion{})
				return nil
			},
			expectedErr: errWrongWarpMessageSourceChainID,
		},
		{
			name: "invalid source address",
			updateExecutor: func(e *standardTxExecutor) error {
				e.state.SetSubnetToL1Conversion(subnetID, state.SubnetToL1Conversion{
					ChainID: chainID,
				})
				return nil
			},
			expectedErr: errWrongWarpMessageSourceAddress,
		},
		{
			name:        "remove last validator",
			message:     removeValidatorWarpMessage.Bytes(),
			expectedErr: errRemovingLastValidator,
		},
		{
			name:    "remove deactivated validator",
			message: removeValidatorWarpMessage.Bytes(),
			updateExecutor: func(e *standardTxExecutor) error {
				// Add another validator to allow removal
				if err := increaseL1Weight(1)(e); err != nil {
					return err
				}

				l1Validator := initialL1Validator
				l1Validator.EndAccumulatedFee = 0 // Deactivate the validator
				return e.state.PutL1Validator(l1Validator)
			},
		},
		{
			name: "remove deactivated validator with nonce overflow",
			message: must[*warp.Message](t)(warp.NewMessage(
				must[*warp.UnsignedMessage](t)(warp.NewUnsignedMessage(
					ctx.NetworkID,
					chainID,
					must[*payload.AddressedCall](t)(payload.NewAddressedCall(
						address,
						must[*message.L1ValidatorWeight](t)(message.NewL1ValidatorWeight(
							validationID,
							math.MaxUint64,
							0,
						)).Bytes(),
					)).Bytes(),
				)),
				warpSignature,
			)).Bytes(),
			updateExecutor: func(e *standardTxExecutor) error {
				// Add another validator to allow removal
				if err := increaseL1Weight(1)(e); err != nil {
					return err
				}

				l1Validator := initialL1Validator
				l1Validator.EndAccumulatedFee = 0 // Deactivate the validator
				return e.state.PutL1Validator(l1Validator)
			},
		},
		{
			name:    "should have been previously deactivated",
			message: removeValidatorWarpMessage.Bytes(),
			updateExecutor: func(e *standardTxExecutor) error {
				e.state.SetAccruedFees(initialL1Validator.EndAccumulatedFee)
				return increaseL1Weight(1)(e)
			},
			expectedErr: errStateCorruption,
		},
		{
			name:           "remove active validator",
			message:        removeValidatorWarpMessage.Bytes(),
			updateExecutor: increaseL1Weight(1),
			expectedRemainingFundsUTXO: &avax.UTXO{
				Asset: avax.Asset{
					ID: ctx.AVAXAssetID,
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
			name:           "L1 weight overflow",
			updateExecutor: increaseL1Weight(math.MaxUint64 - initialWeight),
			expectedErr:    safemath.ErrOverflow,
		},
		{
			name:           "update validator",
			expectedNonce:  nonce + 1,
			expectedWeight: weight,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			// Create the SetL1ValidatorWeightTx
			wallet := txstest.NewWallet(
				t,
				ctx,
				defaultConfig,
				baseState,
				secp256k1fx.NewKeychain(genesistest.DefaultFundedKeys...),
				nil, // subnetIDs
				nil, // validationIDs
				nil, // chainIDs
			)

			setL1ValidatorWeightTx, err := wallet.IssueSetL1ValidatorWeightTx(
				increaseWeightWarpMessage.Bytes(),
				test.builderOptions...,
			)
			require.NoError(err)

			if test.message != nil {
				unsignedTx := setL1ValidatorWeightTx.Unsigned.(*platform.SetL1ValidatorWeightTx)
				unsignedTx.Message = test.message
			}

			diff, err := state.NewDiffOn(baseState, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(err)

			executor := &standardTxExecutor{
				backend: &Backend{
					Config:       defaultConfig,
					Bootstrapped: utils.NewAtomic(true),
					Fx:           fx,
					FlowChecker:  flowChecker,
					Ctx:          ctx,
				},
				feeCalculator: state.PickFeeCalculator(defaultConfig, baseState),
				tx:            setL1ValidatorWeightTx,
				state:         diff,
			}
			if test.updateExecutor != nil {
				require.NoError(test.updateExecutor(executor))
			}

			err = setL1ValidatorWeightTx.Unsigned.Visit(executor)
			require.ErrorIs(err, test.expectedErr)
			if err != nil {
				return
			}

			for utxoID := range setL1ValidatorWeightTx.InputIDs() {
				_, err := diff.GetUTXO(utxoID)
				require.ErrorIs(err, database.ErrNotFound)
			}

			baseTxOutputUTXOs := setL1ValidatorWeightTx.UTXOs()
			for _, expectedUTXO := range baseTxOutputUTXOs {
				utxoID := expectedUTXO.InputID()
				utxo, err := diff.GetUTXO(utxoID)
				require.NoError(err)
				require.Equal(expectedUTXO, utxo)
			}

			l1Validator, err := diff.GetL1Validator(validationID)
			if test.expectedWeight != 0 {
				require.NoError(err)

				expectedL1Validator := initialL1Validator
				expectedL1Validator.MinNonce = test.expectedNonce
				expectedL1Validator.Weight = test.expectedWeight
				require.Equal(expectedL1Validator, l1Validator)
				return
			}
			require.ErrorIs(err, database.ErrNotFound)

			utxoID := avax.UTXOID{
				TxID:        setL1ValidatorWeightTx.ID(),
				OutputIndex: uint32(len(baseTxOutputUTXOs)),
			}
			inputID := utxoID.InputID()
			utxo, err := diff.GetUTXO(inputID)
			if test.expectedRemainingFundsUTXO == nil {
				require.ErrorIs(err, database.ErrNotFound)
				return
			}
			require.NoError(err)

			test.expectedRemainingFundsUTXO.UTXOID = utxoID
			require.Equal(test.expectedRemainingFundsUTXO, utxo)
		})
	}
}

func TestStandardExecutorIncreaseL1ValidatorBalanceTx(t *testing.T) {
	var (
		fx = &secp256k1fx.Fx{}
		vm = &secp256k1fx.TestVM{
			Log: logging.NoLog{},
		}
	)
	require.NoError(t, fx.InitializeVM(vm))

	var (
		ctx           = snowtest.Context(t, constants.PlatformChainID)
		defaultConfig = &config.Internal{
			DynamicFeeConfig:   genesis.LocalParams.DynamicFeeConfig,
			ValidatorFeeConfig: genesis.LocalParams.ValidatorFeeConfig,
			UpgradeConfig:      upgradetest.GetConfig(upgradetest.Latest),
		}
		baseState = statetest.New(t, statetest.Config{
			Upgrades: defaultConfig.UpgradeConfig,
			Context:  ctx,
		})
		wallet = txstest.NewWallet(
			t,
			ctx,
			defaultConfig,
			baseState,
			secp256k1fx.NewKeychain(genesistest.DefaultFundedKeys...),
			nil, // subnetIDs
			nil, // validationIDs
			nil, // chainIDs
		)
		flowChecker = utxo.NewVerifier(
			ctx,
			&vm.Clk,
			fx,
		)

		backend = &Backend{
			Config:       defaultConfig,
			Bootstrapped: utils.NewAtomic(true),
			Fx:           fx,
			FlowChecker:  flowChecker,
			Ctx:          ctx,
		}
		feeCalculator = state.PickFeeCalculator(defaultConfig, baseState)
	)

	// Create the initial state
	diff, err := state.NewDiffOn(baseState, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(t, err)

	// Create the subnet
	createSubnetTx, err := wallet.IssueCreateSubnetTx(
		&secp256k1fx.OutputOwners{},
	)
	require.NoError(t, err)

	// Execute the subnet creation
	_, _, _, err = StandardTx(
		backend,
		feeCalculator,
		createSubnetTx,
		diff,
	)
	require.NoError(t, err)

	// Create the subnet conversion
	sk, err := localsigner.New()
	require.NoError(t, err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(t, err)

	const (
		weight                = 1
		initialBalance uint64 = 0
	)
	var (
		subnetID  = createSubnetTx.ID()
		chainID   = ids.GenerateTestID()
		address   = utils.RandomBytes(32)
		validator = &platform.ConvertSubnetToL1Validator{
			NodeID:  ids.GenerateTestNodeID().Bytes(),
			Weight:  weight,
			Balance: initialBalance,
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

	convertSubnetToL1Tx, err := wallet.IssueConvertSubnetToL1Tx(
		subnetID,
		chainID,
		address,
		[]*platform.ConvertSubnetToL1Validator{
			validator,
		},
	)
	require.NoError(t, err)

	// Execute the subnet conversion
	_, _, _, err = StandardTx(
		backend,
		feeCalculator,
		convertSubnetToL1Tx,
		diff,
	)
	require.NoError(t, err)
	require.NoError(t, diff.Apply(baseState))
	require.NoError(t, baseState.Commit())

	initialL1Validator, err := baseState.GetL1Validator(validationID)
	require.NoError(t, err)

	const balanceIncrease = units.NanoAvax
	tests := []struct {
		name            string
		validationID    ids.ID
		builderOptions  []common.Option
		updateTx        func(*platform.IncreaseL1ValidatorBalanceTx)
		updateExecutor  func(*standardTxExecutor) error
		expectedBalance uint64
		expectedErr     error
	}{
		{
			name: "invalid prior to E-Upgrade",
			updateExecutor: func(e *standardTxExecutor) error {
				e.backend.Config = &config.Internal{
					UpgradeConfig: upgradetest.GetConfig(upgradetest.Durango),
				}
				return nil
			},
			expectedErr: errEtnaUpgradeNotActive,
		},
		{
			name: "tx fails syntactic verification",
			updateExecutor: func(e *standardTxExecutor) error {
				e.backend.Ctx = snowtest.Context(t, ids.GenerateTestID())
				return nil
			},
			expectedErr: avax.ErrWrongChainID,
		},
		{
			name: "invalid memo length",
			builderOptions: []common.Option{
				common.WithMemo([]byte("memo!")),
			},
			expectedErr: avax.ErrMemoTooLarge,
		},
		{
			name: "fee overflow",
			updateTx: func(tx *platform.IncreaseL1ValidatorBalanceTx) {
				tx.Balance = math.MaxUint64
			},
			expectedErr: safemath.ErrOverflow,
		},
		{
			name: "insufficient fee",
			updateExecutor: func(e *standardTxExecutor) error {
				e.feeCalculator = txfee.NewDynamicCalculator(
					e.backend.Config.DynamicFeeConfig.Weights,
					100*genesis.LocalParams.DynamicFeeConfig.MinPrice,
				)
				return nil
			},
			expectedErr: utxo.ErrInsufficientUnlockedFunds,
		},
		{
			name:         "unknown validationID",
			validationID: ids.GenerateTestID(),
			expectedErr:  database.ErrNotFound,
		},
		{
			name:         "too many active validators",
			validationID: validationID,
			updateExecutor: func(e *standardTxExecutor) error {
				e.backend.Config = &config.Internal{
					DynamicFeeConfig: genesis.LocalParams.DynamicFeeConfig,
					ValidatorFeeConfig: validatorfee.Config{
						Capacity:                 0,
						Target:                   genesis.LocalParams.ValidatorFeeConfig.Target,
						MinPrice:                 genesis.LocalParams.ValidatorFeeConfig.MinPrice,
						ExcessConversionConstant: genesis.LocalParams.ValidatorFeeConfig.ExcessConversionConstant,
					},
					UpgradeConfig: upgradetest.GetConfig(upgradetest.Latest),
				}
				return nil
			},
			expectedErr: errMaxNumActiveValidators,
		},
		{
			name:         "accumulated fees overflow",
			validationID: validationID,
			updateExecutor: func(e *standardTxExecutor) error {
				e.state.SetAccruedFees(math.MaxUint64)
				return nil
			},
			expectedErr: safemath.ErrOverflow,
		},
		{
			name:            "increase balance",
			validationID:    validationID,
			expectedBalance: baseState.GetAccruedFees() + balanceIncrease,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			// Create the IncreaseL1ValidatorBalanceTx
			wallet := txstest.NewWallet(
				t,
				ctx,
				defaultConfig,
				baseState,
				secp256k1fx.NewKeychain(genesistest.DefaultFundedKeys...),
				nil, // subnetIDs
				nil, // validationIDs
				nil, // chainIDs
			)

			increaseL1ValidatorBalanceTx, err := wallet.IssueIncreaseL1ValidatorBalanceTx(
				test.validationID,
				balanceIncrease,
				test.builderOptions...,
			)
			require.NoError(err)

			unsignedTx := increaseL1ValidatorBalanceTx.Unsigned.(*platform.IncreaseL1ValidatorBalanceTx)
			if test.updateTx != nil {
				test.updateTx(unsignedTx)
			}

			diff, err := state.NewDiffOn(baseState, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(err)

			executor := &standardTxExecutor{
				backend: &Backend{
					Config:       defaultConfig,
					Bootstrapped: utils.NewAtomic(true),
					Fx:           fx,
					FlowChecker:  flowChecker,
					Ctx:          ctx,
				},
				feeCalculator: state.PickFeeCalculator(defaultConfig, baseState),
				tx:            increaseL1ValidatorBalanceTx,
				state:         diff,
			}
			if test.updateExecutor != nil {
				require.NoError(test.updateExecutor(executor))
			}

			err = unsignedTx.Visit(executor)
			require.ErrorIs(err, test.expectedErr)
			if err != nil {
				return
			}

			for utxoID := range increaseL1ValidatorBalanceTx.InputIDs() {
				_, err := diff.GetUTXO(utxoID)
				require.ErrorIs(err, database.ErrNotFound)
			}

			baseTxOutputUTXOs := increaseL1ValidatorBalanceTx.UTXOs()
			for _, expectedUTXO := range baseTxOutputUTXOs {
				utxoID := expectedUTXO.InputID()
				utxo, err := diff.GetUTXO(utxoID)
				require.NoError(err)
				require.Equal(expectedUTXO, utxo)
			}

			l1Validator, err := diff.GetL1Validator(validationID)
			require.NoError(err)

			expectedL1Validator := initialL1Validator
			expectedL1Validator.EndAccumulatedFee = test.expectedBalance
			require.Equal(expectedL1Validator, l1Validator)
		})
	}
}

func TestStandardExecutorDisableL1ValidatorTx(t *testing.T) {
	var (
		fx = &secp256k1fx.Fx{}
		vm = &secp256k1fx.TestVM{
			Log: logging.NoLog{},
		}
	)
	require.NoError(t, fx.InitializeVM(vm))
	require.NoError(t, fx.Bootstrapped())

	var (
		ctx           = snowtest.Context(t, constants.PlatformChainID)
		defaultConfig = &config.Internal{
			DynamicFeeConfig:   genesis.LocalParams.DynamicFeeConfig,
			ValidatorFeeConfig: genesis.LocalParams.ValidatorFeeConfig,
			UpgradeConfig:      upgradetest.GetConfig(upgradetest.Latest),
		}
		baseState = statetest.New(t, statetest.Config{
			Upgrades: defaultConfig.UpgradeConfig,
			Context:  ctx,
		})
		wallet = txstest.NewWallet(
			t,
			ctx,
			defaultConfig,
			baseState,
			secp256k1fx.NewKeychain(genesistest.DefaultFundedKeys...),
			nil, // subnetIDs
			nil, // validationIDs
			nil, // chainIDs
		)
		flowChecker = utxo.NewVerifier(
			ctx,
			&vm.Clk,
			fx,
		)

		backend = &Backend{
			Config:       defaultConfig,
			Bootstrapped: utils.NewAtomic(true),
			Fx:           fx,
			FlowChecker:  flowChecker,
			Ctx:          ctx,
		}
		feeCalculator = state.PickFeeCalculator(defaultConfig, baseState)
	)

	// Create the initial state
	diff, err := state.NewDiffOn(baseState, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(t, err)

	// Create the subnet
	createSubnetTx, err := wallet.IssueCreateSubnetTx(
		&secp256k1fx.OutputOwners{},
	)
	require.NoError(t, err)

	// Execute the subnet creation
	_, _, _, err = StandardTx(
		backend,
		feeCalculator,
		createSubnetTx,
		diff,
	)
	require.NoError(t, err)

	// Create the subnet conversion
	sk, err := localsigner.New()
	require.NoError(t, err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(t, err)

	const (
		weight         = 1
		initialBalance = units.Avax
	)
	var (
		subnetID  = createSubnetTx.ID()
		chainID   = ids.GenerateTestID()
		address   = utils.RandomBytes(32)
		validator = &platform.ConvertSubnetToL1Validator{
			NodeID:  ids.GenerateTestNodeID().Bytes(),
			Weight:  weight,
			Balance: initialBalance,
			Signer:  *pop,
			// RemainingBalanceOwner and DeactivationOwner are initialized so
			// that later reflect based equality checks pass.
			RemainingBalanceOwner: message.PChainOwner{
				Threshold: 1,
				Addresses: []ids.ShortID{
					ids.GenerateTestShortID(),
				},
			},
			DeactivationOwner: message.PChainOwner{
				Threshold: 1,
				Addresses: []ids.ShortID{
					genesistest.DefaultFundedKeys[0].Address(),
				},
			},
		}
		validationID = subnetID.Append(0)
	)

	convertSubnetToL1Tx, err := wallet.IssueConvertSubnetToL1Tx(
		subnetID,
		chainID,
		address,
		[]*platform.ConvertSubnetToL1Validator{
			validator,
		},
	)
	require.NoError(t, err)

	// Execute the subnet conversion
	_, _, _, err = StandardTx(
		backend,
		feeCalculator,
		convertSubnetToL1Tx,
		diff,
	)
	require.NoError(t, err)
	require.NoError(t, diff.Apply(baseState))
	require.NoError(t, baseState.Commit())

	initialL1Validator, err := baseState.GetL1Validator(validationID)
	require.NoError(t, err)

	tests := []struct {
		name            string
		validationID    ids.ID
		builderOptions  []common.Option
		updateTx        func(*platform.DisableL1ValidatorTx)
		updateExecutor  func(*standardTxExecutor) error
		expectedBalance uint64
		expectedErr     error
	}{
		{
			name: "invalid prior to E-Upgrade",
			updateExecutor: func(e *standardTxExecutor) error {
				e.backend.Config = &config.Internal{
					UpgradeConfig: upgradetest.GetConfig(upgradetest.Durango),
				}
				return nil
			},
			expectedErr: errEtnaUpgradeNotActive,
		},
		{
			name: "tx fails syntactic verification",
			updateExecutor: func(e *standardTxExecutor) error {
				e.backend.Ctx = snowtest.Context(t, ids.GenerateTestID())
				return nil
			},
			expectedErr: avax.ErrWrongChainID,
		},
		{
			name: "invalid memo length",
			builderOptions: []common.Option{
				common.WithMemo([]byte("memo!")),
			},
			expectedErr: avax.ErrMemoTooLarge,
		},
		{
			name:         "l1Validator not found",
			validationID: ids.GenerateTestID(),
			expectedErr:  errCouldNotLoadL1Validator,
		},
		{
			name:         "not authorized",
			validationID: validationID,
			updateTx: func(tx *platform.DisableL1ValidatorTx) {
				tx.DisableAuth.(*secp256k1fx.Input).SigIndices[0] = 123456789
			},
			expectedErr: errUnauthorizedModification,
		},
		{
			name:         "already deactivated",
			validationID: validationID,
			updateExecutor: func(e *standardTxExecutor) error {
				l1Validator := initialL1Validator
				l1Validator.EndAccumulatedFee = 0
				return e.state.PutL1Validator(l1Validator)
			},
			expectedBalance: 0,
		},
		{
			name:         "state corruption",
			validationID: validationID,
			updateExecutor: func(e *standardTxExecutor) error {
				e.state.SetAccruedFees(math.MaxUint64)
				return nil
			},
			expectedErr: errStateCorruption,
		},
		{
			name:            "deactivate validator",
			validationID:    validationID,
			expectedBalance: initialBalance,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			// Create the DisableL1ValidatorTx
			wallet := txstest.NewWallet(
				t,
				ctx,
				defaultConfig,
				baseState,
				secp256k1fx.NewKeychain(genesistest.DefaultFundedKeys...),
				nil, // subnetIDs
				[]ids.ID{validationID},
				nil, // chainIDs
			)

			disableL1ValidatorTx, err := wallet.IssueDisableL1ValidatorTx(
				validationID,
				test.builderOptions...,
			)
			require.NoError(err)

			unsignedTx := disableL1ValidatorTx.Unsigned.(*platform.DisableL1ValidatorTx)
			unsignedTx.ValidationID = test.validationID
			if test.updateTx != nil {
				test.updateTx(unsignedTx)
			}

			diff, err := state.NewDiffOn(baseState, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(err)

			executor := &standardTxExecutor{
				backend: &Backend{
					Config:       defaultConfig,
					Bootstrapped: utils.NewAtomic(true),
					Fx:           fx,
					FlowChecker:  flowChecker,
					Ctx:          ctx,
				},
				feeCalculator: state.PickFeeCalculator(defaultConfig, baseState),
				tx:            disableL1ValidatorTx,
				state:         diff,
			}
			if test.updateExecutor != nil {
				require.NoError(test.updateExecutor(executor))
			}

			err = unsignedTx.Visit(executor)
			require.ErrorIs(err, test.expectedErr)
			if err != nil {
				return
			}

			for utxoID := range disableL1ValidatorTx.InputIDs() {
				_, err := diff.GetUTXO(utxoID)
				require.ErrorIs(err, database.ErrNotFound)
			}

			baseTxOutputUTXOs := disableL1ValidatorTx.UTXOs()
			for _, expectedUTXO := range baseTxOutputUTXOs {
				utxoID := expectedUTXO.InputID()
				utxo, err := diff.GetUTXO(utxoID)
				require.NoError(err)
				require.Equal(expectedUTXO, utxo)
			}

			l1Validator, err := diff.GetL1Validator(validationID)
			require.NoError(err)

			expectedL1Validator := initialL1Validator
			expectedL1Validator.EndAccumulatedFee = 0
			require.Equal(expectedL1Validator, l1Validator)

			utxoID := avax.UTXOID{
				TxID:        disableL1ValidatorTx.ID(),
				OutputIndex: uint32(len(baseTxOutputUTXOs)),
			}
			inputID := utxoID.InputID()
			utxo, err := diff.GetUTXO(inputID)
			if test.expectedBalance == 0 {
				require.ErrorIs(err, database.ErrNotFound)
				return
			}
			require.NoError(err)

			require.Equal(
				&avax.UTXO{
					UTXOID: utxoID,
					Asset: avax.Asset{
						ID: ctx.AVAXAssetID,
					},
					Out: &secp256k1fx.TransferOutput{
						Amt: test.expectedBalance,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: validator.RemainingBalanceOwner.Threshold,
							Addrs:     validator.RemainingBalanceOwner.Addresses,
						},
					},
				},
				utxo,
			)
		})
	}
}
