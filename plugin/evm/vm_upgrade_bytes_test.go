// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/snow"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/metrics"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/precompile/contracts/txallowlist"
	"github.com/ava-labs/subnet-evm/utils"
	"github.com/ava-labs/subnet-evm/vmerrs"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVMUpgradeBytesPrecompile(t *testing.T) {
	// Make a TxAllowListConfig upgrade at genesis and convert it to JSON to apply as upgradeBytes.
	enableAllowListTimestamp := time.Unix(0, 0) // enable at genesis
	upgradeConfig := &params.UpgradeConfig{
		PrecompileUpgrades: []params.PrecompileUpgrade{
			{
				Config: txallowlist.NewConfig(utils.TimeToNewUint64(enableAllowListTimestamp), testEthAddrs[0:1], nil, nil),
			},
		},
	}
	upgradeBytesJSON, err := json.Marshal(upgradeConfig)
	if err != nil {
		t.Fatalf("could not marshal upgradeConfig to json: %s", err)
	}

	// initialize the VM with these upgrade bytes
	issuer, vm, dbManager, appSender := GenesisVM(t, true, genesisJSONSubnetEVM, "", string(upgradeBytesJSON))

	// Submit a successful transaction
	tx0 := types.NewTransaction(uint64(0), testEthAddrs[0], big.NewInt(1), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx0, err := types.SignTx(tx0, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0])
	assert.NoError(t, err)

	errs := vm.txPool.AddRemotesSync([]*types.Transaction{signedTx0})
	if err := errs[0]; err != nil {
		t.Fatalf("Failed to add tx at index: %s", err)
	}

	// Submit a rejected transaction, should throw an error
	tx1 := types.NewTransaction(uint64(0), testEthAddrs[1], big.NewInt(2), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx1, err := types.SignTx(tx1, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[1])
	if err != nil {
		t.Fatal(err)
	}
	errs = vm.txPool.AddRemotesSync([]*types.Transaction{signedTx1})
	if err := errs[0]; !errors.Is(err, vmerrs.ErrSenderAddressNotAllowListed) {
		t.Fatalf("expected ErrSenderAddressNotAllowListed, got: %s", err)
	}

	// shutdown the vm
	if err := vm.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	// prepare the new upgrade bytes to disable the TxAllowList
	disableAllowListTimestamp := enableAllowListTimestamp.Add(10 * time.Hour) // arbitrary choice
	upgradeConfig.PrecompileUpgrades = append(
		upgradeConfig.PrecompileUpgrades,
		params.PrecompileUpgrade{
			Config: txallowlist.NewDisableConfig(utils.TimeToNewUint64(disableAllowListTimestamp)),
		},
	)
	upgradeBytesJSON, err = json.Marshal(upgradeConfig)
	if err != nil {
		t.Fatalf("could not marshal upgradeConfig to json: %s", err)
	}

	// restart the vm
	// Hack: registering metrics uses global variables, so we need to disable metrics here so that we
	// can initialize the VM twice.
	metrics.Enabled = false
	defer func() {
		metrics.Enabled = true
	}()
	if err := vm.Initialize(
		context.Background(), vm.ctx, dbManager, []byte(genesisJSONSubnetEVM), upgradeBytesJSON, []byte{}, issuer, []*commonEng.Fx{}, appSender,
	); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()
	// Set the VM's state to NormalOp to initialize the tx pool.
	if err := vm.SetState(context.Background(), snow.NormalOp); err != nil {
		t.Fatal(err)
	}
	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.txPool.SubscribeNewReorgEvent(newTxPoolHeadChan)
	vm.clock.Set(disableAllowListTimestamp)

	// Make a block, previous rules still apply (TxAllowList is active)
	// Submit a successful transaction
	errs = vm.txPool.AddRemotesSync([]*types.Transaction{signedTx0})
	if err := errs[0]; err != nil {
		t.Fatalf("Failed to add tx at index: %s", err)
	}

	// Submit a rejected transaction, should throw an error
	errs = vm.txPool.AddRemotesSync([]*types.Transaction{signedTx1})
	if err := errs[0]; !errors.Is(err, vmerrs.ErrSenderAddressNotAllowListed) {
		t.Fatalf("expected ErrSenderAddressNotAllowListed, got: %s", err)
	}

	blk := issueAndAccept(t, issuer, vm)

	// Verify that the constructed block only has the whitelisted tx
	block := blk.(*chain.BlockWrapper).Block.(*Block).ethBlock
	txs := block.Transactions()
	if txs.Len() != 1 {
		t.Fatalf("Expected number of txs to be %d, but found %d", 1, txs.Len())
	}
	assert.Equal(t, signedTx0.Hash(), txs[0].Hash())

	// verify the issued block is after the network upgrade
	assert.GreaterOrEqual(t, int64(block.Timestamp()), disableAllowListTimestamp.Unix())

	<-newTxPoolHeadChan // wait for new head in tx pool

	// retry the rejected Tx, which should now succeed
	errs = vm.txPool.AddRemotesSync([]*types.Transaction{signedTx1})
	if err := errs[0]; err != nil {
		t.Fatalf("Failed to add tx at index: %s", err)
	}

	vm.clock.Set(vm.clock.Time().Add(2 * time.Second)) // add 2 seconds for gas fee to adjust
	blk = issueAndAccept(t, issuer, vm)

	// Verify that the constructed block only has the previously rejected tx
	block = blk.(*chain.BlockWrapper).Block.(*Block).ethBlock
	txs = block.Transactions()
	if txs.Len() != 1 {
		t.Fatalf("Expected number of txs to be %d, but found %d", 1, txs.Len())
	}
	assert.Equal(t, signedTx1.Hash(), txs[0].Hash())
}

// func TestVMUpgradeBytesOptionalNetworkUpgrades(t *testing.T) {
// 	tests := []struct {
// 		name           string
// 		setTimestampFn func(upgrade *params.UpgradeConfig, timestamp *big.Int)
// 		checkUpgradeFn func(config *params.ChainConfig, blockTimestamp *big.Int) bool
// 	}{
// 		{
// 			name: "Test",
// 			setTimestampFn: func(upgrade *params.UpgradeConfig, timestamp *big.Int) {
// 				upgrade.OptionalNetworkUpgrades.TestTimestamp = timestamp
// 			},
// 			checkUpgradeFn: func(config *params.ChainConfig, blockTimestamp *big.Int) bool {
// 				return config.IsTest(blockTimestamp)
// 			},
// 		},
// 	}
// 	// Hack: registering metrics uses global variables, so we need to disable metrics here so that we can initialize the VM twice.
// 	metrics.Enabled = false
// 	defer func() {
// 		metrics.Enabled = true
// 	}()
// 	for _, test := range tests {
// 		t.Run(test.name, func(t *testing.T) {
// 			// Get a json specifying a Network upgrade at genesis
// 			// to apply as upgradeBytes.
// 			testTimestamp := time.Unix(10, 0)
// 			upgradeConfig := &params.UpgradeConfig{
// 				OptionalNetworkUpgrades: &params.OptionalNetworkUpgrades{},
// 			}
// 			test.setTimestampFn(upgradeConfig, big.NewInt(testTimestamp.Unix()))
// 			upgradeBytesJSON, err := json.Marshal(upgradeConfig)
// 			require.NoError(t, err)

// 			// initialize the VM with these upgrade bytes
// 			issuer, vm, dbManager, appSender := GenesisVM(t, true, genesisJSONPreSubnetEVM, "", string(upgradeBytesJSON))
// 			vm.clock.Set(testTimestamp)

// 			// verify upgrade is applied
// 			require.True(t, test.checkUpgradeFn(vm.chainConfig, big.NewInt(testTimestamp.Unix())))

// 			// Submit a successful transaction and build a block to move the chain head past the SubnetEVMTimestamp network upgrade
// 			tx0 := types.NewTransaction(uint64(0), testEthAddrs[0], big.NewInt(1), 21000, big.NewInt(testMinGasPrice), nil)
// 			signedTx0, err := types.SignTx(tx0, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0])
// 			require.NoError(t, err)
// 			errs := vm.txPool.AddRemotesSync([]*types.Transaction{signedTx0})
// 			require.NoError(t, errs[0])

// 			issueAndAccept(t, issuer, vm) // make a block

// 			require.NoError(t, vm.Shutdown(context.Background()))
// 			// VM should not start again without proper upgrade bytes.
// 			err = vm.Initialize(context.Background(), vm.ctx, dbManager, []byte(genesisJSONPreSubnetEVM), []byte{}, []byte{}, issuer, []*commonEng.Fx{}, appSender)
// 			require.ErrorContains(t, err, fmt.Sprintf("mismatching %s fork block timestamp in database", test.name))

// 			// VM should not start if fork is moved back
// 			test.setTimestampFn(upgradeConfig, big.NewInt(2))
// 			upgradeBytesJSON, err = json.Marshal(upgradeConfig)
// 			require.NoError(t, err)
// 			err = vm.Initialize(context.Background(), vm.ctx, dbManager, []byte(genesisJSONPreSubnetEVM), upgradeBytesJSON, []byte{}, issuer, []*commonEng.Fx{}, appSender)
// 			require.ErrorContains(t, err, fmt.Sprintf("mismatching %s fork block timestamp in database", test.name))

// 			// VM should not start if fork is moved forward
// 			test.setTimestampFn(upgradeConfig, big.NewInt(30))
// 			upgradeBytesJSON, err = json.Marshal(upgradeConfig)
// 			require.NoError(t, err)
// 			err = vm.Initialize(context.Background(), vm.ctx, dbManager, []byte(genesisJSONPreSubnetEVM), upgradeBytesJSON, []byte{}, issuer, []*commonEng.Fx{}, appSender)
// 			require.ErrorContains(t, err, fmt.Sprintf("mismatching %s fork block timestamp in database", test.name))
// 		})
// 	}
// }

func TestMandatoryUpgradesEnforced(t *testing.T) {
	// make genesis w/ fork at block 5
	// but this should not be used because we are enforcing
	// network upgrades within the code
	var genesis core.Genesis
	if err := json.Unmarshal([]byte(genesisJSONPreSubnetEVM), &genesis); err != nil {
		t.Fatalf("could not unmarshal genesis bytes: %s", err)
	}
	genesisSubnetEVMTimestamp := utils.NewUint64(5)
	genesis.Config.SubnetEVMTimestamp = genesisSubnetEVMTimestamp
	genesisBytes, err := json.Marshal(&genesis)
	if err != nil {
		t.Fatalf("could not unmarshal genesis bytes: %s", err)
	}

	// initialize the VM with these upgrade bytes
	tests := []struct {
		networkID uint32
		expected  bool
	}{
		{
			networkID: constants.MainnetID,
			expected:  true,
		},
		{
			networkID: constants.FujiID,
			expected:  true,
		},
		{
			networkID: constants.LocalID,
			expected:  false,
		},
		{
			networkID: constants.UnitTestID,
			expected:  false,
		},
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("networkID %d", test.networkID), func(t *testing.T) {
			vm := &VM{}
			ctx, dbManager, genesisBytes, issuer, _ := setupGenesis(t, string(genesisBytes))
			ctx.NetworkID = test.networkID
			appSender := &commonEng.SenderTest{T: t}
			appSender.CantSendAppGossip = true
			appSender.SendAppGossipF = func(context.Context, []byte, int, int, int) error { return nil }
			err := vm.Initialize(
				context.Background(),
				ctx,
				dbManager,
				genesisBytes,
				nil,
				nil,
				issuer,
				[]*commonEng.Fx{},
				appSender,
			)
			require.NoError(t, err, "error initializing GenesisVM")

			require.NoError(t, vm.SetState(context.Background(), snow.Bootstrapping))
			require.NoError(t, vm.SetState(context.Background(), snow.NormalOp))

			defer func() {
				if err := vm.Shutdown(context.Background()); err != nil {
					t.Fatal(err)
				}
			}()

			// verify upgrade is rescheduled
			require.Equal(t, test.expected, vm.chainConfig.IsSubnetEVM(0))
		})
	}
}

