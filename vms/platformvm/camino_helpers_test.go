// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/nodeid"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/version"
	as "github.com/ava-labs/avalanchego/vms/platformvm/addrstate"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/caminoconfig"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/stretchr/testify/require"
)

const (
	defaultCaminoValidatorWeight = 2 * units.KiloAvax
)

var (
	localStakingPath          = "../../staking/local/"
	caminoPreFundedKeys       = secp256k1.TestKeys()
	_, caminoPreFundedNodeIDs = nodeid.LoadLocalCaminoNodeKeysAndIDs(localStakingPath)
)

func newCaminoVM(genesisConfig api.Camino, genesisUTXOs []api.UTXO, startTime *time.Time) *VM {
	vm := &VM{Config: defaultCaminoConfig(true)}

	baseDBManager := manager.NewMemDB(version.Semantic1_0_0)
	chainDBManager := baseDBManager.NewPrefixDBManager([]byte{0})
	atomicDB := prefixdb.New([]byte{1}, baseDBManager.Current().Database)

	if startTime == nil {
		defaultStartTime := banffForkTime.Add(time.Second)
		startTime = &defaultStartTime
	}
	vm.clock.Set(*startTime)
	msgChan := make(chan common.Message, 1)
	ctx := defaultContext()

	m := atomic.NewMemory(atomicDB)
	msm := &mutableSharedMemory{
		SharedMemory: m.NewSharedMemory(ctx.ChainID),
	}
	ctx.SharedMemory = msm

	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()
	_, genesisBytes := newCaminoGenesisWithUTXOs(genesisConfig, genesisUTXOs, startTime)
	appSender := &common.SenderTest{}
	appSender.CantSendAppGossip = true
	appSender.SendAppGossipF = func(context.Context, []byte) error {
		return nil
	}

	if err := vm.Initialize(context.TODO(), ctx, chainDBManager, genesisBytes, nil, nil, msgChan, nil, appSender); err != nil {
		panic(err)
	}
	if err := vm.SetState(context.TODO(), snow.NormalOp); err != nil {
		panic(err)
	}

	// Create a subnet and store it in testSubnet1
	// Note: following Banff activation, block acceptance will move
	// chain time ahead
	var err error
	testSubnet1, err = vm.txBuilder.NewCreateSubnetTx(
		2, // threshold; 2 sigs from keys[0], keys[1], keys[2] needed to add validator to this subnet
		[]ids.ShortID{ // control keys
			caminoPreFundedKeys[0].PublicKey().Address(),
			caminoPreFundedKeys[1].PublicKey().Address(),
			caminoPreFundedKeys[2].PublicKey().Address(),
		},
		[]*secp256k1.PrivateKey{caminoPreFundedKeys[0]},
		caminoPreFundedKeys[0].PublicKey().Address(),
	)
	if err != nil {
		panic(err)
	} else if err := vm.Builder.AddUnverifiedTx(testSubnet1); err != nil {
		panic(err)
	} else if blk, err := vm.Builder.BuildBlock(context.TODO()); err != nil {
		panic(err)
	} else if err := blk.Verify(context.TODO()); err != nil {
		panic(err)
	} else if err := blk.Accept(context.TODO()); err != nil {
		panic(err)
	} else if err := vm.SetPreference(context.TODO(), vm.manager.LastAccepted()); err != nil {
		panic(err)
	}

	return vm
}

func defaultCaminoConfig(postBanff bool) config.Config { //nolint:unparam
	banffTime := mockable.MaxTime
	if postBanff {
		banffTime = defaultValidateEndTime.Add(-2 * time.Second)
	}

	vdrs := validators.NewManager()
	primaryVdrs := validators.NewSet()
	_ = vdrs.Add(constants.PrimaryNetworkID, primaryVdrs)
	return config.Config{
		Chains:                 chains.TestManager,
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		Validators:             vdrs,
		TxFee:                  defaultTxFee,
		CreateSubnetTxFee:      100 * defaultTxFee,
		CreateBlockchainTxFee:  100 * defaultTxFee,
		MinValidatorStake:      defaultCaminoValidatorWeight,
		MaxValidatorStake:      defaultCaminoValidatorWeight,
		MinDelegatorStake:      1 * units.MilliAvax,
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig: reward.Config{
			MaxConsumptionRate: .12 * reward.PercentDenominator,
			MinConsumptionRate: .10 * reward.PercentDenominator,
			MintingPeriod:      365 * 24 * time.Hour,
			SupplyCap:          720 * units.MegaAvax,
		},
		ApricotPhase3Time: defaultValidateEndTime,
		ApricotPhase5Time: defaultValidateEndTime,
		BanffTime:         banffTime,
		CaminoConfig: caminoconfig.Config{
			DACProposalBondAmount: 100 * units.Avax,
		},
	}
}

