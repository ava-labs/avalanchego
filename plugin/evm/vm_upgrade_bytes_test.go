// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"encoding/json"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/metrics"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/precompile"
	"github.com/stretchr/testify/assert"
)

func TestVMUpgradeBytesPrecompile(t *testing.T) {
	// Make a TxAllowListConfig upgrade at genesis and convert it to JSON to apply as upgradeBytes.
	enableAllowListTimestamp := time.Unix(0, 0) // enable at genesis
	upgradeConfig := &params.UpgradeConfig{
		PrecompileUpgrades: []params.PrecompileUpgrade{
			{
				TxAllowListConfig: precompile.NewTxAllowListConfig(big.NewInt(enableAllowListTimestamp.Unix()), testEthAddrs[0:1]),
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

	errs := vm.chain.GetTxPool().AddRemotesSync([]*types.Transaction{signedTx0})
	if err := errs[0]; err != nil {
		t.Fatalf("Failed to add tx at index: %s", err)
	}

	// Submit a rejected transaction, should throw an error
	tx1 := types.NewTransaction(uint64(0), testEthAddrs[1], big.NewInt(2), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx1, err := types.SignTx(tx1, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[1])
	if err != nil {
		t.Fatal(err)
	}
	errs = vm.chain.GetTxPool().AddRemotesSync([]*types.Transaction{signedTx1})
	if err := errs[0]; !errors.Is(err, precompile.ErrSenderAddressNotAllowListed) {
		t.Fatalf("expected ErrSenderAddressNotAllowListed, got: %s", err)
	}

	// shutdown the vm
	if err := vm.Shutdown(); err != nil {
		t.Fatal(err)
	}

	// prepare the new upgrade bytes to disable the TxAllowList
	disableAllowListTimestamp := enableAllowListTimestamp.Add(10 * time.Hour) // arbitrary choice
	upgradeConfig.PrecompileUpgrades = append(
		upgradeConfig.PrecompileUpgrades,
		params.PrecompileUpgrade{
			TxAllowListConfig: precompile.NewDisableTxAllowListConfig(big.NewInt(disableAllowListTimestamp.Unix())),
		},
	)
	upgradeBytesJSON, err = json.Marshal(upgradeConfig)
	if err != nil {
		t.Fatalf("could not marshal upgradeConfig to json: %s", err)
	}

	// restart the vm
	ctx := NewContext()
	if err := vm.Initialize(
		ctx, dbManager, []byte(genesisJSONSubnetEVM), upgradeBytesJSON, []byte{}, issuer, []*common.Fx{}, appSender,
	); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()
	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan)
	vm.clock.Set(disableAllowListTimestamp)

	// Make a block, previous rules still apply (TxAllowList is active)
	// Submit a successful transaction
	errs = vm.chain.GetTxPool().AddRemotesSync([]*types.Transaction{signedTx0})
	if err := errs[0]; err != nil {
		t.Fatalf("Failed to add tx at index: %s", err)
	}

	// Submit a rejected transaction, should throw an error
	errs = vm.chain.GetTxPool().AddRemotesSync([]*types.Transaction{signedTx1})
	if err := errs[0]; !errors.Is(err, precompile.ErrSenderAddressNotAllowListed) {
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
	assert.True(t, block.Timestamp().Cmp(big.NewInt(disableAllowListTimestamp.Unix())) >= 0)

	<-newTxPoolHeadChan // wait for new head in tx pool

	// retry the rejected Tx, which should now succeed
	errs = vm.chain.GetTxPool().AddRemotesSync([]*types.Transaction{signedTx1})
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

func TestVMUpgradeBytesNetworkUpgrades(t *testing.T) {
	// Hack: registering metrics uses global variables, so we need to disable metrics here so that we can initialize the VM twice.
	metrics.Enabled = false
	defer func() {
		metrics.Enabled = true
	}()

	// Get a json specifying a Network upgrade at genesis
	// to apply as upgradeBytes.
	subnetEVMTimestamp := time.Unix(10, 0)
	upgradeConfig := &params.UpgradeConfig{
		NetworkUpgrades: &params.NetworkUpgrades{
			SubnetEVMTimestamp: big.NewInt(subnetEVMTimestamp.Unix()),
		},
	}
	upgradeBytesJSON, err := json.Marshal(upgradeConfig)
	if err != nil {
		t.Fatalf("could not marshal upgradeConfig to json: %s", err)
	}

	// initialize the VM with these upgrade bytes
	issuer, vm, dbManager, appSender := GenesisVM(t, true, genesisJSONPreSubnetEVM, "", string(upgradeBytesJSON))
	vm.clock.Set(subnetEVMTimestamp)

	// verify upgrade is applied
	if !vm.chainConfig.IsSubnetEVM(big.NewInt(subnetEVMTimestamp.Unix())) {
		t.Fatal("expected subnet-evm network upgrade to have been enabled")
	}

	// Submit a successful transaction and build a block to move the chain head past the SubnetEVMTimestamp network upgrade
	tx0 := types.NewTransaction(uint64(0), testEthAddrs[0], big.NewInt(1), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx0, err := types.SignTx(tx0, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0])
	assert.NoError(t, err)
	errs := vm.chain.GetTxPool().AddRemotesSync([]*types.Transaction{signedTx0})
	if err := errs[0]; err != nil {
		t.Fatalf("Failed to add tx at index: %s", err)
	}

	issueAndAccept(t, issuer, vm) // make a block

	if err := vm.Shutdown(); err != nil {
		t.Fatal(err)
	}

	// VM should not start again without proper upgrade bytes.
	err = vm.Initialize(vm.ctx, dbManager, []byte(genesisJSONPreSubnetEVM), []byte{}, []byte{}, issuer, []*common.Fx{}, appSender)
	assert.ErrorContains(t, err, "mismatching SubnetEVM fork block timestamp in database")

	// VM should not start if fork is moved back
	upgradeConfig.NetworkUpgrades.SubnetEVMTimestamp = big.NewInt(2)
	upgradeBytesJSON, err = json.Marshal(upgradeConfig)
	if err != nil {
		t.Fatalf("could not marshal upgradeConfig to json: %s", err)
	}
	err = vm.Initialize(vm.ctx, dbManager, []byte(genesisJSONPreSubnetEVM), upgradeBytesJSON, []byte{}, issuer, []*common.Fx{}, appSender)
	assert.ErrorContains(t, err, "mismatching SubnetEVM fork block timestamp in database")

	// VM should not start if fork is moved forward
	upgradeConfig.NetworkUpgrades.SubnetEVMTimestamp = big.NewInt(30)
	upgradeBytesJSON, err = json.Marshal(upgradeConfig)
	if err != nil {
		t.Fatalf("could not marshal upgradeConfig to json: %s", err)
	}
	err = vm.Initialize(vm.ctx, dbManager, []byte(genesisJSONPreSubnetEVM), upgradeBytesJSON, []byte{}, issuer, []*common.Fx{}, appSender)
	assert.ErrorContains(t, err, "mismatching SubnetEVM fork block timestamp in database")
}

func TestVMUpgradeBytesNetworkUpgradesWithGenesis(t *testing.T) {
	// make genesis w/ fork at block 5
	var genesis core.Genesis
	if err := json.Unmarshal([]byte(genesisJSONPreSubnetEVM), &genesis); err != nil {
		t.Fatalf("could not unmarshal genesis bytes: %s", err)
	}
	genesisSubnetEVMTimestamp := big.NewInt(5)
	genesis.Config.SubnetEVMTimestamp = genesisSubnetEVMTimestamp
	genesisBytes, err := json.Marshal(&genesis)
	if err != nil {
		t.Fatalf("could not unmarshal genesis bytes: %s", err)
	}

	// Get a json specifying a Network upgrade at genesis
	// to apply as upgradeBytes.
	subnetEVMTimestamp := time.Unix(10, 0)
	upgradeConfig := &params.UpgradeConfig{
		NetworkUpgrades: &params.NetworkUpgrades{
			SubnetEVMTimestamp: big.NewInt(subnetEVMTimestamp.Unix()),
		},
	}
	upgradeBytesJSON, err := json.Marshal(upgradeConfig)
	if err != nil {
		t.Fatalf("could not marshal upgradeConfig to json: %s", err)
	}

	// initialize the VM with these upgrade bytes
	_, vm, _, _ := GenesisVM(t, true, string(genesisBytes), "", string(upgradeBytesJSON))

	// verify upgrade is rescheduled
	assert.False(t, vm.chainConfig.IsSubnetEVM(genesisSubnetEVMTimestamp))
	assert.True(t, vm.chainConfig.IsSubnetEVM(big.NewInt(subnetEVMTimestamp.Unix())))

	if err := vm.Shutdown(); err != nil {
		t.Fatal(err)
	}

	// abort a fork specified in genesis
	upgradeConfig.NetworkUpgrades.SubnetEVMTimestamp = nil
	upgradeBytesJSON, err = json.Marshal(upgradeConfig)
	if err != nil {
		t.Fatalf("could not marshal upgradeConfig to json: %s", err)
	}

	// initialize the VM with these upgrade bytes
	_, vm, _, _ = GenesisVM(t, true, string(genesisBytes), "", string(upgradeBytesJSON))

	// verify upgrade is aborted
	assert.False(t, vm.chainConfig.IsSubnetEVM(genesisSubnetEVMTimestamp))
	assert.False(t, vm.chainConfig.IsSubnetEVM(big.NewInt(subnetEVMTimestamp.Unix())))

	if err := vm.Shutdown(); err != nil {
		t.Fatal(err)
	}
}
