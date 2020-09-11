// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/codec"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// ID of the EVM VM
var (
	EVMID = ids.NewID([32]byte{'e', 'v', 'm'})
)

// Genesis returns the genesis data of the Platform Chain.
//
// Since an Avalanche network has exactly one Platform Chain, and the Platform
// Chain defines the genesis state of the network (who is staking, which chains
// exist, etc.), defining the genesis state of the Platform Chain is the same as
// defining the genesis state of the network.
//
// The ID of the new network is [networkID].

// FromConfig returns:
// 1) The byte representation of the genesis state of the platform chain
//    (ie the genesis state of the network)
// 2) The asset ID of AVAX
func FromConfig(config *Config) ([]byte, ids.ID, error) {
	if err := config.init(); err != nil {
		return nil, ids.ID{}, err
	}

	initialSupply := uint64(0)

	// Specify the genesis state of the AVM
	avmArgs := avm.BuildGenesisArgs{}
	{
		avax := avm.AssetDefinition{
			Name:         "AVAX",
			Symbol:       "AVAX",
			Denomination: 9,
			InitialState: map[string][]interface{}{},
		}

		if len(config.MintAddresses) > 0 {
			avax.InitialState["variableCap"] = []interface{}{avm.Owners{
				Threshold: 1,
				Minters:   config.MintAddresses,
			}}
		}
		for _, addr := range config.FundedAddresses {
			avax.InitialState["fixedCap"] = append(avax.InitialState["fixedCap"], avm.Holder{
				Amount:  json.Uint64(5 * units.MegaAvax),
				Address: addr,
			})
			initialSupply += 5 * units.MegaAvax
		}

		avmArgs.GenesisData = map[string]avm.AssetDefinition{
			"AVAX": avax, // The AVM starts out with one asset: AVAX
		}
	}
	avmReply := avm.BuildGenesisReply{}

	avmSS := avm.StaticService{}
	err := avmSS.BuildGenesis(nil, &avmArgs, &avmReply)
	if err != nil {
		return nil, ids.ID{}, err
	}

	avaxAssetID, err := AVAXAssetID(avmReply.Bytes.Bytes)
	if err != nil {
		return nil, ids.ID{}, fmt.Errorf("couldn't generate AVAX asset ID: %w", err)
	}

	genesisTime := time.Date(
		/*year=*/ 2019,
		/*month=*/ time.November,
		/*day=*/ 1,
		/*hour=*/ 0,
		/*minute=*/ 0,
		/*second=*/ 0,
		/*nano-second=*/ 0,
		/*location=*/ time.UTC,
	)

	// Specify the initial state of the Platform Chain
	platformvmArgs := platformvm.BuildGenesisArgs{
		NetworkID:   json.Uint32(config.NetworkID),
		AvaxAssetID: avaxAssetID,
		Time:        json.Uint64(genesisTime.Unix()),
		Message:     config.Message,
	}
	for _, addr := range config.FundedAddresses {
		platformvmArgs.UTXOs = append(platformvmArgs.UTXOs,
			platformvm.APIUTXO{
				Address: addr,
				Amount:  json.Uint64(5 * units.MegaAvax),
			},
		)
		initialSupply += 5 * units.MegaAvax
	}

	stakingDuration := 365 * 24 * time.Hour // ~ 1 year
	endStakingTime := genesisTime.Add(stakingDuration)

	for i, validatorID := range config.ParsedStakerIDs {
		weight := json.Uint64(20 * units.KiloAvax)
		destAddr := config.FundedAddresses[i%len(config.FundedAddresses)]
		platformvmArgs.Validators = append(platformvmArgs.Validators,
			platformvm.APIPrimaryValidator{
				APIStaker: platformvm.APIStaker{
					StartTime: json.Uint64(genesisTime.Unix()),
					EndTime:   json.Uint64(endStakingTime.Unix()),
					Weight:    &weight,
					NodeID:    validatorID.PrefixedString(constants.NodeIDPrefix),
				},
				RewardOwner: &platformvm.APIOwner{
					Threshold: 1,
					Addresses: []string{destAddr},
				},
			},
		)
		initialSupply += 20 * units.KiloAvax
	}

	// Specify the chains that exist upon this network's creation
	platformvmArgs.Chains = []platformvm.APIChain{
		{
			GenesisData: avmReply.Bytes,
			SubnetID:    constants.PrimaryNetworkID,
			VMID:        avm.ID,
			FxIDs: []ids.ID{
				secp256k1fx.ID,
				nftfx.ID,
				propertyfx.ID,
			},
			Name: "X-Chain",
		},
		{
			GenesisData: formatting.CB58{Bytes: config.EVMBytes},
			SubnetID:    constants.PrimaryNetworkID,
			VMID:        EVMID,
			Name:        "C-Chain",
		},
	}

	platformvmArgs.InitialSupply = json.Uint64(initialSupply)
	platformvm.InitialSupply = initialSupply

	platformvmReply := platformvm.BuildGenesisReply{}
	platformvmSS := platformvm.StaticService{}
	if err := platformvmSS.BuildGenesis(nil, &platformvmArgs, &platformvmReply); err != nil {
		return nil, ids.ID{}, fmt.Errorf("problem while building platform chain's genesis state: %w", err)
	}

	return platformvmReply.Bytes.Bytes, avaxAssetID, nil
}