// Returns:
// 1) The genesis state
// 2) The byte representation of the default genesis for tests
func newCaminoGenesisWithUTXOs(caminoGenesisConfig api.Camino, genesisUTXOs []api.UTXO, starttime *time.Time) (*api.BuildGenesisArgs, []byte) {
	hrp := constants.NetworkIDToHRP[testNetworkID]

	if starttime == nil {
		starttime = &defaultValidateStartTime
	}
	caminoGenesisConfig.UTXODeposits = make([]api.UTXODeposit, len(genesisUTXOs))
	caminoGenesisConfig.ValidatorDeposits = make([][]api.UTXODeposit, len(caminoPreFundedKeys))
	caminoGenesisConfig.ValidatorConsortiumMembers = make([]ids.ShortID, len(caminoPreFundedKeys))

	genesisValidators := make([]api.PermissionlessValidator, len(caminoPreFundedKeys))
	for i, key := range caminoPreFundedKeys {
		addr, err := address.FormatBech32(hrp, key.PublicKey().Address().Bytes())
		if err != nil {
			panic(err)
		}
		genesisValidators[i] = api.PermissionlessValidator{
			Staker: api.Staker{
				StartTime: json.Uint64(starttime.Unix()),
				EndTime:   json.Uint64(starttime.Add(10 * defaultMinStakingDuration).Unix()),
				NodeID:    caminoPreFundedNodeIDs[i],
			},
			RewardOwner: &api.Owner{
				Threshold: 1,
				Addresses: []string{addr},
			},
			Staked: []api.UTXO{{
				Amount:  json.Uint64(defaultWeight),
				Address: addr,
			}},
		}
		caminoGenesisConfig.ValidatorDeposits[i] = make([]api.UTXODeposit, 1)
		caminoGenesisConfig.ValidatorConsortiumMembers[i] = key.Address()
		caminoGenesisConfig.AddressStates = append(caminoGenesisConfig.AddressStates, genesis.AddressState{
			Address: key.Address(),
			State:   as.AddressStateConsortiumMember,
		})
	}

	buildGenesisArgs := api.BuildGenesisArgs{
		Encoding:      formatting.Hex,
		NetworkID:     json.Uint32(testNetworkID),
		AvaxAssetID:   avaxAssetID,
		UTXOs:         genesisUTXOs,
		Validators:    genesisValidators,
		Chains:        nil,
		Time:          json.Uint64(defaultGenesisTime.Unix()),
		InitialSupply: json.Uint64(360 * units.MegaAvax),
		Camino:        caminoGenesisConfig,
	}

	buildGenesisResponse := api.BuildGenesisReply{}
	platformvmSS := api.StaticService{}
	if err := platformvmSS.BuildGenesis(nil, &buildGenesisArgs, &buildGenesisResponse); err != nil {
		panic(fmt.Errorf("problem while building platform chain's genesis state: %w", err))
	}

	genesisBytes, err := formatting.Decode(buildGenesisResponse.Encoding, buildGenesisResponse.Bytes)
	if err != nil {
		panic(err)
	}

	return &buildGenesisArgs, genesisBytes
}

func generateKeyAndOwner(t *testing.T) (*secp256k1.PrivateKey, ids.ShortID, secp256k1fx.OutputOwners) {
	t.Helper()
	key, err := testKeyFactory.NewPrivateKey()
	require.NoError(t, err)
	addr := key.Address()
	return key, addr, secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{addr},
	}
}
