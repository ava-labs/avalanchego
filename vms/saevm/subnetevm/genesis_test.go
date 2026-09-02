// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnetevm

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/log"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
	"github.com/ava-labs/avalanchego/vms/saevm/warp/warptest"

	subnetevmparams "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	ethparams "github.com/ava-labs/libevm/params"
)

func TestParseGenesisNetworkUpgradeOverrides(t *testing.T) {
	genesis := newTestGenesis(upgradetest.Etna, saetest.NewUNSAFEKeyChain(t, 1))
	genesisBytes, err := json.Marshal(genesis)
	require.NoError(t, err, "json.Marshal(genesis)")

	wantEtnaTimestamp := uint64(upgrade.InitiallyActiveTime.Unix() + 2) // #nosec G115 -- known positive test timestamp
	upgradeBytes, err := json.Marshal(extras.UpgradeConfig{
		NetworkUpgradeOverrides: &extras.NetworkUpgrades{
			EtnaTimestamp: utils.PointerTo(wantEtnaTimestamp),
		},
	})
	require.NoError(t, err, "json.Marshal(upgradeConfig)")

	got, err := parseGenesis(
		newSnowCtx(t, upgradetest.GetConfig(upgradetest.Etna)),
		genesisBytes,
		upgradeBytes,
	)
	require.NoError(t, err, "parseGenesis()")
	require.Equal(
		t,
		wantEtnaTimestamp,
		*subnetevmparams.GetExtra(got.Config).EtnaTimestamp,
		"parseGenesis() EtnaTimestamp",
	)
}

func TestReadLastAcceptedHash(t *testing.T) {
	genesisHash := common.HexToHash("0x1")
	acceptedHash := common.HexToHash("0x2")
	tests := []struct {
		name  string
		write bool
		want  common.Hash
	}{
		{name: "fresh_database", want: genesisHash},
		{name: "restarted_database", write: true, want: acceptedHash},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()
			if test.write {
				rawdb.WriteHeadFastBlockHash(db, acceptedHash)
			}
			require.Equal(t, test.want, readLastAcceptedHash(db, genesisHash), "readLastAcceptedHash()")
		})
	}
}

func TestRestartRejectsActivatedNetworkUpgradeChange(t *testing.T) {
	initialHelicon := uint64(upgrade.InitiallyActiveTime.Unix() + 2) // #nosec G115 -- known positive test timestamp
	upgradeBytes := func(timestamp uint64) []byte {
		return mustMarshalJSON(t, extras.UpgradeConfig{
			NetworkUpgradeOverrides: &extras.NetworkUpgrades{
				HeliconTimestamp: utils.PointerTo(timestamp),
			},
		})
	}

	sut := newSUT(
		t,
		withFork(upgradetest.Helicon),
		withNumAccounts(2),
		withNow(time.Unix(int64(initialHelicon+100), 0)), // #nosec G115 -- test timestamp is far below MaxInt64
		withUpgradeConfig(func([]common.Address) []byte {
			return upgradeBytes(initialHelicon)
		}),
	)
	sut.sendTransferTx(t, 0, 1, big.NewInt(1))
	sut.buildAcceptExecuteBlock(t)

	clockTime := sut.vm.clock.Time()
	require.NoError(t, sut.shutdownVM(), "VM.Shutdown()")
	sut.apiServer.Close()
	sut.shutdownVM = nil
	sut.apiServer = nil

	snowCtx := newSnowCtx(t, sut.upgrades)
	snowCtx.Log = logging.NoLog{}
	warptest.SetValidators(t, snowCtx, warptest.NewValidators(t, warptest.WithMinimum(2)))
	validatorState := snowCtx.ValidatorState.(*validatorstest.State)
	validatorState.GetCurrentValidatorSetF = func(context.Context, ids.ID) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
		return map[ids.ID]*validators.GetCurrentValidatorOutput{}, 0, nil
	}
	previousLog := log.Root()
	log.SetDefault(log.NewLogger(log.DiscardHandler()))
	t.Cleanup(func() {
		log.SetDefault(previousLog)
	})

	vm, _, shutdown, err := initVM(
		sut.ctx,
		snowCtx,
		sut.baseDB,
		&clockTime,
		sut.genesisBytes,
		upgradeBytes(initialHelicon+1),
		sut.configBytes,
	)
	if shutdown != nil {
		t.Cleanup(func() {
			require.NoError(t, shutdown(), "VM.Shutdown()")
		})
	}
	var compatErr *ethparams.ConfigCompatError
	require.ErrorAs(t, err, &compatErr, "initVM()")
	require.Nil(t, vm, "initVM()")
}
