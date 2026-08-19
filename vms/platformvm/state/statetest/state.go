// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statetest

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

var DefaultNodeID = ids.GenerateTestNodeID()

type Config struct {
	DB           database.Database
	Genesis      []byte
	Registerer   prometheus.Registerer
	Validators   validators.Manager
	Upgrades     upgrade.Config
	Config       config.Config
	Context      *snow.Context
	Metrics      metrics.Metrics
	RewardConfig reward.Config
}

func New(t testing.TB, c Config) *state.State {
	if c.DB == nil {
		c.DB = memdb.New()
	}
	if c.Context == nil {
		c.Context = &snow.Context{
			NetworkID: constants.UnitTestID,
			NodeID:    DefaultNodeID,
			Log:       logging.NoLog{},
		}
	}
	if len(c.Genesis) == 0 {
		c.Genesis = genesistest.NewBytes(t, genesistest.Config{
			NetworkID: c.Context.NetworkID,
		})
	}
	if c.Registerer == nil {
		c.Registerer = prometheus.NewRegistry()
	}
	if c.Validators == nil {
		c.Validators = validators.NewManager()
	}
	if c.Upgrades == (upgrade.Config{}) {
		c.Upgrades = upgradetest.GetConfig(upgradetest.Latest)
	}
	if c.Config == (config.Config{}) {
		c.Config = config.Default
	}
	if c.Metrics == nil {
		c.Metrics = metrics.Noop
	}
	if c.RewardConfig == (reward.Config{}) {
		c.RewardConfig = reward.Config{
			MaxConsumptionRate: .12 * reward.PercentDenominator,
			MinConsumptionRate: .1 * reward.PercentDenominator,
			MintingPeriod:      365 * 24 * time.Hour,
			SupplyCap:          720 * units.MegaAvax,
		}
	}

	s, err := state.New(
		c.DB,
		c.Genesis,
		c.Registerer,
		c.Validators,
		c.Upgrades,
		&c.Config,
		c.Context,
		c.Metrics,
		c.RewardConfig,
	)
	require.NoError(t, err)
	return s
}

func CurrentValidator(staker *state.Staker) state.CurrentValidator {
	validator, err := state.NewCurrentValidator(
		staker.TxID,
		stakingTx(staker),
		staker.StartTime,
		staker.EndTime,
		staker.Weight,
		staker.PotentialReward,
	)
	if err != nil {
		panic(err)
	}
	return validator
}

func CurrentDelegator(staker *state.Staker) state.CurrentDelegator {
	delegator, err := state.NewCurrentDelegator(
		staker.TxID,
		stakingTx(staker),
		staker.StartTime,
		staker.EndTime,
		staker.Weight,
		staker.PotentialReward,
	)
	if err != nil {
		panic(err)
	}
	return delegator
}

func PendingValidator(staker *state.Staker) state.PendingValidator {
	validator, err := state.NewPendingValidator(staker.TxID, stakingTx(staker))
	if err != nil {
		panic(err)
	}
	return validator
}

func PendingDelegator(staker *state.Staker) state.PendingDelegator {
	delegator, err := state.NewPendingDelegator(staker.TxID, stakingTx(staker))
	if err != nil {
		panic(err)
	}
	return delegator
}

func stakingTx(staker *state.Staker) platform.ScheduledStaker {
	validator := platform.Validator{
		NodeID: staker.NodeID,
		Start:  uint64(staker.StartTime.Unix()),
		End:    uint64(staker.EndTime.Unix()),
		Wght:   staker.Weight,
	}

	switch staker.Priority {
	case 0:
		var txSigner signer.Signer = &signer.Empty{}
		if staker.PublicKey != nil {
			txSigner = testSigner{key: staker.PublicKey}
		}
		return &platform.AddPermissionlessValidatorTx{
			Validator: validator,
			Subnet:    staker.SubnetID,
			Signer:    txSigner,
		}
	case platform.PrimaryNetworkDelegatorApricotPendingPriority:
		return &platform.AddDelegatorTx{Validator: validator}
	case platform.PrimaryNetworkDelegatorBanffPendingPriority,
		platform.PrimaryNetworkDelegatorCurrentPriority,
		platform.SubnetPermissionlessDelegatorPendingPriority,
		platform.SubnetPermissionlessDelegatorCurrentPriority:
		return &platform.AddPermissionlessDelegatorTx{
			Validator: validator,
			Subnet:    staker.SubnetID,
		}
	case platform.PrimaryNetworkValidatorPendingPriority,
		platform.PrimaryNetworkValidatorCurrentPriority,
		platform.SubnetPermissionlessValidatorPendingPriority,
		platform.SubnetPermissionlessValidatorCurrentPriority:
		var txSigner signer.Signer = &signer.Empty{}
		if staker.PublicKey != nil {
			txSigner = testSigner{key: staker.PublicKey}
		}
		return &platform.AddPermissionlessValidatorTx{
			Validator: validator,
			Subnet:    staker.SubnetID,
			Signer:    txSigner,
		}
	case platform.SubnetPermissionedValidatorPendingPriority,
		platform.SubnetPermissionedValidatorCurrentPriority:
		return &platform.AddSubnetValidatorTx{
			SubnetValidator: platform.SubnetValidator{
				Validator: validator,
				Subnet:    staker.SubnetID,
			},
		}
	default:
		panic("invalid staker priority")
	}
}

type testSigner struct {
	key *bls.PublicKey
}

func (testSigner) Verify() error {
	return nil
}

func (s testSigner) Key() *bls.PublicKey {
	return s.key
}
