// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"net/http"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/constants"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/json"
	"github.com/ava-labs/gecko/vms/components/avax"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

// Note that since an Avalanche network has exactly one Platform Chain,
// and the Platform Chain defines the genesis state of the network
// (who is staking, which chains exist, etc.), defining the genesis
// state of the Platform Chain is the same as defining the genesis
// state of the network.

var (
	errUTXOHasNoValue       = errors.New("genesis UTXO has no value")
	errValidatorAddsNoValue = errors.New("validator would have already unstaked")
)

// StaticService defines the static API methods exposed by the platform VM
type StaticService struct{}

// APIUTXO is a UTXO on the Platform Chain that exists at the chain's genesis.
type APIUTXO struct {
	Amount  json.Uint64 `json:"amount"`
	Address string      `json:"address"`
}

// APIValidator is a validator.
// [Amount] is the amount of tokens being staked.
// [Endtime] is the Unix time repr. of when they are done staking
// [ID] is the node ID of the staker
// [Address] is the address where the staked $AVAX (and, if applicable, reward)
// is sent when this staker is done staking.
type APIValidator struct {
	StartTime   json.Uint64  `json:"startTime"`
	EndTime     json.Uint64  `json:"endTime"`
	Weight      *json.Uint64 `json:"weight,omitempty"`
	StakeAmount *json.Uint64 `json:"stakeAmount,omitempty"`
	Address     string       `json:"address,omitempty"`
	ID          ids.ShortID  `json:"id"`
}

// APIPrimaryValidator is a validator of the primary network
type APIPrimaryValidator struct {
	APIValidator

	RewardAddress     string      `json:"rewardAddress"`
	DelegationFeeRate json.Uint32 `json:"delegationFeeRate"`
}

// FormattedAPIValidator allows for a formatted address
type FormattedAPIValidator struct {
	StartTime   json.Uint64   `json:"startTime"`
	EndTime     json.Uint64   `json:"endTime"`
	Weight      *json.Uint64  `json:"weight,omitempty"`
	StakeAmount *json.Uint64  `json:"stakeAmount,omitempty"`
	Uptime      *json.Float32 `json:"uptime,omitempty"`
	Connected   *bool         `json:"connected,omitempty"`
	ID          string        `json:"nodeID"`
}

func (v *FormattedAPIValidator) weight() uint64 {
	switch {
	case v.Weight != nil:
		return uint64(*v.Weight)
	case v.StakeAmount != nil:
		return uint64(*v.StakeAmount)
	default:
		return 0
	}
}

// FormattedAPIPrimaryValidator is a formatted validator of the primary network
type FormattedAPIPrimaryValidator struct {
	FormattedAPIValidator

	RewardAddress string `json:"rewardAddress"`

	// Delegation fee rate as a percentage. Must be in [0,100].
	DelegationFeeRate json.Float32 `json:"delegationFeeRate"`
}

// APIChain defines a chain that exists
// at the network's genesis.
// [GenesisData] is the initial state of the chain.
// [VMID] is the ID of the VM this chain runs.
// [FxIDs] are the IDs of the Fxs the chain supports.
// [Name] is a human-readable, non-unique name for the chain.
// [SubnetID] is the ID of the subnet that validates the chain
type APIChain struct {
	GenesisData formatting.CB58 `json:"genesisData"`
	VMID        ids.ID          `json:"vmID"`
	FxIDs       []ids.ID        `json:"fxIDs"`
	Name        string          `json:"name"`
	SubnetID    ids.ID          `json:"subnetID"`
}

// BuildGenesisArgs are the arguments used to create
// the genesis data of the Platform Chain.
// [NetworkID] is the ID of the network
// [UTXOs] are the UTXOs on the Platform Chain that exist at genesis.
// [Validators] are the validators of the primary network at genesis.
// [Chains] are the chains that exist at genesis.
// [Time] is the Platform Chain's time at network genesis.
type BuildGenesisArgs struct {
	AvaxAssetID ids.ID                         `json:"avaxAssetID"`
	NetworkID   json.Uint32                    `json:"address"`
	UTXOs       []APIUTXO                      `json:"utxos"`
	Validators  []FormattedAPIPrimaryValidator `json:"primaryNetworkValidators"`
	Chains      []APIChain                     `json:"chains"`
	Time        json.Uint64                    `json:"time"`
	Message     string                         `json:"message"`
}

