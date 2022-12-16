// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
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
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/txheap"
	"github.com/ava-labs/avalanchego/vms/platformvm/validator"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var errNonExistingOffer = errors.New("non existing deposit offer")

type Camino struct {
	VerifyNodeSignature        bool                    `json:"verifyNodeSignature"`
	LockModeBondDeposit        bool                    `json:"lockModeBondDeposit"`
	InitialAdmin               ids.ShortID             `json:"initialAdmin"`
	AddressStates              []genesis.AddressState  `json:"addressStates"`
	DepositOffers              []genesis.DepositOffer  `json:"depositOffers"`
	ValidatorDeposits          [][]ids.ID              `json:"validatorDeposits"`
	ValidatorConsortiumMembers []ids.ShortID           `json:"validatorConsortiumMembers"`
	UTXODeposits               []ids.ID                `json:"utxoDeposits"`
	InitialMultisigAddresses   []genesis.MultisigAlias `json:"initialMultisigAddresses"`
}

func (c Camino) ParseToGenesis() genesis.Camino {
	return genesis.Camino{
		VerifyNodeSignature:      c.VerifyNodeSignature,
		LockModeBondDeposit:      c.LockModeBondDeposit,
		InitialAdmin:             c.InitialAdmin,
		AddressStates:            c.AddressStates,
		DepositOffers:            c.DepositOffers,
		InitialMultisigAddresses: c.InitialMultisigAddresses,
	}
}

// BuildGenesis build the genesis state of the Platform Chain (and thereby the Avalanche network.)
func buildCaminoGenesis(args *BuildGenesisArgs, reply *BuildGenesisReply) error {
	if len(args.Camino.UTXODeposits) != len(args.UTXOs) {
		return errors.New("len(args.Camino.UTXODeposits) != len(args.UTXOs)")
	}
	if len(args.Camino.ValidatorDeposits) != len(args.Validators) {
		return errors.New("len(args.Camino.ValidatorDeposits) != len(args.Validators)")
	}
	for i := range args.Validators {
		if len(args.Camino.ValidatorDeposits[i]) != len(args.Validators[i].Staked) {
			return fmt.Errorf("len(args.Camino.ValidatorDeposits[%d]) != len(args.Validators[%d].Staked)", i, i)
		}
	}

	startTimestamp := uint64(args.Time)
	networkID := uint32(args.NetworkID)
	utxos := make([]*genesis.UTXO, 0, len(args.UTXOs))
	validators := txheap.NewByEndTime()
	deposits := txheap.NewByDuration()

	offers := make(map[ids.ID]genesis.DepositOffer, len(args.Camino.DepositOffers))
	for i := range args.Camino.DepositOffers {
		offer := args.Camino.DepositOffers[i]
		offerID, err := offer.ID()
		if err != nil {
			return err
		}
		offers[offerID] = offer
	}

	for validatorIndex, vdr := range args.Validators {
		vdr := vdr
		validatorTx, err := makeValidator(
			&vdr,
			args.Camino.ValidatorConsortiumMembers[validatorIndex],
			args.AvaxAssetID,
			startTimestamp,
			networkID,
		)
		if err != nil {
			return err
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
				offers,
				&vdr.Staked[i],
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
				deposits.Add(depositTx)
			}

			utxos = append(utxos, &genesis.UTXO{
				UTXO:    *utxo,
				Message: messageBytes,
			})
		}

		validators.Add(validatorTx)
	}

	for i, apiUTXO := range args.UTXOs {
		apiUTXO := apiUTXO
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

		utxo, depositTx, err := makeUTXOAndDeposit(
			offers,
			&apiUTXO,
			ids.Empty,
			args.Camino.UTXODeposits[i],
			uint32(i),
			addrID,
			args.AvaxAssetID,
			networkID,
		)
		if err != nil {
			return err
		}

		if depositTx != nil {
			deposits.Add(depositTx)
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
				NetworkID:    uint32(args.NetworkID),
				BlockchainID: ids.Empty,
			}},
			SubnetID:    chain.SubnetID,
			ChainName:   chain.Name,
			VMID:        chain.VMID,
			FxIDs:       chain.FxIDs,
			GenesisData: genesisBytes,
			SubnetAuth:  &secp256k1fx.Input{},
		}}
		if err := tx.Sign(txs.GenesisCodec, nil); err != nil {
			return err
		}

		chains = append(chains, tx)
	}

	validatorTxs := validators.List()

	camino := args.Camino.ParseToGenesis()
	if deposits != nil {
		camino.Deposits = deposits.List()
	}

	// genesis holds the genesis state
	g := genesis.Genesis{
		UTXOs:         utxos,
		Validators:    validatorTxs,
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
	consortiumMemberAddr ids.ShortID,
	avaxAssetID ids.ID,
	startTime uint64,
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
	if uint64(vdr.EndTime) <= startTime {
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
			Validator: validator.Validator{
				NodeID: vdr.NodeID,
				Start:  startTime,
				End:    uint64(vdr.EndTime),
				Wght:   weight,
			},
			RewardsOwner: rewardsOwner,
		},
		ConsortiumMemberAddress: consortiumMemberAddr,
	}}
	if err := tx.Sign(txs.GenesisCodec, nil); err != nil {
		return nil, err
	}

	return tx, nil
}

func makeUTXOAndDeposit(
	offers map[ids.ID]genesis.DepositOffer,
	apiUTXO *UTXO,
	bondTxID ids.ID,
	depositOfferID ids.ID,
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
		Amt:          uint64(apiUTXO.Amount),
		OutputOwners: owner,
	}

	var depositTx *txs.Tx
	txID := bondTxID
	depositTxID := ids.Empty
	if depositOfferID != ids.Empty {
		offer, ok := offers[depositOfferID]
		if !ok {
			return nil, nil, errNonExistingOffer
		}

		depositTx = &txs.Tx{Unsigned: &txs.DepositTx{
			BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    networkID,
				BlockchainID: ids.Empty,
				Ins:          []*avax.TransferableInput{},
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
			DepositOfferID:  depositOfferID,
			DepositDuration: offer.MinDuration,
			RewardsOwner:    &owner,
		}}
		if err := depositTx.Sign(txs.GenesisCodec, nil); err != nil {
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
