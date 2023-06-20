// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"
	"math"
	"reflect"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

type generatorPriorityType uint8

const (
	permissionlessValidator generatorPriorityType = iota
	permissionedValidator
	permissionlessDelegator
	permissionedDelegator
)

// TODO ABENEGIA: complete
// stakerTxGenerator helps creating random yet reproducible Staker objects,
// which can be used in our property tests. stakerTxGenerator takes care of
// enforcing some Staker invariants on each and every random sample.
// TestGeneratedStakersValidity documents and verifies the enforced invariants.
func stakerTxGenerator(
	ctx *snow.Context,
	priority generatorPriorityType,
	subnetID *ids.ID,
	nodeID *ids.NodeID,
	blsSigner signer.Signer,
	maxWeight uint64, // helps avoiding overflows in delegator tests
) gopter.Gen {
	switch priority {
	case permissionedValidator:
		return addValidatorTxGenerator(ctx, nodeID)
	case permissionedDelegator:
		return addDelegatorTxGenerator(ctx, nodeID, maxWeight)
	case permissionlessValidator:
		return addPermissionlessValidatorTxGenerator(ctx, subnetID, nodeID, blsSigner)
	case permissionlessDelegator:
		return addPermissionlessDelegatorTxGenerator(ctx, subnetID, nodeID, maxWeight)
	default:
		panic(fmt.Sprintf("unhandled tx priority %v", priority))
	}
}

func addPermissionlessValidatorTxGenerator(
	ctx *snow.Context,
	subnetID *ids.ID,
	nodeID *ids.NodeID,
	blsSigner signer.Signer,
) gopter.Gen {
	return validatorTxGenerator(nodeID, math.MaxUint64).FlatMap(
		func(v interface{}) gopter.Gen {
			genStakerSubnetID := genID
			if subnetID != nil {
				genStakerSubnetID = gen.Const(*subnetID)
			}
			validatorTx := v.(txs.Validator)

			specificGen := gen.StructPtr(reflect.TypeOf(&txs.AddPermissionlessValidatorTx{}), map[string]gopter.Gen{
				"BaseTx": gen.Const(txs.BaseTx{
					BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins:          []*avax.TransferableInput{},
						Outs:         []*avax.TransferableOutput{},
					},
				}),
				"Validator": gen.Const(validatorTx),
				"Subnet":    genStakerSubnetID,
				"Signer":    gen.Const(blsSigner),
				"StakeOuts": gen.Const([]*avax.TransferableOutput{
					{
						Asset: avax.Asset{
							ID: ctx.AVAXAssetID,
						},
						Out: &secp256k1fx.TransferOutput{
							Amt: validatorTx.Weight(),
						},
					},
				}),
				"ValidatorRewardsOwner": gen.Const(
					&secp256k1fx.OutputOwners{
						Addrs: []ids.ShortID{},
					},
				),
				"DelegatorRewardsOwner": gen.Const(
					&secp256k1fx.OutputOwners{
						Addrs: []ids.ShortID{},
					},
				),
				"DelegationShares": gen.UInt32Range(0, reward.PercentDenominator),
			})

			return specificGen.FlatMap(
				func(v interface{}) gopter.Gen {
					stakerTx := v.(*txs.AddPermissionlessValidatorTx)

					if err := stakerTx.SyntacticVerify(ctx); err != nil {
						panic(fmt.Errorf("failed syntax verification in tx generator, %w", err))
					}

					// Note: we don't sign the tx here, since we want the freedom to modify
					// the stakerTx just before testing while avoid having the wrong txID.
					// We use txs.Tx as a box to return a txs.StakerTx interface.
					sTx := &txs.Tx{Unsigned: stakerTx}

					return gen.Const(sTx)
				},
				reflect.TypeOf(&txs.AddPermissionlessValidatorTx{}),
			)
		},
		reflect.TypeOf(&txs.AddPermissionlessValidatorTx{}),
	)
}