// Genesis returns:
// 1) The byte representation of the genesis state of the platform chain
//    (ie the genesis state of the network)
// 2) The asset ID of AVAX
func Genesis(networkID uint32) ([]byte, ids.ID, error) {
	return FromConfig(GetConfig(networkID))
}

// VMGenesis ...
func VMGenesis(networkID uint32, vmID ids.ID) (*platformvm.Tx, error) {
	genesisBytes, _, err := Genesis(networkID)
	if err != nil {
		return nil, err
	}
	genesis := platformvm.Genesis{}
	if err := platformvm.Codec.Unmarshal(genesisBytes, &genesis); err != nil {
		return nil, fmt.Errorf("couldn't unmarshal genesis bytes due to: %w", err)
	}
	if err := genesis.Initialize(); err != nil {
		return nil, err
	}
	for _, chain := range genesis.Chains {
		uChain := chain.UnsignedTx.(*platformvm.UnsignedCreateChainTx)
		if uChain.VMID.Equals(vmID) {
			return chain, nil
		}
	}
	return nil, fmt.Errorf("couldn't find blockchain with VM ID %s", vmID)
}

// AVAXAssetID ...
func AVAXAssetID(avmGenesisBytes []byte) (ids.ID, error) {
	c := codec.NewDefault()
	errs := wrappers.Errs{}
	errs.Add(
		c.RegisterType(&avm.BaseTx{}),
		c.RegisterType(&avm.CreateAssetTx{}),
		c.RegisterType(&avm.OperationTx{}),
		c.RegisterType(&avm.ImportTx{}),
		c.RegisterType(&avm.ExportTx{}),
		c.RegisterType(&secp256k1fx.TransferInput{}),
		c.RegisterType(&secp256k1fx.MintOutput{}),
		c.RegisterType(&secp256k1fx.TransferOutput{}),
		c.RegisterType(&secp256k1fx.MintOperation{}),
		c.RegisterType(&secp256k1fx.Credential{}),
	)
	if errs.Errored() {
		return ids.ID{}, errs.Err
	}

	genesis := avm.Genesis{}
	if err := c.Unmarshal(avmGenesisBytes, &genesis); err != nil {
		return ids.ID{}, err
	}

	if len(genesis.Txs) == 0 {
		return ids.ID{}, errors.New("genesis creates no transactions")
	}
	genesisTx := genesis.Txs[0]

	tx := avm.Tx{UnsignedTx: &genesisTx.CreateAssetTx}
	unsignedBytes, err := c.Marshal(tx.UnsignedTx)
	if err != nil {
		return ids.ID{}, err
	}
	signedBytes, err := c.Marshal(&tx)
	if err != nil {
		return ids.ID{}, err
	}
	tx.Initialize(unsignedBytes, signedBytes)

	return tx.ID(), nil
}
