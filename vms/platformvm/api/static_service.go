// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"cmp"
	"errors"
	"fmt"
	"net/http"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
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

	_ utils.Sortable[UTXO] = UTXO{}
)

// StaticService defines the static API methods exposed by the platform VM
type StaticService struct{}

// UTXO is a UTXO on the Platform Chain that exists at the chain's genesis.
type UTXO struct {
	Locktime json.Uint64 `json:"locktime"`
	Amount   json.Uint64 `json:"amount"`
	Address  string      `json:"address"`
	Message  string      `json:"message"`
}

// TODO can we define this on *UTXO?
func (utxo UTXO) Compare(other UTXO) int {
	if locktimeCmp := cmp.Compare(utxo.Locktime, other.Locktime); locktimeCmp != 0 {
		return locktimeCmp
	}
	if amountCmp := cmp.Compare(utxo.Amount, other.Amount); amountCmp != 0 {
		return amountCmp
	}

	utxoAddr, err := bech32ToID(utxo.Address)
	if err != nil {
		return 0
	}

	otherAddr, err := bech32ToID(other.Address)
	if err != nil {
		return 0
	}

	return utxoAddr.Compare(otherAddr)
}

// TODO: Refactor APIStaker, APIValidators and merge them together for
//       PermissionedValidators + PermissionlessValidators.

// APIStaker is the representation of a staker sent via APIs.
// [TxID] is the txID of the transaction that added this staker.
// [Amount] is the amount of tokens being staked.
// [StartTime] is the Unix time when they start staking
// [Endtime] is the Unix time repr. of when they are done staking
// [NodeID] is the node ID of the staker
// [Uptime] is the observed uptime of this staker
type Staker struct {
	TxID      ids.ID      `json:"txID"`
	StartTime json.Uint64 `json:"startTime"`
	EndTime   json.Uint64 `json:"endTime"`
	Weight    json.Uint64 `json:"weight"`
	NodeID    ids.NodeID  `json:"nodeID"`

	// Deprecated: Use Weight instead
	// TODO: remove [StakeAmount] after enough time for dependencies to update
	StakeAmount *json.Uint64 `json:"stakeAmount,omitempty"`
}

// GenesisValidator should to be used for genesis validators only.
type GenesisValidator Staker

// Owner is the repr. of a reward owner sent over APIs.
type Owner struct {
	Locktime  json.Uint64 `json:"locktime"`
	Threshold json.Uint32 `json:"threshold"`
	Addresses []string    `json:"addresses"`
}

// PermissionlessValidator is the repr. of a permissionless validator sent over
// APIs.
type PermissionlessValidator struct {
	Staker
	// Deprecated: RewardOwner has been replaced by ValidationRewardOwner and
	//             DelegationRewardOwner.
	RewardOwner *Owner `json:"rewardOwner,omitempty"`
	// The owner of the rewards from the validation period, if applicable.
	ValidationRewardOwner *Owner `json:"validationRewardOwner,omitempty"`
	// The owner of the rewards from delegations during the validation period,
	// if applicable.
	DelegationRewardOwner  *Owner                    `json:"delegationRewardOwner,omitempty"`
	PotentialReward        *json.Uint64              `json:"potentialReward,omitempty"`
	AccruedDelegateeReward *json.Uint64              `json:"accruedDelegateeReward,omitempty"`
	DelegationFee          json.Float32              `json:"delegationFee"`
	ExactDelegationFee     *json.Uint32              `json:"exactDelegationFee,omitempty"`
	Uptime                 *json.Float32             `json:"uptime,omitempty"`
	Connected              *bool                     `json:"connected,omitempty"`
	Staked                 []UTXO                    `json:"staked,omitempty"`
	Signer                 *signer.ProofOfPossession `json:"signer,omitempty"`

	// The delegators delegating to this validator
	DelegatorCount  *json.Uint64        `json:"delegatorCount,omitempty"`
	DelegatorWeight *json.Uint64        `json:"delegatorWeight,omitempty"`
	Delegators      *[]PrimaryDelegator `json:"delegators,omitempty"`
}

// GenesisPermissionlessValidator should to be used for genesis validators only.
type GenesisPermissionlessValidator struct {
	GenesisValidator
	RewardOwner        *Owner                    `json:"rewardOwner,omitempty"`
	DelegationFee      json.Float32              `json:"delegationFee"`
	ExactDelegationFee *json.Uint32              `json:"exactDelegationFee,omitempty"`
	Staked             []UTXO                    `json:"staked,omitempty"`
	Signer             *signer.ProofOfPossession `json:"signer,omitempty"`
}

// PermissionedValidator is the repr. of a permissioned validator sent over APIs.
type PermissionedValidator struct {
	Staker
	// The owner the staking reward, if applicable, will go to
	Connected *bool         `json:"connected,omitempty"`
	Uptime    *json.Float32 `json:"uptime,omitempty"`
}

// PrimaryDelegator is the repr. of a primary network delegator sent over APIs.
type PrimaryDelegator struct {
	Staker
	RewardOwner     *Owner       `json:"rewardOwner,omitempty"`
	PotentialReward *json.Uint64 `json:"potentialReward,omitempty"`
}

// Chain defines a chain that exists
// at the network's genesis.
// [GenesisData] is the initial state of the chain.
// [VMID] is the ID of the VM this chain runs.
// [FxIDs] are the IDs of the Fxs the chain supports.
// [Name] is a human-readable, non-unique name for the chain.
// [SubnetID] is the ID of the subnet that validates the chain
type Chain struct {
	GenesisData string   `json:"genesisData"`
	VMID        ids.ID   `json:"vmID"`
	FxIDs       []ids.ID `json:"fxIDs"`
	Name        string   `json:"name"`
	SubnetID    ids.ID   `json:"subnetID"`
}

