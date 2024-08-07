// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/keystore"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/test"
)

func newCaminoService(t *testing.T, camino api.Camino, phase test.Phase, utxos []api.UTXO) *CaminoService { //nolint:unparam
	vm := newCaminoVM(t, camino, phase, utxos)

	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()
	ks := keystore.New(logging.NoLog{}, manager.NewMemDB(version.Semantic1_0_0))
	require.NoError(t, ks.CreateUser(testUsername, testPassword))
	vm.ctx.Keystore = ks.NewBlockchainKeyStore(vm.ctx.ChainID)
	return &CaminoService{
		Service: Service{
			vm:          vm,
			addrManager: avax.NewAddressManager(vm.ctx),
		},
	}
}

func newCaminoVM(t *testing.T, genesisConfig api.Camino, phase test.Phase, genesisUTXOs []api.UTXO) *VM {
	require := require.New(t)

	vm := &VM{Config: *test.Config(t, phase)}

	baseDBManager := manager.NewMemDB(version.Semantic1_0_0)
	chainDBManager := baseDBManager.NewPrefixDBManager([]byte{0})
	atomicDB := prefixdb.New([]byte{1}, baseDBManager.Current().Database)

	vm.clock.Set(test.PhaseTime(t, phase, &vm.Config))
	msgChan := make(chan common.Message, 1)
	ctx := test.ContextWithSharedMemory(t, atomicDB)

	// utxo with funds for testSubnet1 (see below)
	genesisUTXOs = append(genesisUTXOs, api.UTXO{
		Amount:  json.Uint64(vm.Config.CreateSubnetTxFee),
		Address: test.FundedKeysBech32[0],
	})

	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()
	genesisBytes := test.Genesis(t, ctx.AVAXAssetID, genesisConfig, genesisUTXOs)
	appSender := &common.SenderTest{}
	appSender.CantSendAppGossip = true
	appSender.SendAppGossipF = func(context.Context, []byte) error {
		return nil
	}

	require.NoError(vm.Initialize(
		context.Background(),
		ctx,
		chainDBManager,
		genesisBytes,
		nil,
		nil,
		msgChan,
		nil,
		appSender,
	))

	// align chain time and local clock
	vm.state.SetTimestamp(vm.clock.Time())

	require.NoError(vm.SetState(context.Background(), snow.NormalOp))

	// Create a subnet and store it in testSubnet1
	// Note: following Banff activation, block acceptance will move
	// chain time ahead
	var err error
	testSubnet1, err = vm.txBuilder.NewCreateSubnetTx(
		2, // threshold; 2 sigs from keys[0], keys[1], keys[2] needed to add validator to this subnet
		[]ids.ShortID{ // control keys
			test.FundedKeys[0].Address(),
			test.FundedKeys[1].Address(),
			test.FundedKeys[2].Address(),
		},
		[]*secp256k1.PrivateKey{test.FundedKeys[0]},
		test.FundedKeys[0].Address(),
	)

	require.NoError(err)
	require.NoError(vm.Builder.AddUnverifiedTx(testSubnet1))
	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(blk.Verify(context.Background()))
	require.NoError(blk.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	t.Cleanup(func() {
		vm.ctx.Lock.Lock()
		defer vm.ctx.Lock.Unlock()
		require.NoError(vm.Shutdown(context.Background()))
	})

	return vm
}
