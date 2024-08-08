// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	safemath "github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	as "github.com/ava-labs/avalanchego/vms/platformvm/addrstate"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/caminoconfig"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

const (
	maxStakingDuration = 365 * 24 * time.Hour

	MinStakingDuration = 24 * time.Hour
	ValidatorWeight    = 2 * units.KiloAvax
	PreFundedBalance   = 100 * ValidatorWeight
	TxFee              = uint64(100)
)

var (
	avaxAssetID  = ids.ID{'C', 'A', 'M'}
	cChainID     = ids.ID{'C', '-', 'C', 'H', 'A', 'I', 'N'}
	xChainID     = ids.ID{'X', '-', 'C', 'H', 'A', 'I', 'N'}
	rewardConfig = reward.Config{
		MaxConsumptionRate: .12 * reward.PercentDenominator,
		MinConsumptionRate: .10 * reward.PercentDenominator,
		MintingPeriod:      365 * 24 * time.Hour,
		SupplyCap:          720 * units.MegaAvax,
	}
	GenesisTime             = time.Date(1997, 1, 1, 0, 0, 0, 0, time.UTC)
	LatestPhaseTime         = GenesisTime.Add(time.Second * 10000)
	ValidatorStartTime      = GenesisTime
	ValidatorEndTime        = GenesisTime.Add(10 * MinStakingDuration)
	GenesisTimestamp        = uint64(GenesisTime.Unix())
	ValidatorStartTimestamp = uint64(ValidatorStartTime.Unix())
	ValidatorEndTimestamp   = uint64(ValidatorEndTime.Unix())
)

func Config(t *testing.T, phase Phase) *config.Config {
	t.Helper()

	var (
		apricotPhase3Time = mockable.MaxTime
		apricotPhase5Time = mockable.MaxTime
		banffTime         = mockable.MaxTime
		athensTime        = mockable.MaxTime
		cortinaTime       = mockable.MaxTime
		berlinTime        = mockable.MaxTime
		cairoTime         = mockable.MaxTime
	)

	// always reset LatestForkTime (a package level variable)
	// to ensure test independence
	switch phase {
	case PhaseCairo:
		cairoTime = LatestPhaseTime
		fallthrough
	case PhaseBerlin:
		berlinTime = LatestPhaseTime
		cortinaTime = LatestPhaseTime
		fallthrough
	case PhaseAthens:
		athensTime = LatestPhaseTime
		fallthrough
	case PhaseSunrise:
		banffTime = LatestPhaseTime
		fallthrough
	case PhaseApricot5:
		apricotPhase5Time = LatestPhaseTime
		apricotPhase3Time = LatestPhaseTime
	default:
		require.FailNowf(t, "", "unknown phase %d (%s)", phase)
	}

	vdrs := validators.NewManager()
	primaryVdrs := validators.NewSet()
	_ = vdrs.Add(constants.PrimaryNetworkID, primaryVdrs)
	return &config.Config{
		Chains:                 chains.TestManager,
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		StakingEnabled:         true,
		Validators:             vdrs,
		TxFee:                  TxFee,
		CreateSubnetTxFee:      100 * TxFee,
		TransformSubnetTxFee:   100 * TxFee,
		CreateBlockchainTxFee:  100 * TxFee,
		MinValidatorStake:      ValidatorWeight,
		MaxValidatorStake:      ValidatorWeight,
		MinDelegatorStake:      1 * units.MilliAvax,
		MinStakeDuration:       MinStakingDuration,
		MaxStakeDuration:       maxStakingDuration,
		RewardConfig:           rewardConfig,
		ApricotPhase3Time:      apricotPhase3Time,
		ApricotPhase5Time:      apricotPhase5Time,
		BanffTime:              banffTime,
		AthensPhaseTime:        athensTime,
		CortinaTime:            cortinaTime,
		BerlinPhaseTime:        berlinTime,
		CairoPhaseTime:         cairoTime,
		CaminoConfig: caminoconfig.Config{
			DACProposalBondAmount: 100 * units.Avax,
		},
	}
}

