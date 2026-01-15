// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"cmp"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/txheap"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// Note that since an Avalanche network has exactly one Platform Chain,
// and the Platform Chain defines the genesis state of the network
// (who is staking, which chains exist, etc.), defining the genesis
// state of the Platform Chain is the same as defining the genesis
// state of the network.

var (
	errUTXOHasNoValue         = errors.New("genesis UTXO has no value")
	errValidatorHasNoWeight   = errors.New("validator has not weight")
	errValidatorAlreadyExited = errors.New("validator would have already unstaked")
	errStakeOverflow          = errors.New("validator stake exceeds limit")

	_ utils.Sortable[Allocation] = Allocation{}
)

// UTXO adds messages to UTXOs
type UTXO struct {
	avax.UTXO `serialize:"true"`
	Message   []byte `serialize:"true" json:"message"`
}

// Genesis represents a genesis state of the platform chain
type Genesis struct {
	UTXOs         []*UTXO   `serialize:"true"`
	Validators    []*txs.Tx `serialize:"true"`
	Chains        []*txs.Tx `serialize:"true"`
	Timestamp     uint64    `serialize:"true"`
	InitialSupply uint64    `serialize:"true"`
	Message       string    `serialize:"true"`
}

func Parse(genesisBytes []byte) (*Genesis, error) {
	gen := &Genesis{}
	if _, err := Codec.Unmarshal(genesisBytes, gen); err != nil {
		return nil, err
	}
	for _, tx := range gen.Validators {
		if err := tx.Initialize(txs.GenesisCodec); err != nil {
			return nil, err
		}
	}
	for _, tx := range gen.Chains {
		if err := tx.Initialize(txs.GenesisCodec); err != nil {
			return nil, err
		}
	}
	return gen, nil
}

// Allocation is a UTXO on the Platform Chain that exists at the chain's genesis
type Allocation struct {
	Locktime uint64
	Amount   uint64
	Address  string
	Message  []byte
}

// Compare compares two allocations
func (a Allocation) Compare(other Allocation) int {
	if locktimeCmp := cmp.Compare(a.Locktime, other.Locktime); locktimeCmp != 0 {
		return locktimeCmp
	}
	if amountCmp := cmp.Compare(a.Amount, other.Amount); amountCmp != 0 {
		return amountCmp
	}

	addr, err := bech32ToID(a.Address)
	if err != nil {
		return 0
	}

	otherAddr, err := bech32ToID(other.Address)
	if err != nil {
		return 0
	}

	return addr.Compare(otherAddr)
}

// Validator represents a validator at genesis
type Validator struct {
	TxID      ids.ID
	StartTime uint64
	EndTime   uint64
	Weight    uint64
	NodeID    ids.NodeID
}

// Owner is the repr. of a reward owner at genesis
type Owner struct {
	Locktime  uint64
	Threshold uint32
	Addresses []string
}

// GenesisPermissionlessValidator represents a permissionless validator at genesis
type PermissionlessValidator struct {
	Validator
	RewardOwner        *Owner
	DelegationFee      float32
	ExactDelegationFee uint32
	Staked             []Allocation
	Signer             *signer.ProofOfPossession
}

// Chain defines a chain that exists at the network's genesis
// [GenesisData] is the initial state of the chain.
// [VMID] is the ID of the VM this chain runs.
// [FxIDs] are the IDs of the Fxs the chain supports.
// [Name] is a human-readable, non-unique name for the chain.
// [SubnetID] is the ID of the subnet that validates the chain
type Chain struct {
	GenesisData []byte
	VMID        ids.ID
	FxIDs       []ids.ID
	Name        string
	SubnetID    ids.ID
}

// bech32ToID takes bech32 address and produces a shortID
func bech32ToID(addrStr string) (ids.ShortID, error) {
	_, addrBytes, err := address.ParseBech32(addrStr)
	if err != nil {
		return ids.ShortID{}, err
	}
	return ids.ToShortID(addrBytes)
}