// BuildGenesisArgs are the arguments used to create
// the genesis data of the Platform Chain.
// [NetworkID] is the ID of the network
// [UTXOs] are the UTXOs on the Platform Chain that exist at genesis.
// [Validators] are the validators of the primary network at genesis.
// [Chains] are the chains that exist at genesis.
// [Time] is the Platform Chain's time at network genesis.
type BuildGenesisArgs struct {
	AvaxAssetID   ids.ID                           `json:"avaxAssetID"`
	NetworkID     json.Uint32                      `json:"networkID"`
	UTXOs         []UTXO                           `json:"utxos"`
	Validators    []GenesisPermissionlessValidator `json:"validators"`
	Chains        []Chain                          `json:"chains"`
	Time          json.Uint64                      `json:"time"`
	InitialSupply json.Uint64                      `json:"initialSupply"`
	Message       string                           `json:"message"`
	Encoding      formatting.Encoding              `json:"encoding"`
}

// BuildGenesisReply is the reply from BuildGenesis
type BuildGenesisReply struct {
	Bytes    string              `json:"bytes"`
	Encoding formatting.Encoding `json:"encoding"`
}

// bech32ToID takes bech32 address and produces a shortID
func bech32ToID(addrStr string) (ids.ShortID, error) {
	_, addrBytes, err := address.ParseBech32(addrStr)
	if err != nil {
		return ids.ShortID{}, err
	}
	return ids.ToShortID(addrBytes)
}

// BuildGenesis build the genesis state of the Platform Chain (and thereby the Avalanche network.)
func (*StaticService) BuildGenesis(_ *http.Request, args *BuildGenesisArgs, reply *BuildGenesisReply) error {
	// Specify the UTXOs on the Platform chain that exist at genesis.
	utxos := make([]*genesis.UTXO, 0, len(args.UTXOs))
	for i, apiUTXO := range args.UTXOs {
		if apiUTXO.Amount == 0 {
			return errUTXOHasNoValue
		}
		addrID, err := bech32ToID(apiUTXO.Address)
		if err != nil {
			return err
		}

		utxo := avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        ids.Empty,
				OutputIndex: uint32(i),
			},
			Asset: avax.Asset{ID: args.AvaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: uint64(apiUTXO.Amount),
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{addrID},
				},
			},
		}
		if apiUTXO.Locktime > args.Time {
			utxo.Out = &stakeable.LockOut{
				Locktime:        uint64(apiUTXO.Locktime),
				TransferableOut: utxo.Out.(avax.TransferableOut),
			}
		}
		messageBytes, err := formatting.Decode(args.Encoding, apiUTXO.Message)
		if err != nil {
			return fmt.Errorf("problem decoding UTXO message bytes: %w", err)
		}
		utxos = append(utxos, &genesis.UTXO{
			UTXO:    utxo,
			Message: messageBytes,
		})
	}

	// Specify the validators that are validating the primary network at genesis.
	vdrs := txheap.NewByEndTime()
	for _, vdr := range args.Validators {
		weight := uint64(0)
		stake := make([]*avax.TransferableOutput, len(vdr.Staked))
		utils.Sort(vdr.Staked)
		for i, apiUTXO := range vdr.Staked {
			addrID, err := bech32ToID(apiUTXO.Address)
			if err != nil {
				return err
			}

			utxo := &avax.TransferableOutput{
				Asset: avax.Asset{ID: args.AvaxAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: uint64(apiUTXO.Amount),
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{addrID},
					},
				},
			}
			if apiUTXO.Locktime > args.Time {
				utxo.Out = &stakeable.LockOut{
					Locktime:        uint64(apiUTXO.Locktime),
					TransferableOut: utxo.Out,
				}
			}
			stake[i] = utxo

			newWeight, err := math.Add(weight, uint64(apiUTXO.Amount))
			if err != nil {
				return errStakeOverflow
			}
			weight = newWeight
		}

		if weight == 0 {
			return errValidatorHasNoWeight
		}
		if uint64(vdr.EndTime) <= uint64(args.Time) {
			return errValidatorAlreadyExited
		}

		owner := &secp256k1fx.OutputOwners{
			Locktime:  uint64(vdr.RewardOwner.Locktime),
			Threshold: uint32(vdr.RewardOwner.Threshold),
		}
		for _, addrStr := range vdr.RewardOwner.Addresses {
			addrID, err := bech32ToID(addrStr)
			if err != nil {
				return err
			}
			owner.Addrs = append(owner.Addrs, addrID)
		}
		utils.Sort(owner.Addrs)

		delegationFee := uint32(0)
		if vdr.ExactDelegationFee != nil {
			delegationFee = uint32(*vdr.ExactDelegationFee)
		}

		var (
			baseTx = txs.BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    uint32(args.NetworkID),
				BlockchainID: ids.Empty,
			}}
			validator = txs.Validator{
				NodeID: vdr.NodeID,
				Start:  uint64(args.Time),
				End:    uint64(vdr.EndTime),
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
			return err
		}

		vdrs.Add(tx)
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
		if err := tx.Initialize(txs.GenesisCodec); err != nil {
			return err
		}

		chains = append(chains, tx)
	}

	validatorTxs := vdrs.List()

	// genesis holds the genesis state
	g := genesis.Genesis{
		UTXOs:         utxos,
		Validators:    validatorTxs,
		Chains:        chains,
		Timestamp:     uint64(args.Time),
		InitialSupply: uint64(args.InitialSupply),
		Message:       args.Message,
	}

	// Marshal genesis to bytes
	bytes, err := genesis.Codec.Marshal(genesis.CodecVersion, g)
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