func addValidatorTxGenerator(
	ctx *snow.Context,
	nodeID *ids.NodeID,
) gopter.Gen {
	return validatorTxGenerator(nodeID, math.MaxUint64).FlatMap(
		func(v interface{}) gopter.Gen {
			validatorTx := v.(txs.Validator)

			specificGen := gen.StructPtr(reflect.TypeOf(&txs.AddValidatorTx{}), map[string]gopter.Gen{
				"BaseTx": gen.Const(txs.BaseTx{
					BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins:          []*avax.TransferableInput{},
						Outs:         []*avax.TransferableOutput{},
					},
				}),
				"Validator": gen.Const(validatorTx),
				"StakeOuts": gen.Const([]*avax.TransferableOutput{
					{
						Asset: avax.Asset{
							ID: ctx.AVAXAssetID,
						},
						Out: &secp256k1fx.TransferOutput{
							Amt: validatorTx.Weight(),
						},
					},
				}),
				"RewardsOwner": gen.Const(
					&secp256k1fx.OutputOwners{
						Addrs: []ids.ShortID{},
					},
				),
				"DelegationShares": gen.UInt32Range(0, reward.PercentDenominator),
			})

			return specificGen.FlatMap(
				func(v interface{}) gopter.Gen {
					stakerTx := v.(*txs.AddValidatorTx)

					if err := stakerTx.SyntacticVerify(ctx); err != nil {
						panic(fmt.Errorf("failed syntax verification in tx generator, %w", err))
					}

					// Note: we don't sign the tx here, since we want the freedom to modify
					// the stakerTx just before testing while avoid having the wrong txID.
					// We use txs.Tx as a box to return a txs.StakerTx interface.
					sTx := &txs.Tx{Unsigned: stakerTx}

					return gen.Const(sTx)
				},
				reflect.TypeOf(&txs.AddValidatorTx{}),
			)
		},
		reflect.TypeOf(txs.Validator{}),
	)
}

func addPermissionlessDelegatorTxGenerator(
	ctx *snow.Context,
	subnetID *ids.ID,
	nodeID *ids.NodeID,
	maxWeight uint64, // helps avoiding overflows in delegator tests
) gopter.Gen {
	return validatorTxGenerator(nodeID, maxWeight).FlatMap(
		func(v interface{}) gopter.Gen {
			genStakerSubnetID := genID
			if subnetID != nil {
				genStakerSubnetID = gen.Const(*subnetID)
			}

			validatorTx := v.(txs.Validator)
			specificGen := gen.StructPtr(reflect.TypeOf(txs.AddPermissionlessDelegatorTx{}), map[string]gopter.Gen{
				"BaseTx": gen.Const(txs.BaseTx{
					BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins:          []*avax.TransferableInput{},
						Outs:         []*avax.TransferableOutput{},
					},
				}),
				"Validator": gen.Const(validatorTx),
				"Subnet":    genStakerSubnetID,
				"StakeOuts": gen.Const([]*avax.TransferableOutput{
					{
						Asset: avax.Asset{
							ID: ctx.AVAXAssetID,
						},
						Out: &secp256k1fx.TransferOutput{
							Amt: validatorTx.Weight(),
						},
					},
				}),
				"DelegationRewardsOwner": gen.Const(
					&secp256k1fx.OutputOwners{
						Addrs: []ids.ShortID{},
					},
				),
			})

			return specificGen.FlatMap(
				func(v interface{}) gopter.Gen {
					stakerTx := v.(*txs.AddPermissionlessDelegatorTx)

					if err := stakerTx.SyntacticVerify(ctx); err != nil {
						panic(fmt.Errorf("failed syntax verification in tx generator, %w", err))
					}

					// Note: we don't sign the tx here, since we want the freedom to modify
					// the stakerTx just before testing while avoid having the wrong txID.
					// We use txs.Tx as a box to return a txs.StakerTx interface.
					sTx := &txs.Tx{Unsigned: stakerTx}

					return gen.Const(sTx)
				},
				reflect.TypeOf(&txs.AddPermissionlessDelegatorTx{}),
			)
		},
		reflect.TypeOf(txs.Validator{}),
	)
}

