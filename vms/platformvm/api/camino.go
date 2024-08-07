// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/vms/types"
)

var (
	errWrongUTXONumber              = errors.New("not matching amount of utxo deposits and utxos")
	errWrongValidatorNumber         = errors.New("not matching amount of validator deposits and validators")
	errWrongDepositsAndStakedNumber = errors.New("not matching amount of deposit definitions ad validator staked outs")
	errWrongLockMode                = errors.New("wrong lock mode")
)

type UTXODeposit struct {
	OfferID         ids.ID `json:"offerID"`
	Duration        uint64 `json:"duration"`
	TimestampOffset uint64 `json:"timestampOffset"`
	Memo            string `json:"memo"`
}

type Camino struct {
	VerifyNodeSignature        bool                   `json:"verifyNodeSignature"`
	LockModeBondDeposit        bool                   `json:"lockModeBondDeposit"`
	InitialAdmin               ids.ShortID            `json:"initialAdmin"`
	AddressStates              []genesis.AddressState `json:"addressStates"`
	DepositOffers              []*deposit.Offer       `json:"depositOffers"`
	ValidatorDeposits          [][]UTXODeposit        `json:"validatorDeposits"`
	ValidatorConsortiumMembers []ids.ShortID          `json:"validatorConsortiumMembers"`
	UTXODeposits               []UTXODeposit          `json:"utxoDeposits"`
	MultisigAliases            []*multisig.Alias      `json:"multisigAliases"`
}

func (c *Camino) ParseToGenesis() genesis.Camino {
	if c == nil {
		return genesis.Camino{}
	}
	return genesis.Camino{
		VerifyNodeSignature: c.VerifyNodeSignature,
		LockModeBondDeposit: c.LockModeBondDeposit,
		InitialAdmin:        c.InitialAdmin,
		AddressStates:       c.AddressStates,
		DepositOffers:       c.DepositOffers,
		MultisigAliases:     c.MultisigAliases,
	}
}