// BuildGenesisReply is the reply from BuildGenesis
type BuildGenesisReply struct {
	Bytes formatting.CB58 `json:"bytes"`
}

// Genesis represents a genesis state of the platform chain
type Genesis struct {
	UTXOs      []*avax.UTXO `serialize:"true"`
	Validators []*Tx        `serialize:"true"`
	Chains     []*Tx        `serialize:"true"`
	Timestamp  uint64       `serialize:"true"`
	Message    string       `serialize:"true"`
}

// Initialize ...
func (g *Genesis) Initialize() error {
	for _, tx := range g.Validators {
		if err := tx.Sign(Codec, nil); err != nil {
			return err
		}
	}
	for _, tx := range g.Chains {
		if err := tx.Sign(Codec, nil); err != nil {
			return err
		}
	}
	return nil
}

// beck32ToID takes bech32 address and produces a shortID
func bech32ToID(address string) (ids.ShortID, error) {
	_, addr, err := formatting.ParseBech32(address)
	if err != nil {
		return ids.ShortID{}, err
	}
	return ids.ToShortID(addr)
}

// BuildGenesis build the genesis state of the Platform Chain (and thereby the Avalanche network.)
func (ss *StaticService) BuildGenesis(_ *http.Request, args *BuildGenesisArgs, reply *BuildGenesisReply) error {
	// Specify the UTXOs on the Platform chain that exist at genesis.
	utxos := make([]*avax.UTXO, 0, len(args.UTXOs))
	for i, utxo := range args.UTXOs {
		if utxo.Amount == 0 {
			return errUTXOHasNoValue
		}
		addrID, err := bech32ToID(utxo.Address)
		if err != nil {
			return err
		}
		utxos = append(utxos, &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        ids.Empty,
				OutputIndex: uint32(i),
			},
			Asset: avax.Asset{ID: args.AvaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: uint64(utxo.Amount),
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{addrID},
				},
			},
		})
	}

	// Specify the validators that are validating the primary network at genesis.
	validators := &EventHeap{}
	for _, validator := range args.Validators {
		weight := validator.weight()
		if weight == 0 {
			return errValidatorAddsNoValue
		}
		if uint64(validator.EndTime) <= uint64(args.Time) {
			return errValidatorAddsNoValue
		}
		addrID, err := bech32ToID(validator.RewardAddress)
		if err != nil {
			return err
		}
		nodeID, err := ids.ShortFromPrefixedString(validator.ID, constants.NodeIDPrefix)
		if err != nil {
			return err
		}

		tx := &Tx{UnsignedTx: &UnsignedAddValidatorTx{
			BaseTx: BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    uint32(args.NetworkID),
				BlockchainID: ids.Empty,
			}},
			Validator: Validator{
				NodeID: nodeID,
				Start:  uint64(args.Time),
				End:    uint64(validator.EndTime),
				Wght:   weight,
			},
			Stake: []*avax.TransferableOutput{{
				Asset: avax.Asset{ID: args.AvaxAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: weight,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{addrID},
					},
				},
			}},
			RewardsOwner: &secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{addrID},
			},
		}}
		if err := tx.Sign(Codec, nil); err != nil {
			return err
		}

		validators.Add(tx)
	}

	// Specify the chains that exist at genesis.
	chains := []*Tx{}
	for _, chain := range args.Chains {
		tx := &Tx{UnsignedTx: &UnsignedCreateChainTx{
			BaseTx: BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    uint32(args.NetworkID),
				BlockchainID: ids.Empty,
			}},
			SubnetID:    chain.SubnetID,
			ChainName:   chain.Name,
			VMID:        chain.VMID,
			FxIDs:       chain.FxIDs,
			GenesisData: chain.GenesisData.Bytes,
			SubnetAuth:  &secp256k1fx.OutputOwners{},
		}}
		if err := tx.Sign(Codec, nil); err != nil {
			return err
		}

		chains = append(chains, tx)
	}

	// genesis holds the genesis state
	genesis := Genesis{
		UTXOs:      utxos,
		Validators: validators.Txs,
		Chains:     chains,
		Timestamp:  uint64(args.Time),
		Message:    args.Message,
	}

	// Marshal genesis to bytes
	bytes, err := Codec.Marshal(genesis)
	reply.Bytes.Bytes = bytes
	return err
}