func addDelegatorTxGenerator(
	ctx *snow.Context,
	nodeID *ids.NodeID,
	maxWeight uint64, // helps avoiding overflows in delegator tests
) gopter.Gen {
	return validatorTxGenerator(nodeID, maxWeight).FlatMap(
		func(v interface{}) gopter.Gen {
			validatorTx := v.(txs.Validator)
			specificGen := gen.StructPtr(reflect.TypeOf(txs.AddDelegatorTx{}), map[string]gopter.Gen{
				"BaseTx": gen.Const(txs.BaseTx{
					BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins:          []*avax.TransferableInput{},
						Outs:         []*avax.TransferableOutput{},
					},
				}),
				"Validator": gen.Const(validatorTx),
				"StakeOuts": gen.Const([]*avax.TransferableOutput{
					{
						Asset: avax.Asset{
							ID: ctx.AVAXAssetID,
						},
						Out: &secp256k1fx.TransferOutput{
							Amt: validatorTx.Weight(),
						},
					},
				}),
				"DelegationRewardsOwner": gen.Const(
					&secp256k1fx.OutputOwners{
						Addrs: []ids.ShortID{},
					},
				),
			})

			return specificGen.FlatMap(
				func(v interface{}) gopter.Gen {
					stakerTx := v.(*txs.AddDelegatorTx)

					if err := stakerTx.SyntacticVerify(ctx); err != nil {
						panic(fmt.Errorf("failed syntax verification in tx generator, %w", err))
					}

					// Note: we don't sign the tx here, since we want the freedom to modify
					// the stakerTx just before testing while avoid having the wrong txID.
					// We use txs.Tx as a box to return a txs.StakerTx interface.
					sTx := &txs.Tx{Unsigned: stakerTx}

					return gen.Const(sTx)
				},
				reflect.TypeOf(&txs.AddDelegatorTx{}),
			)
		},
		reflect.TypeOf(txs.Validator{}),
	)
}

func validatorTxGenerator(
	nodeID *ids.NodeID,
	maxWeight uint64, // helps avoiding overflows in delegator tests
) gopter.Gen {
	return genStakerMicroData().FlatMap(
		func(v interface{}) gopter.Gen {
			macro := v.(stakerMicroData)

			genStakerNodeID := genNodeID
			if nodeID != nil {
				genStakerNodeID = gen.Const(*nodeID)
			}

			return gen.Struct(reflect.TypeOf(txs.Validator{}), map[string]gopter.Gen{
				"NodeID": genStakerNodeID,
				"Start":  gen.Const(uint64(macro.StartTime.Unix())),
				"End":    gen.Const(uint64(macro.StartTime.Add(time.Duration(macro.Duration)).Unix())),
				"Wght":   gen.UInt64Range(1, maxWeight),
			})
		},
		reflect.TypeOf(stakerMicroData{}),
	)
}

// stakerMicroData holds seed attributes to generate stakerMacroData
type stakerMicroData struct {
	StartTime time.Time
	Duration  int64
}

// genStakerMicroData is the helper to generate stakerMicroData
func genStakerMicroData() gopter.Gen {
	return gen.Struct(reflect.TypeOf(&stakerMicroData{}), map[string]gopter.Gen{
		"StartTime": gen.Time(),
		"Duration":  gen.Int64Range(1, 365*24),
	})
}

const (
	lengthID     = 32
	lengthNodeID = 20
)

// genID is the helper generator for ids.ID objects
var genID = gen.SliceOfN(lengthID, gen.UInt8()).FlatMap(
	func(v interface{}) gopter.Gen {
		byteSlice := v.([]byte)
		var byteArray [lengthID]byte
		copy(byteArray[:], byteSlice)
		return gen.Const(ids.ID(byteArray))
	},
	reflect.TypeOf([]byte{}),
)

// genNodeID is the helper generator for ids.NodeID objects
var genNodeID = gen.SliceOfN(lengthNodeID, gen.UInt8()).FlatMap(
	func(v interface{}) gopter.Gen {
		byteSlice := v.([]byte)
		var byteArray [lengthNodeID]byte
		copy(byteArray[:], byteSlice)
		return gen.Const(ids.NodeID(byteArray))
	},
	reflect.TypeOf([]byte{}),
)