// BuildGenesis build the genesis state of the Platform Chain (and thereby the Avalanche network.)
func buildCaminoGenesis(args *BuildGenesisArgs, reply *BuildGenesisReply) error {
	if !args.Camino.LockModeBondDeposit {
		return errWrongLockMode
	}
	if len(args.Camino.UTXODeposits) != len(args.UTXOs) {
		return errWrongUTXONumber
	}
	if len(args.Camino.ValidatorDeposits) != len(args.Validators) {
		return errWrongValidatorNumber
	}
	for i := range args.Validators {
		if len(args.Camino.ValidatorDeposits[i]) != len(args.Validators[i].Staked) {
			return errWrongDepositsAndStakedNumber
		}
	}

	startTimestamp := uint64(args.Time)
	networkID := uint32(args.NetworkID)
	utxos := make([]*genesis.UTXO, 0, len(args.UTXOs))
	genesisBlocks := map[uint64]*genesis.Block{}
	consortiumMemberNodes := make([]genesis.ConsortiumMemberNodeID, len(args.Validators))

	for validatorIndex, vdr := range args.Validators {
		vdr := vdr
		validatorTx, err := makeValidator(
			&vdr,
			args.AvaxAssetID,
			networkID,
		)
		if err != nil {
			return err
		}

		validatorBlockTimestamp := uint64(vdr.StartTime)
		genesisBlock := getGenesisBlock(genesisBlocks, validatorBlockTimestamp)
		genesisBlock.Validators = append(genesisBlock.Validators, validatorTx)

		consortiumMemberNodes[validatorIndex] = genesis.ConsortiumMemberNodeID{
			ConsortiumMemberAddress: args.Camino.ValidatorConsortiumMembers[validatorIndex],
			NodeID:                  args.Validators[validatorIndex].NodeID,
		}

		bondTxID := validatorTx.ID()
		bond := validatorTx.Unsigned.Outputs()

		// creating deposits (if any) and utxos out of validator's bond
		for i, output := range bond {
			messageBytes, err := formatting.Decode(args.Encoding, vdr.Staked[i].Message)
			if err != nil {
				return fmt.Errorf("problem decoding UTXO message bytes: %w", err)
			}

			innerOut := output.Out.(*locked.Out).TransferableOut.(*secp256k1fx.TransferOutput)

			utxo, depositTx, err := makeUTXOAndDeposit(
				uint64(vdr.Staked[i].Amount),
				bondTxID,
				args.Camino.ValidatorDeposits[validatorIndex][i],
				uint32(i),
				innerOut.Addrs[0],
				args.AvaxAssetID,
				networkID,
			)
			if err != nil {
				return err
			}

			if depositTx != nil {
				blockTimestamp := startTimestamp + args.Camino.ValidatorDeposits[validatorIndex][i].TimestampOffset
				if blockTimestamp < validatorBlockTimestamp {
					return errors.New("validator timestamp is after validator's bond deposit timestamp")
				}

				genesisBlock := getGenesisBlock(genesisBlocks, blockTimestamp)
				genesisBlock.Deposits = append(genesisBlock.Deposits, depositTx)
			}

			utxos = append(utxos, &genesis.UTXO{
				UTXO:    *utxo,
				Message: messageBytes,
			})
		}
	}

	unlockedUTXOIndices := map[uint64][]int{}

	for i, apiUTXO := range args.UTXOs {
		if apiUTXO.Amount == 0 {
			return errUTXOHasNoValue
		}

		addrID, err := bech32ToID(apiUTXO.Address)
		if err != nil {
			return err
		}

		messageBytes, err := formatting.Decode(args.Encoding, apiUTXO.Message)
		if err != nil {
			return fmt.Errorf("problem decoding UTXO message bytes: %w", err)
		}

		blockTimestamp := startTimestamp + args.Camino.UTXODeposits[i].TimestampOffset
		genesisBlock := getGenesisBlock(genesisBlocks, blockTimestamp)
		outputIndex := uint32(0)
		if len(genesisBlock.UnlockedUTXOsTxs) != 0 {
			outputIndex = uint32(len(genesisBlock.UnlockedUTXOsTxs[0].Unsigned.Outputs()))
		}

		utxo, depositTx, err := makeUTXOAndDeposit(
			uint64(apiUTXO.Amount),
			ids.Empty,
			args.Camino.UTXODeposits[i],
			outputIndex,
			addrID,
			args.AvaxAssetID,
			networkID,
		)
		if err != nil {
			return err
		}

		if depositTx != nil {
			genesisBlock.Deposits = append(genesisBlock.Deposits, depositTx)
		} else {
			if len(genesisBlock.UnlockedUTXOsTxs) == 0 {
				genesisBlock.UnlockedUTXOsTxs = []*txs.Tx{{Unsigned: &txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    networkID,
					BlockchainID: ids.Empty,
				}}}}
			}
			tx := genesisBlock.UnlockedUTXOsTxs[0].Unsigned.(*txs.BaseTx)
			tx.Outs = append(tx.Outs, &avax.TransferableOutput{
				Asset: avax.Asset{ID: args.AvaxAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: uint64(apiUTXO.Amount),
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{addrID},
					},
				},
			})
			unlockedUTXOIndices[blockTimestamp] = append(unlockedUTXOIndices[blockTimestamp], len(utxos))
		}

		utxos = append(utxos, &genesis.UTXO{
			UTXO:    *utxo,
			Message: messageBytes,
		})
	}

	// Specify the chains that exist at genesis.
	chains := []*txs.Tx{}
	for _, chain := range args.Chains {
		genesisBytes, err := formatting.Decode(args.Encoding, chain.GenesisData)
		if err != nil {
			return fmt.Errorf("problem decoding chain genesis data: %w", err)
		}
		tx := &txs.Tx{Unsigned: &txs.CreateChainTx{
			BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    networkID,
				BlockchainID: ids.Empty,
			}},
			SubnetID:    chain.SubnetID,
			ChainName:   chain.Name,
			VMID:        chain.VMID,
			FxIDs:       chain.FxIDs,
			GenesisData: genesisBytes,
			SubnetAuth:  &secp256k1fx.Input{},
		}}
		if err := tx.Initialize(txs.GenesisCodec); err != nil {
			return err
		}

		chains = append(chains, tx)
	}

	camino := args.Camino.ParseToGenesis()

	camino.Blocks = make([]*genesis.Block, 0, len(genesisBlocks))
	for _, genesisBlock := range genesisBlocks {
		if len(genesisBlock.UnlockedUTXOsTxs) != 0 {
			tx := genesisBlock.UnlockedUTXOsTxs[0]
			if err := tx.Initialize(txs.GenesisCodec); err != nil {
				return err
			}

			if utxoIndices, ok := unlockedUTXOIndices[genesisBlock.Timestamp]; ok {
				for _, i := range utxoIndices {
					utxos[i].TxID = tx.ID()
				}
			}
		}

		camino.Blocks = append(camino.Blocks, genesisBlock)
	}
	utils.Sort(camino.Blocks)

	camino.ConsortiumMembersNodeIDs = consortiumMemberNodes

	// genesis holds the genesis state
	g := genesis.Genesis{
		UTXOs:         utxos,
		Chains:        chains,
		Camino:        camino,
		Timestamp:     uint64(args.Time),
		InitialSupply: uint64(args.InitialSupply),
		Message:       args.Message,
	}

	// Marshal genesis to bytes
	bytes, err := genesis.Codec.Marshal(genesis.Version, g)
	if err != nil {
		return fmt.Errorf("couldn't marshal genesis: %w", err)
	}
	reply.Bytes, err = formatting.Encode(args.Encoding, bytes)
	if err != nil {
		return fmt.Errorf("couldn't encode genesis as string: %w", err)
	}
	reply.Encoding = args.Encoding
	return nil
}