func Genesis(t *testing.T, avaxAssetID ids.ID, caminoGenesisConfig api.Camino, additionalUTXOs []api.UTXO) []byte {
	t.Helper()
	require.Len(t, FundedKeys, len(FundedNodeIDs))

	supply := uint64(0)
	var err error

	utxos := make([]api.UTXO, len(FundedKeys))
	for i := range FundedKeys {
		utxos[i] = api.UTXO{
			Amount:  json.Uint64(PreFundedBalance),
			Address: FundedKeysBech32[i],
		}
		supply, err = safemath.Add64(supply, PreFundedBalance)
		require.NoError(t, err)
	}
	utxos = append(utxos, additionalUTXOs...)
	caminoGenesisConfig.UTXODeposits = make([]api.UTXODeposit, len(utxos))
	caminoGenesisConfig.ValidatorDeposits = make([][]api.UTXODeposit, len(FundedKeys))
	caminoGenesisConfig.ValidatorConsortiumMembers = make([]ids.ShortID, len(FundedKeys))

	genesisValidators := make([]api.PermissionlessValidator, len(FundedKeys))
	for i, key := range FundedKeys {
		genesisValidators[i] = api.PermissionlessValidator{
			Staker: api.Staker{
				StartTime: json.Uint64(ValidatorStartTime.Unix()),
				EndTime:   json.Uint64(ValidatorEndTime.Unix()),
				NodeID:    FundedNodeIDs[i],
			},
			RewardOwner: &api.Owner{
				Threshold: 1,
				Addresses: []string{FundedKeysBech32[i]},
			},
			Staked: []api.UTXO{{
				Amount:  json.Uint64(ValidatorWeight),
				Address: FundedKeysBech32[i],
			}},
			DelegationFee: reward.PercentDenominator,
		}
		supply, err = safemath.Add64(supply, ValidatorWeight)
		require.NoError(t, err)
		caminoGenesisConfig.ValidatorDeposits[i] = make([]api.UTXODeposit, 1)
		caminoGenesisConfig.ValidatorConsortiumMembers[i] = key.Address()
		caminoGenesisConfig.AddressStates = append(caminoGenesisConfig.AddressStates, genesis.AddressState{
			Address: key.Address(),
			State:   as.AddressStateConsortium,
		})
	}

	buildGenesisArgs := api.BuildGenesisArgs{
		NetworkID:     json.Uint32(constants.UnitTestID),
		AvaxAssetID:   avaxAssetID,
		UTXOs:         utxos,
		Validators:    genesisValidators,
		Camino:        &caminoGenesisConfig,
		Chains:        nil,
		Time:          json.Uint64(GenesisTime.Unix()),
		InitialSupply: json.Uint64(supply),
		Encoding:      formatting.Hex,
	}

	buildGenesisResponse := api.BuildGenesisReply{}
	platformvmSS := api.StaticService{}
	require.NoError(t, platformvmSS.BuildGenesis(nil, &buildGenesisArgs, &buildGenesisResponse))

	genesisBytes, err := formatting.Decode(buildGenesisResponse.Encoding, buildGenesisResponse.Bytes)
	require.NoError(t, err)

	return genesisBytes
}

func Context(t *testing.T) *snow.Context {
	t.Helper()

	aliaser := ids.NewAliaser()
	require.NoError(t, aliaser.Alias(constants.PlatformChainID, "P"))

	ctx := snow.DefaultContextTest()
	ctx.AVAXAssetID = avaxAssetID
	ctx.ChainID = constants.PlatformChainID
	ctx.XChainID = xChainID
	ctx.CChainID = cChainID
	ctx.BCLookup = aliaser
	ctx.NetworkID = constants.UnitTestID
	ctx.SubnetID = constants.PrimaryNetworkID
	ctx.ValidatorState = &validators.TestState{
		GetSubnetIDF: func(_ context.Context, chainID ids.ID) (ids.ID, error) {
			subnetID, ok := map[ids.ID]ids.ID{
				constants.PlatformChainID: ctx.SubnetID,
				ctx.XChainID:              ctx.SubnetID,
				ctx.CChainID:              ctx.SubnetID,
			}[chainID]
			if !ok {
				return ids.Empty, errors.New("missing")
			}
			return subnetID, nil
		},
	}
	return ctx
}

func ContextWithSharedMemory(t *testing.T, db database.Database) *snow.Context {
	t.Helper()
	ctx := Context(t)
	m := atomic.NewMemory(db)
	ctx.SharedMemory = m.NewSharedMemory(ctx.ChainID)
	return ctx
}

func Clock() *mockable.Clock {
	return ClockWithTime(GenesisTime)
}

func ClockWithTime(time time.Time) *mockable.Clock {
	clk := mockable.Clock{}
	clk.Set(time)
	return &clk
}

type fxVMInt struct {
	registry codec.Registry
	clk      *mockable.Clock
	log      logging.Logger
}

func (fvi *fxVMInt) CodecRegistry() codec.Registry {
	return fvi.registry
}

func (fvi *fxVMInt) Clock() *mockable.Clock {
	return fvi.clk
}

func (fvi *fxVMInt) Logger() logging.Logger {
	return fvi.log
}

func Fx(t *testing.T, clk *mockable.Clock, log logging.Logger, isBootstrapped bool) fx.Fx {
	t.Helper()

	fxVMInt := &fxVMInt{
		registry: linearcodec.NewDefault(),
		clk:      clk,
		log:      log,
	}
	fx := &secp256k1fx.Fx{}
	require.NoError(t, fx.Initialize(fxVMInt))
	if isBootstrapped {
		require.NoError(t, fx.Bootstrapped())
	}
	return fx
}