// New builds the genesis state of the P-Chain (and thereby the Avalanche network.)
// [avaxAssetID] is the ID of the AVAX asset
// [networkID] is the ID of the network
// [allocations] are the UTXOs on the Platform Chain that exist at genesis.
// [validators] are the validators of the primary network at genesis.
// [chains] are the chains that exist at genesis.
// [time] is the Platform Chain's time at network genesis.
// [initialSupply] is the initial supply of the AVAX asset.
// [message] is the message to be sent to the genesis UTXOs.
func New(
	avaxAssetID ids.ID,
	networkID uint32,
	allocations []Allocation,
	validators []PermissionlessValidator,
	chains []Chain,
	time uint64,
	initialSupply uint64,
	message string,
) (*Genesis, error) {
	// Specify the UTXOs on the Platform chain that exist at genesis
	utxos := make([]*UTXO, 0, len(allocations))
	for i, allocation := range allocations {
		if allocation.Amount == 0 {
			return nil, errUTXOHasNoValue
		}
		addrID, err := bech32ToID(allocation.Address)
		if err != nil {
			return nil, err
		}

		utxo := avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        ids.Empty,
				OutputIndex: uint32(i),
			},
			Asset: avax.Asset{ID: avaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: allocation.Amount,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{addrID},
				},
			},
		}
		if allocation.Locktime > time {
			utxo.Out = &stakeable.LockOut{
				Locktime:        allocation.Locktime,
				TransferableOut: utxo.Out.(avax.TransferableOut),
			}
		}
		if err != nil {
			return nil, fmt.Errorf("problem decoding UTXO message bytes: %w", err)
		}
		utxos = append(utxos, &UTXO{
			UTXO:    utxo,
			Message: allocation.Message,
		})
	}

	// Specify the validators that are validating the primary network at genesis
	vdrs := txheap.NewByEndTime()
	for _, vdr := range validators {
		weight := uint64(0)
		stake := make([]*avax.TransferableOutput, len(vdr.Staked))
		utils.Sort(vdr.Staked)
		for i, allocation := range vdr.Staked {
			addrID, err := bech32ToID(allocation.Address)
			if err != nil {
				return nil, err
			}

			utxo := &avax.TransferableOutput{
				Asset: avax.Asset{ID: avaxAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: allocation.Amount,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{addrID},
					},
				},
			}
			if allocation.Locktime > time {
				utxo.Out = &stakeable.LockOut{
					Locktime:        allocation.Locktime,
					TransferableOut: utxo.Out,
				}
			}
			stake[i] = utxo

			newWeight, err := math.Add(weight, allocation.Amount)
			if err != nil {
				return nil, errStakeOverflow
			}
			weight = newWeight
		}

		if weight == 0 {
			return nil, errValidatorHasNoWeight
		}
		if vdr.EndTime <= time {
			return nil, errValidatorAlreadyExited
		}

		owner := &secp256k1fx.OutputOwners{
			Locktime:  vdr.RewardOwner.Locktime,
			Threshold: vdr.RewardOwner.Threshold,
		}
		for _, addrStr := range vdr.RewardOwner.Addresses {
			addrID, err := bech32ToID(addrStr)
			if err != nil {
				return nil, err
			}
			owner.Addrs = append(owner.Addrs, addrID)
		}
		utils.Sort(owner.Addrs)

		delegationFee := vdr.ExactDelegationFee

		var (
			baseTx = txs.BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    networkID,
				BlockchainID: ids.Empty,
			}}
			validator = txs.Validator{
				NodeID: vdr.NodeID,
				Start:  time,
				End:    vdr.EndTime,
				Wght:   weight,
			}
			tx *txs.Tx
		)
		if vdr.Signer == nil {
			tx = &txs.Tx{Unsigned: &txs.AddValidatorTx{
				BaseTx:           baseTx,
				Validator:        validator,
				StakeOuts:        stake,
				RewardsOwner:     owner,
				DelegationShares: delegationFee,
			}}
		} else {
			tx = &txs.Tx{Unsigned: &txs.AddPermissionlessValidatorTx{
				BaseTx:                baseTx,
				Validator:             validator,
				Signer:                vdr.Signer,
				StakeOuts:             stake,
				ValidatorRewardsOwner: owner,
				DelegatorRewardsOwner: owner,
				DelegationShares:      delegationFee,
			}}
		}

		if err := tx.Initialize(txs.GenesisCodec); err != nil {
			return nil, err
		}

		vdrs.Add(tx)
	}

	// Specify the chains that exist at genesis
	chainsTxs := []*txs.Tx{}
	for _, chain := range chains {
		tx := &txs.Tx{Unsigned: &txs.CreateChainTx{
			BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    networkID,
				BlockchainID: ids.Empty,
			}},
			SubnetID:    chain.SubnetID,
			ChainName:   chain.Name,
			VMID:        chain.VMID,
			FxIDs:       chain.FxIDs,
			GenesisData: chain.GenesisData,
			SubnetAuth:  &secp256k1fx.Input{},
		}}
		if err := tx.Initialize(txs.GenesisCodec); err != nil {
			return nil, err
		}

		chainsTxs = append(chainsTxs, tx)
	}

	validatorTxs := vdrs.List()

	g := &Genesis{
		UTXOs:         utxos,
		Validators:    validatorTxs,
		Chains:        chainsTxs,
		Timestamp:     time,
		InitialSupply: initialSupply,
		Message:       message,
	}

	return g, nil
}

// Bytes serializes the Genesis to bytes using the PlatformVM genesis codec
func (g *Genesis) Bytes() ([]byte, error) {
	return Codec.Marshal(CodecVersion, g)
}