func makeValidator(
	vdr *PermissionlessValidator,
	avaxAssetID ids.ID,
	networkID uint32,
) (*txs.Tx, error) {
	weight := uint64(0)
	bondLockIDs := locked.IDsEmpty.Lock(locked.StateBonded)
	bond := make([]*avax.TransferableOutput, len(vdr.Staked))
	for i, apiUTXO := range vdr.Staked {
		addrID, err := bech32ToID(apiUTXO.Address)
		if err != nil {
			return nil, err
		}

		bond[i] = &avax.TransferableOutput{
			Asset: avax.Asset{ID: avaxAssetID},
			Out: &locked.Out{
				IDs: bondLockIDs,
				TransferableOut: &secp256k1fx.TransferOutput{
					Amt: uint64(apiUTXO.Amount),
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{addrID},
					},
				},
			},
		}

		newWeight, err := math.Add64(weight, uint64(apiUTXO.Amount))
		if err != nil {
			return nil, errStakeOverflow
		}
		weight = newWeight
	}

	if weight == 0 {
		return nil, errValidatorAddsNoValue
	}
	if vdr.EndTime <= vdr.StartTime {
		return nil, errValidatorAddsNoValue
	}

	rewardsOwner, err := getSecpOwner(vdr.RewardOwner)
	if err != nil {
		return nil, err
	}

	tx := &txs.Tx{Unsigned: &txs.CaminoAddValidatorTx{
		AddValidatorTx: txs.AddValidatorTx{
			BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    networkID,
				BlockchainID: ids.Empty,
				Outs:         bond,
			}},
			Validator: txs.Validator{
				NodeID: vdr.NodeID,
				Start:  uint64(vdr.StartTime),
				End:    uint64(vdr.EndTime),
				Wght:   weight,
			},
			RewardsOwner: rewardsOwner,
		},
		NodeOwnerAuth: &secp256k1fx.Input{},
	}}
	if err := tx.Initialize(txs.GenesisCodec); err != nil {
		return nil, err
	}

	return tx, nil
}

func makeUTXOAndDeposit(
	amount uint64,
	bondTxID ids.ID,
	deposit UTXODeposit,
	outputIndex uint32,
	ownerAddr ids.ShortID,
	avaxAssetID ids.ID,
	networkID uint32,
) (
	*avax.UTXO, // utxo
	*txs.Tx, // deposit tx
	error,
) {
	owner := secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{ownerAddr},
	}

	innerOut := &secp256k1fx.TransferOutput{
		Amt:          amount,
		OutputOwners: owner,
	}

	var depositTx *txs.Tx
	txID := bondTxID
	depositTxID := ids.Empty
	if deposit.OfferID != ids.Empty {
		ins := []*avax.TransferableInput{}
		if bondTxID != ids.Empty {
			ins = append(ins, &avax.TransferableInput{
				UTXOID: avax.UTXOID{
					TxID:        txID,
					OutputIndex: outputIndex,
				},
				Asset: avax.Asset{ID: avaxAssetID},
				In: &locked.In{
					IDs: locked.IDs{
						BondTxID:    bondTxID,
						DepositTxID: ids.Empty,
					},
					TransferableIn: &secp256k1fx.TransferInput{
						Amt:   amount,
						Input: secp256k1fx.Input{},
					},
				},
			})
		}

		depositTx = &txs.Tx{Unsigned: &txs.DepositTx{
			BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    networkID,
				BlockchainID: ids.Empty,
				Memo:         types.JSONByteSlice(deposit.Memo),
				Ins:          ins,
				Outs: []*avax.TransferableOutput{{
					Asset: avax.Asset{ID: avaxAssetID},
					Out: &locked.Out{
						IDs: locked.IDs{
							BondTxID:    bondTxID,
							DepositTxID: locked.ThisTxID,
						},
						TransferableOut: innerOut,
					},
				}},
			}},
			DepositOfferID:  deposit.OfferID,
			DepositDuration: uint32(deposit.Duration),
			RewardsOwner:    &owner,
		}}
		if err := depositTx.Initialize(txs.GenesisCodec); err != nil {
			return nil, nil, err
		}

		txID = depositTx.ID()
		outputIndex = 0
		depositTxID = txID
	}

	var utxoOut verify.State = innerOut
	if bondTxID != ids.Empty || depositTx != nil {
		utxoOut = &locked.Out{
			IDs: locked.IDs{
				BondTxID:    bondTxID,
				DepositTxID: depositTxID,
			},
			TransferableOut: innerOut,
		}
	}

	return &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID:        txID,
			OutputIndex: outputIndex,
		},
		Asset: avax.Asset{ID: avaxAssetID},
		Out:   utxoOut,
	}, depositTx, nil
}

func getSecpOwner(owner *Owner) (*secp256k1fx.OutputOwners, error) {
	outputOwners := &secp256k1fx.OutputOwners{
		Locktime:  uint64(owner.Locktime),
		Threshold: uint32(owner.Threshold),
	}
	for _, addrStr := range owner.Addresses {
		addrID, err := bech32ToID(addrStr)
		if err != nil {
			return nil, err
		}
		outputOwners.Addrs = append(outputOwners.Addrs, addrID)
	}
	utils.Sort(outputOwners.Addrs)
	return outputOwners, nil
}

func getGenesisBlock(blocks map[uint64]*genesis.Block, timestamp uint64) *genesis.Block {
	block, ok := blocks[timestamp]
	if !ok {
		block = &genesis.Block{
			Timestamp: timestamp,
		}
		blocks[timestamp] = block
	}
	return block
}
