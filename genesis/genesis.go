// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ava-labs/coreth/core"

	"github.com/ava-labs/go-ethereum/common"
	"github.com/ava-labs/go-ethereum/params"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/json"
	"github.com/ava-labs/gecko/utils/units"
	"github.com/ava-labs/gecko/utils/wrappers"
	"github.com/ava-labs/gecko/vms/avm"
	"github.com/ava-labs/gecko/vms/components/codec"
	"github.com/ava-labs/gecko/vms/evm"
	"github.com/ava-labs/gecko/vms/nftfx"
	"github.com/ava-labs/gecko/vms/platformvm"
	"github.com/ava-labs/gecko/vms/propertyfx"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
	"github.com/ava-labs/gecko/vms/spchainvm"
	"github.com/ava-labs/gecko/vms/spdagvm"
	"github.com/ava-labs/gecko/vms/timestampvm"
)

// Genesis returns the genesis data of the Platform Chain.
// Since an AVA network has exactly one Platform Chain, and the Platform Chain
// defines the genesis state of the network (who is staking, which chains exist,
// etc.), defining the genesis state of the Platform Chain is the same as
// defining the genesis state of the network.
// The ID of the new network is [networkID].

// FromConfig ...
func FromConfig(networkID uint32, config *Config) ([]byte, error) {
	if err := config.init(); err != nil {
		return nil, err
	}

	// Specify the genesis state of the AVM
	avmArgs := avm.BuildGenesisArgs{}
	{
		ava := avm.AssetDefinition{
			Name:         "AVA",
			Symbol:       "AVA",
			Denomination: 9,
			InitialState: map[string][]interface{}{},
		}

		if len(config.MintAddresses) > 0 {
			ava.InitialState["variableCap"] = []interface{}{avm.Owners{
				Threshold: 1,
				Minters:   config.MintAddresses,
			}}
		}
		for _, addr := range config.FundedAddresses {
			ava.InitialState["fixedCap"] = append(ava.InitialState["fixedCap"], avm.Holder{
				Amount:  json.Uint64(45 * units.MegaAva),
				Address: addr,
			})
		}

		avmArgs.GenesisData = map[string]avm.AssetDefinition{
			// The AVM starts out with one asset, $AVA
			"AVA": ava,
		}
	}
	avmReply := avm.BuildGenesisReply{}

	avmSS := avm.StaticService{}
	err := avmSS.BuildGenesis(nil, &avmArgs, &avmReply)
	if err != nil {
		panic(err)
	}

	// Specify the genesis state of Athereum (the built-in instance of the EVM)
	evmBalance, success := new(big.Int).SetString("33b2e3c9fd0804000000000", 16)
	if success != true {
		return nil, errors.New("problem creating evm genesis state")
	}

	alloc := core.GenesisAlloc{}
	for _, addr := range config.FundedEVMAddresses {
		alloc[common.HexToAddress(addr)] = core.GenesisAccount{
			Balance: evmBalance,
		}
	}
	evmArgs := core.Genesis{
		Config: &params.ChainConfig{
			ChainID:             big.NewInt(43110),
			HomesteadBlock:      big.NewInt(0),
			DAOForkBlock:        big.NewInt(0),
			DAOForkSupport:      true,
			EIP150Block:         big.NewInt(0),
			EIP150Hash:          common.HexToHash("0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0"),
			EIP155Block:         big.NewInt(0),
			EIP158Block:         big.NewInt(0),
			ByzantiumBlock:      big.NewInt(0),
			ConstantinopleBlock: big.NewInt(0),
			PetersburgBlock:     big.NewInt(0),
		},
		Nonce:      0,
		Timestamp:  0,
		ExtraData:  []byte{0},
		GasLimit:   100000000,
		Difficulty: big.NewInt(0),
		Mixhash:    common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
		Coinbase:   common.HexToAddress("0x0000000000000000000000000000000000000000"),
		Alloc:      alloc,
		Number:     0,
		GasUsed:    0,
		ParentHash: common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
	}
	evmSS := evm.StaticService{}
	evmReply, err := evmSS.BuildGenesis(nil, &evmArgs)
	if err != nil {
		return nil, err
	}

	// Specify the genesis state of the simple payments DAG
	spdagvmArgs := spdagvm.BuildGenesisArgs{}
	for _, addr := range config.ParsedFundedAddresses {
		spdagvmArgs.Outputs = append(spdagvmArgs.Outputs,
			spdagvm.APIOutput{
				Amount:    json.Uint64(20 * units.KiloAva),
				Threshold: 1,
				Addresses: []ids.ShortID{addr},
			},
		)
	}

	spdagvmReply := spdagvm.BuildGenesisReply{}
	spdagvmSS := spdagvm.StaticService{}
	if err := spdagvmSS.BuildGenesis(nil, &spdagvmArgs, &spdagvmReply); err != nil {
		return nil, fmt.Errorf("problem creating simple payments DAG: %w", err)
	}

	// Specify the genesis state of the simple payments chain
	spchainvmArgs := spchainvm.BuildGenesisArgs{}
	for _, addr := range config.ParsedFundedAddresses {
		spchainvmArgs.Accounts = append(spchainvmArgs.Accounts,
			spchainvm.APIAccount{
				Address: addr,
				Balance: json.Uint64(20 * units.KiloAva),
			},
		)
	}
	spchainvmReply := spchainvm.BuildGenesisReply{}

	spchainvmSS := spchainvm.StaticService{}
	if err := spchainvmSS.BuildGenesis(nil, &spchainvmArgs, &spchainvmReply); err != nil {
		return nil, fmt.Errorf("problem creating simple payments chain: %w", err)
	}

	// Specify the initial state of the Platform Chain
	platformvmArgs := platformvm.BuildGenesisArgs{
		NetworkID: json.Uint32(networkID),
	}
	for _, addr := range config.ParsedFundedAddresses {
		platformvmArgs.Accounts = append(platformvmArgs.Accounts,
			platformvm.APIAccount{
				Address: addr,
				Balance: json.Uint64(20 * units.KiloAva),
			},
		)
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
	stakingDuration := 365 * 24 * time.Hour // ~ 1 year
	endStakingTime := genesisTime.Add(stakingDuration)

	for i, validatorID := range config.ParsedStakerIDs {
		weight := json.Uint64(20 * units.KiloAva)
		platformvmArgs.Validators = append(platformvmArgs.Validators,
			platformvm.APIDefaultSubnetValidator{
				APIValidator: platformvm.APIValidator{
					StartTime: json.Uint64(genesisTime.Unix()),
					EndTime:   json.Uint64(endStakingTime.Unix()),
					Weight:    &weight,
					ID:        validatorID,
				},
				Destination: config.ParsedFundedAddresses[i%len(config.ParsedFundedAddresses)],
			},
		)
	}

	// Specify the chains that exist upon this network's creation
	platformvmArgs.Chains = []platformvm.APIChain{
		platformvm.APIChain{
			GenesisData: avmReply.Bytes,
			SubnetID:    platformvm.DefaultSubnetID,
			VMID:        avm.ID,
			FxIDs: []ids.ID{
				secp256k1fx.ID,
				nftfx.ID,
				propertyfx.ID,
			},
			Name: "X-Chain",
		},
		platformvm.APIChain{
			GenesisData: evmReply,
			SubnetID:    platformvm.DefaultSubnetID,
			VMID:        evm.ID,
			Name:        "C-Chain",
		},
		platformvm.APIChain{
			GenesisData: spdagvmReply.Bytes,
			SubnetID:    platformvm.DefaultSubnetID,
			VMID:        spdagvm.ID,
			Name:        "Simple DAG Payments",
		},
		platformvm.APIChain{
			GenesisData: spchainvmReply.Bytes,
			SubnetID:    platformvm.DefaultSubnetID,
			VMID:        spchainvm.ID,
			Name:        "Simple Chain Payments",
		},
		platformvm.APIChain{
			GenesisData: formatting.CB58{Bytes: []byte{}}, // There is no genesis data
			SubnetID:    platformvm.DefaultSubnetID,
			VMID:        timestampvm.ID,
			Name:        "Simple Timestamp Server",
		},
	}

	platformvmArgs.Time = json.Uint64(genesisTime.Unix())
	platformvmReply := platformvm.BuildGenesisReply{}

	platformvmSS := platformvm.StaticService{}
	if err := platformvmSS.BuildGenesis(nil, &platformvmArgs, &platformvmReply); err != nil {
		return nil, fmt.Errorf("problem while building platform chain's genesis state: %w", err)
	}

	return platformvmReply.Bytes.Bytes, nil
}

// Genesis ...
func Genesis(networkID uint32) ([]byte, error) { return FromConfig(networkID, GetConfig(networkID)) }

// VMGenesis ...
func VMGenesis(networkID uint32, vmID ids.ID) (*platformvm.CreateChainTx, error) {
	genesisBytes, err := Genesis(networkID)
	if err != nil {
		return nil, err
	}
	genesis := platformvm.Genesis{}
	platformvm.Codec.Unmarshal(genesisBytes, &genesis)
	if err := genesis.Initialize(); err != nil {
		return nil, err
	}
	for _, chain := range genesis.Chains {
		if chain.VMID.Equals(vmID) {
			return chain, nil
		}
	}
	return nil, fmt.Errorf("couldn't find subnet with VM ID %s", vmID)
}

// AVAAssetID ...
func AVAAssetID(networkID uint32) (ids.ID, error) {
	createAVM, err := VMGenesis(networkID, avm.ID)
	if err != nil {
		return ids.ID{}, err
	}

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
	if err := c.Unmarshal(createAVM.GenesisData, &genesis); err != nil {
		return ids.ID{}, err
	}

	if len(genesis.Txs) == 0 {
		return ids.ID{}, errors.New("genesis creates no transactions")
	}
	genesisTx := genesis.Txs[0]

	tx := avm.Tx{UnsignedTx: &genesisTx.CreateAssetTx}
	txBytes, err := c.Marshal(&tx)
	if err != nil {
		return ids.ID{}, err
	}
	tx.Initialize(txBytes)

	return tx.ID(), nil
}