func mustMarshal(t *testing.T, v interface{}) string {
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

func TestVMStateUpgrade(t *testing.T) {
	// modify genesis to add a key to the state
	genesis := &core.Genesis{}
	err := json.Unmarshal([]byte(genesisJSONSubnetEVM), genesis)
	require.NoError(t, err)
	genesisAccount, ok := genesis.Alloc[testEthAddrs[0]]
	require.True(t, ok)
	storageKey := common.HexToHash("0x1234")
	genesisAccount.Storage = map[common.Hash]common.Hash{storageKey: common.HexToHash("0x5555")}
	genesisCode, err := hexutil.Decode("0xabcd")
	require.NoError(t, err)
	genesisAccount.Code = genesisCode
	genesisAccount.Nonce = 2                        // set to a non-zero value to test that it is preserved
	genesis.Alloc[testEthAddrs[0]] = genesisAccount // have to assign this back to the map for changes to take effect.
	genesisStr := mustMarshal(t, genesis)

	upgradedCodeStr := "0xdeadbeef" // this code will be applied during the upgrade
	upgradedCode, err := hexutil.Decode(upgradedCodeStr)
	// This modification will be applied to an existing account
	genesisAccountUpgrade := &params.StateUpgradeAccount{
		BalanceChange: (*math.HexOrDecimal256)(big.NewInt(100)),
		Storage:       map[common.Hash]common.Hash{storageKey: {}},
		Code:          upgradedCode,
	}

	// This modification will be applied to a new account
	newAccount := common.Address{42}
	require.NoError(t, err)
	newAccountUpgrade := &params.StateUpgradeAccount{
		BalanceChange: (*math.HexOrDecimal256)(big.NewInt(100)),
		Storage:       map[common.Hash]common.Hash{storageKey: common.HexToHash("0x6666")},
		Code:          upgradedCode,
	}

	upgradeTimestamp := time.Unix(10, 0) // arbitrary timestamp to perform the network upgrade
	upgradeBytesJSON := fmt.Sprintf(
		`{
			"stateUpgrades": [
				{
					"blockTimestamp": %d,
					"accounts": {
						"%s": %s,
						"%s": %s
					}
				}
			]
		}`,
		upgradeTimestamp.Unix(),
		testEthAddrs[0].Hex(),
		mustMarshal(t, genesisAccountUpgrade),
		newAccount.Hex(),
		mustMarshal(t, newAccountUpgrade),
	)
	require.Contains(t, upgradeBytesJSON, upgradedCodeStr)

	// initialize the VM with these upgrade bytes
	issuer, vm, _, _ := GenesisVM(t, true, genesisStr, "", upgradeBytesJSON)
	defer func() { require.NoError(t, vm.Shutdown(context.Background())) }()

	// Verify the new account doesn't exist yet
	genesisState, err := vm.blockChain.State()
	require.NoError(t, err)
	require.Equal(t, common.Big0, genesisState.GetBalance(newAccount))

	// Advance the chain to the upgrade time
	vm.clock.Set(upgradeTimestamp)

	// Submit a successful (unrelated) transaction, so we can build a block
	// in this tx, testEthAddrs[1] sends 1 wei to itself.
	tx0 := types.NewTransaction(uint64(0), testEthAddrs[1], big.NewInt(1), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx0, err := types.SignTx(tx0, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[1])
	require.NoError(t, err)

	errs := vm.txPool.AddRemotesSync([]*types.Transaction{signedTx0})
	require.NoError(t, errs[0], "Failed to add tx")

	blk := issueAndAccept(t, issuer, vm)
	require.NotNil(t, blk)
	require.EqualValues(t, 1, blk.Height())

	// Verify the state upgrade was applied
	state, err := vm.blockChain.State()
	require.NoError(t, err)

	// Existing account
	expectedGenesisAccountBalance := new(big.Int).Add(
		genesisAccount.Balance,
		(*big.Int)(genesisAccountUpgrade.BalanceChange),
	)
	require.Equal(t, state.GetBalance(testEthAddrs[0]), expectedGenesisAccountBalance)
	require.Equal(t, state.GetState(testEthAddrs[0], storageKey), genesisAccountUpgrade.Storage[storageKey])
	require.Equal(t, state.GetCode(testEthAddrs[0]), upgradedCode)
	require.Equal(t, state.GetCodeHash(testEthAddrs[0]), crypto.Keccak256Hash(upgradedCode))
	require.Equal(t, state.GetNonce(testEthAddrs[0]), genesisAccount.Nonce) // Nonce should be preserved since it was non-zero

	// New account
	expectedNewAccountBalance := newAccountUpgrade.BalanceChange
	require.Equal(t, state.GetBalance(newAccount), (*big.Int)(expectedNewAccountBalance))
	require.Equal(t, state.GetCode(newAccount), upgradedCode)
	require.Equal(t, state.GetCodeHash(newAccount), crypto.Keccak256Hash(upgradedCode))
	require.Equal(t, state.GetNonce(newAccount), uint64(1)) // Nonce should be set to 1 when code is set if nonce was 0
	require.Equal(t, state.GetState(newAccount, storageKey), newAccountUpgrade.Storage[storageKey])
}
