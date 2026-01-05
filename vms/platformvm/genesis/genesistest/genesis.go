// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesistest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	platformvmgenesis "github.com/ava-labs/avalanchego/vms/platformvm/genesis"
)

const (
	DefaultValidatorDuration = 28 * 24 * time.Hour
	DefaultValidatorWeight   = 5 * units.MilliAvax
	DefaultInitialBalance    = 1 * units.Avax

	ValidatorDelegationShares = reward.PercentDenominator
	XChainName                = "x"
	InitialSupply             = 360 * units.MegaAvax
)

var (
	AVAXAsset = avax.Asset{ID: snowtest.AVAXAssetID}

	DefaultValidatorStartTime     = upgrade.InitiallyActiveTime
	DefaultValidatorStartTimeUnix = uint64(DefaultValidatorStartTime.Unix())
	DefaultValidatorEndTime       = DefaultValidatorStartTime.Add(DefaultValidatorDuration)
	DefaultValidatorEndTimeUnix   = uint64(DefaultValidatorEndTime.Unix())
)

var (
	// Keys that are funded in the genesis
	DefaultFundedKeys = secp256k1.TestKeys()

	// Node IDs of genesis validators
	DefaultNodeIDs []ids.NodeID
)

func init() {
	DefaultNodeIDs = make([]ids.NodeID, len(DefaultFundedKeys))
	for i := range DefaultFundedKeys {
		DefaultNodeIDs[i] = ids.GenerateTestNodeID()
	}
}

type Config struct {
	NetworkID          uint32
	NodeIDs            []ids.NodeID
	ValidatorWeight    uint64
	ValidatorStartTime time.Time
	ValidatorEndTime   time.Time

	FundedKeys     []*secp256k1.PrivateKey
	InitialBalance uint64
}

func New(t testing.TB, c Config) *platformvmgenesis.Genesis {
	if c.NetworkID == 0 {
		c.NetworkID = constants.UnitTestID
	}
	if len(c.NodeIDs) == 0 {
		c.NodeIDs = DefaultNodeIDs
	}
	if c.ValidatorWeight == 0 {
		c.ValidatorWeight = DefaultValidatorWeight
	}
	if c.ValidatorStartTime.IsZero() {
		c.ValidatorStartTime = DefaultValidatorStartTime
	}
	if c.ValidatorEndTime.IsZero() {
		c.ValidatorEndTime = DefaultValidatorEndTime
	}
	if len(c.FundedKeys) == 0 {
		c.FundedKeys = DefaultFundedKeys
	}
	if c.InitialBalance == 0 {
		c.InitialBalance = DefaultInitialBalance
	}

	require := require.New(t)

	genesis := &platformvmgenesis.Genesis{
		UTXOs:         make([]*platformvmgenesis.UTXO, len(c.FundedKeys)),
		Validators:    make([]*txs.Tx, len(c.NodeIDs)),
		Timestamp:     uint64(c.ValidatorStartTime.Unix()),
		InitialSupply: InitialSupply,
	}
	for i, key := range c.FundedKeys {
		genesis.UTXOs[i] = &platformvmgenesis.UTXO{UTXO: avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        snowtest.AVAXAssetID,
				OutputIndex: uint32(i),
			},
			Asset: AVAXAsset,
			Out: &secp256k1fx.TransferOutput{
				Amt: c.InitialBalance,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs: []ids.ShortID{
						key.Address(),
					},
				},
			},
		}}
	}
	for i, nodeID := range c.NodeIDs {
		key := c.FundedKeys[i%len(c.FundedKeys)]
		owner := secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				key.Address(),
			},
		}
		validator := &txs.AddValidatorTx{
			BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    c.NetworkID,
				BlockchainID: constants.PlatformChainID,
			}},
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(c.ValidatorStartTime.Unix()),
				End:    uint64(c.ValidatorEndTime.Unix()),
				Wght:   c.ValidatorWeight,
			},
			StakeOuts: []*avax.TransferableOutput{
				{
					Asset: AVAXAsset,
					Out: &secp256k1fx.TransferOutput{
						Amt:          c.ValidatorWeight,
						OutputOwners: owner,
					},
				},
			},
			RewardsOwner:     &owner,
			DelegationShares: ValidatorDelegationShares,
		}
		validatorTx := &txs.Tx{Unsigned: validator}
		require.NoError(validatorTx.Initialize(txs.GenesisCodec))

		genesis.Validators[i] = validatorTx
	}

	chain := &txs.CreateChainTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    c.NetworkID,
			BlockchainID: constants.PlatformChainID,
		}},
		SubnetID:   constants.PrimaryNetworkID,
		ChainName:  XChainName,
		VMID:       constants.AVMID,
		SubnetAuth: &secp256k1fx.Input{},
	}
	chainTx := &txs.Tx{Unsigned: chain}
	require.NoError(chainTx.Initialize(txs.GenesisCodec))

	genesis.Chains = []*txs.Tx{chainTx}
	return genesis
}

func NewBytes(t testing.TB, c Config) []byte {
	g := New(t, c)
	genesisBytes, err := platformvmgenesis.Codec.Marshal(platformvmgenesis.CodecVersion, g)
	require.NoError(t, err)
	return genesisBytes
}
