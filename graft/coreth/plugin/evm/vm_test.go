// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"crypto/rand"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/api/keystore"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	engCommon "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/coreth"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

var (
	testNetworkID    uint32 = 10
	testCChainID            = ids.ID{'c', 'c', 'h', 'a', 'i', 'n', 't', 'e', 's', 't'}
	testXChainID            = ids.ID{'t', 'e', 's', 't', 'x'}
	nonExistentID           = ids.ID{'F'}
	testTxFee               = uint64(1000)
	testKeys         []*crypto.PrivateKeySECP256K1R
	testEthAddrs     []common.Address // testEthAddrs[i] corresponds to testKeys[i]
	testShortIDAddrs []ids.ShortID
	testAvaxAssetID  = ids.ID{1, 2, 3}
	username         = "Johns"
	password         = "CjasdjhiPeirbSenfeI13" // #nosec G101
)

func init() {
	var b []byte
	factory := crypto.FactorySECP256K1R{}

	for _, key := range []string{
		"24jUJ9vZexUM6expyMcT48LBx27k1m7xpraoV62oSQAHdziao5",
		"2MMvUMsxx6zsHSNXJdFD8yc5XkancvwyKPwpw4xUK3TCGDuNBY",
		"cxb7KpGWhDMALTjNNSJ7UQkkomPesyWAPUaWRGdyeBNzR6f35",
	} {
		b, _ = formatting.Decode(formatting.CB58, key)
		pk, _ := factory.ToPrivateKey(b)
		secpKey := pk.(*crypto.PrivateKeySECP256K1R)
		testKeys = append(testKeys, secpKey)
		testEthAddrs = append(testEthAddrs, GetEthAddress(secpKey))
		testShortIDAddrs = append(testShortIDAddrs, pk.PublicKey().Address())
	}
}

// BuildGenesisTest returns the genesis bytes for Coreth VM to be used in testing
func BuildGenesisTest(t *testing.T) []byte {
	ss := StaticService{}

	genesisJSON := "{\"config\":{\"chainId\":43112,\"homesteadBlock\":0,\"daoForkBlock\":0,\"daoForkSupport\":true,\"eip150Block\":0,\"eip150Hash\":\"0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0\",\"eip155Block\":0,\"eip158Block\":0,\"byzantiumBlock\":0,\"constantinopleBlock\":0,\"petersburgBlock\":0,\"istanbulBlock\":0,\"muirGlacierBlock\":0},\"nonce\":\"0x0\",\"timestamp\":\"0x0\",\"extraData\":\"0x00\",\"gasLimit\":\"0x5f5e100\",\"difficulty\":\"0x0\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"coinbase\":\"0x0000000000000000000000000000000000000000\",\"alloc\":{\"0100000000000000000000000000000000000000\":{\"code\":\"0x7300000000000000000000000000000000000000003014608060405260043610603d5760003560e01c80631e010439146042578063b6510bb314606e575b600080fd5b605c60048036036020811015605657600080fd5b503560b1565b60408051918252519081900360200190f35b818015607957600080fd5b5060af60048036036080811015608e57600080fd5b506001600160a01b03813516906020810135906040810135906060013560b6565b005b30cd90565b836001600160a01b031681836108fc8690811502906040516000604051808303818888878c8acf9550505050505015801560f4573d6000803e3d6000fd5b505050505056fea26469706673582212201eebce970fe3f5cb96bf8ac6ba5f5c133fc2908ae3dcd51082cfee8f583429d064736f6c634300060a0033\",\"balance\":\"0x0\"}},\"number\":\"0x0\",\"gasUsed\":\"0x0\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\"}"

	genesis := &core.Genesis{}
	if err := json.Unmarshal([]byte(genesisJSON), genesis); err != nil {
		t.Fatalf("Problem unmarshaling genesis JSON: %s", err)
	}
	genesisReply, err := ss.BuildGenesis(nil, genesis)
	if err != nil {
		t.Fatalf("Failed to create test genesis")
	}
	genesisBytes, err := formatting.Decode(genesisReply.Encoding, genesisReply.Bytes)
	if err != nil {
		t.Fatalf("Failed to decode genesis bytes: %s", err)
	}
	return genesisBytes
}

func NewContext() *snow.Context {
	ctx := snow.DefaultContextTest()
	ctx.NetworkID = testNetworkID
	ctx.ChainID = testCChainID
	ctx.AVAXAssetID = testAvaxAssetID
	ctx.XChainID = ids.Empty.Prefix(0)
	aliaser := ctx.BCLookup.(*ids.Aliaser)
	_ = aliaser.Alias(testCChainID, "C")
	_ = aliaser.Alias(testCChainID, testCChainID.String())
	_ = aliaser.Alias(testXChainID, "X")
	_ = aliaser.Alias(testXChainID, testXChainID.String())

	// SNLookup might be required here???
	return ctx
}

// GenesisVM creates a VM instance with the genesis test bytes and returns
// the channel use to send messages to the engine, the vm, and atomic memory
func GenesisVM(t *testing.T, finishBootstrapping bool) (chan engCommon.Message, *VM, []byte, *atomic.Memory) {
	genesisBytes := BuildGenesisTest(t)
	ctx := NewContext()

	baseDB := memdb.New()

	m := &atomic.Memory{}
	m.Initialize(logging.NoLog{}, prefixdb.New([]byte{0}, baseDB))
	ctx.SharedMemory = m.NewSharedMemory(ctx.ChainID)

	// NB: this lock is intentionally left locked when this function returns.
	// The caller of this function is responsible for unlocking.
	ctx.Lock.Lock()

	userKeystore, err := keystore.CreateTestKeystore()
	if err != nil {
		t.Fatal(err)
	}
	if err := userKeystore.AddUser(username, password); err != nil {
		t.Fatal(err)
	}
	ctx.Keystore = userKeystore.NewBlockchainKeyStore(ctx.ChainID)

	issuer := make(chan engCommon.Message, 1)
	vm := &VM{
		txFee: testTxFee,
	}
	err = vm.Initialize(
		ctx,
		prefixdb.New([]byte{1}, baseDB),
		genesisBytes,
		issuer,
		[]*engCommon.Fx{},
	)
	if err != nil {
		t.Fatal(err)
	}

	if finishBootstrapping {
		if err := vm.Bootstrapping(); err != nil {
			t.Fatal(err)
		}

		if err := vm.Bootstrapped(); err != nil {
			t.Fatal(err)
		}
	}

	return issuer, vm, genesisBytes, m
}

func TestVMGenesis(t *testing.T) {
	_, vm, _, _ := GenesisVM(t, true)

	shutdownChan := make(chan error, 1)
	shutdownFunc := func() {
		err := vm.Shutdown()
		shutdownChan <- err
	}

	go shutdownFunc()
	shutdownTimeout := 50 * time.Millisecond
	ticker := time.NewTicker(shutdownTimeout)
	select {
	case <-ticker.C:
		t.Fatalf("VM shutdown took longer than timeout: %v", shutdownTimeout)
	case err := <-shutdownChan:
		if err != nil {
			t.Fatalf("Shutdown errored: %s", err)
		}
	}
}

func TestIssueAtomicTxs(t *testing.T) {
	issuer, vm, _, sharedMemory := GenesisVM(t, true)

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	importAmount := uint64(10000000)
	utxoID := avax.UTXOID{
		TxID: ids.ID{
			0x0f, 0x2f, 0x4f, 0x6f, 0x8e, 0xae, 0xce, 0xee,
			0x0d, 0x2d, 0x4d, 0x6d, 0x8c, 0xac, 0xcc, 0xec,
			0x0b, 0x2b, 0x4b, 0x6b, 0x8a, 0xaa, 0xca, 0xea,
			0x09, 0x29, 0x49, 0x69, 0x88, 0xa8, 0xc8, 0xe8,
		},
	}

	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  avax.Asset{ID: vm.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: importAmount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{testKeys[0].PublicKey().Address()},
			},
		},
	}
	utxoBytes, err := vm.codec.Marshal(codecVersion, utxo)
	if err != nil {
		t.Fatal(err)
	}

	xChainSharedMemory := sharedMemory.NewSharedMemory(vm.ctx.XChainID)
	inputID := utxo.InputID()
	if err := xChainSharedMemory.Put(vm.ctx.ChainID, []*atomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			testKeys[0].PublicKey().Address().Bytes(),
		},
	}}); err != nil {
		t.Fatal(err)
	}

	importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[0], []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.issueTx(importTx); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blk, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	if status := blk.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	vm.SetPreference(blk.ID())

	if err := blk.Accept(); err != nil {
		t.Fatal(err)
	}

	if status := blk.Status(); status != choices.Accepted {
		t.Fatalf("Expected status of accepted block to be %s, but found %s", choices.Accepted, status)
	}

	lastAcceptedID := vm.LastAccepted()
	if lastAcceptedID != blk.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk.ID(), lastAcceptedID)
	}

	exportTx, err := vm.newExportTx(vm.ctx.AVAXAssetID, importAmount-vm.txFee-1, vm.ctx.XChainID, testShortIDAddrs[0], []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.issueTx(exportTx); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blk2, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := blk2.Verify(); err != nil {
		t.Fatal(err)
	}

	if status := blk2.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := blk2.Accept(); err != nil {
		t.Fatal(err)
	}

	if status := blk2.Status(); status != choices.Accepted {
		t.Fatalf("Expected status of accepted block to be %s, but found %s", choices.Accepted, status)
	}

	lastAcceptedID = vm.LastAccepted()
	if lastAcceptedID != blk2.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk2.ID(), lastAcceptedID)
	}
}

func TestBuildEthTxBlock(t *testing.T) {
	issuer, vm, _, sharedMemory := GenesisVM(t, true)

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	key, err := coreth.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	importAmount := uint64(10000000)
	utxoID := avax.UTXOID{
		TxID: ids.ID{
			0x0f, 0x2f, 0x4f, 0x6f, 0x8e, 0xae, 0xce, 0xee,
			0x0d, 0x2d, 0x4d, 0x6d, 0x8c, 0xac, 0xcc, 0xec,
			0x0b, 0x2b, 0x4b, 0x6b, 0x8a, 0xaa, 0xca, 0xea,
			0x09, 0x29, 0x49, 0x69, 0x88, 0xa8, 0xc8, 0xe8,
		},
	}

	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  avax.Asset{ID: vm.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: importAmount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{testKeys[0].PublicKey().Address()},
			},
		},
	}
	utxoBytes, err := vm.codec.Marshal(codecVersion, utxo)
	if err != nil {
		t.Fatal(err)
	}

	xChainSharedMemory := sharedMemory.NewSharedMemory(vm.ctx.XChainID)
	inputID := utxo.InputID()
	if err := xChainSharedMemory.Put(vm.ctx.ChainID, []*atomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			testKeys[0].PublicKey().Address().Bytes(),
		},
	}}); err != nil {
		t.Fatal(err)
	}

	importTx, err := vm.newImportTx(vm.ctx.XChainID, key.Address, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.issueTx(importTx); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blk, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	if status := blk.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	vm.SetPreference(blk.ID())

	if err := blk.Accept(); err != nil {
		t.Fatal(err)
	}

	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), key.Address, big.NewInt(10), 21000, params.MinGasPrice, nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainID), key.PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}
	errs := vm.chain.AddRemoteTxs(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	<-issuer

	blk, err = vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	if status := blk.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := blk.Accept(); err != nil {
		t.Fatal(err)
	}

	if status := blk.Status(); status != choices.Accepted {
		t.Fatalf("Expected status of accepted block to be %s, but found %s", choices.Accepted, status)
	}

	lastAcceptedID := vm.LastAccepted()
	if lastAcceptedID != blk.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk.ID(), lastAcceptedID)
	}
}

func TestConflictingImportTxs(t *testing.T) {
	issuer, vm, _, sharedMemory := GenesisVM(t, true)

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	conflictKey, err := coreth.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	xChainSharedMemory := sharedMemory.NewSharedMemory(vm.ctx.XChainID)
	importTxs := make([]*Tx, 0, 3)
	conflictTxs := make([]*Tx, 0, 3)
	for i, key := range testKeys {
		importAmount := uint64(10000000)
		utxoID := avax.UTXOID{
			TxID: ids.ID{byte(i)},
		}

		utxo := &avax.UTXO{
			UTXOID: utxoID,
			Asset:  avax.Asset{ID: vm.ctx.AVAXAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: importAmount,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{key.PublicKey().Address()},
				},
			},
		}
		utxoBytes, err := vm.codec.Marshal(codecVersion, utxo)
		if err != nil {
			t.Fatal(err)
		}

		inputID := utxo.InputID()
		if err := xChainSharedMemory.Put(vm.ctx.ChainID, []*atomic.Element{{
			Key:   inputID[:],
			Value: utxoBytes,
			Traits: [][]byte{
				key.PublicKey().Address().Bytes(),
			},
		}}); err != nil {
			t.Fatal(err)
		}

		importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[i], []*crypto.PrivateKeySECP256K1R{key})
		if err != nil {
			t.Fatal(err)
		}
		importTxs = append(importTxs, importTx)

		conflictTx, err := vm.newImportTx(vm.ctx.XChainID, conflictKey.Address, []*crypto.PrivateKeySECP256K1R{key})
		if err != nil {
			t.Fatal(err)
		}
		conflictTxs = append(conflictTxs, conflictTx)
	}

	expectedParentBlkID := vm.LastAccepted()
	for i, tx := range importTxs {
		if err := vm.issueTx(tx); err != nil {
			t.Fatal(err)
		}

		<-issuer

		blk, err := vm.BuildBlock()
		if err != nil {
			t.Fatal(err)
		}

		if err := blk.Verify(); err != nil {
			t.Fatal(err)
		}

		if status := blk.Status(); status != choices.Processing {
			t.Fatalf("Expected status of built block %d to be %s, but found %s", i, choices.Processing, status)
		}

		if parentID := blk.Parent().ID(); parentID != expectedParentBlkID {
			t.Fatalf("Expected parent to have blockID %s, but found %s", expectedParentBlkID, parentID)
		}

		expectedParentBlkID = blk.ID()
		vm.SetPreference(blk.ID())
	}

	for i, tx := range conflictTxs {
		if err := vm.issueTx(tx); err != nil {
			t.Fatal(err)
		}

		<-issuer

		_, err := vm.BuildBlock()
		// The new block is verified in BuildBlock, so
		// BuildBlock should fail due to an attempt to
		// double spend an atomic UTXO.
		if err == nil {
			t.Fatalf("Block verification should have failed in BuildBlock %d due to double spending atomic UTXO", i)
		}
	}
}

// Regression test to ensure that after accepting block A
// then calling SetPreference on block B (when it becomes preferred)
// and the head of a longer chain (block D) does not corrupt the
// canonical chain.
//  A
// / \
// B  C
//    |
//    D
func TestSetPreferenceRace(t *testing.T) {
	// Create two VMs which will agree on block A and then
	// build the two distinct preferred chains above
	issuer1, vm1, _, sharedMemory1 := GenesisVM(t, true)
	issuer2, vm2, _, sharedMemory2 := GenesisVM(t, true)

	defer func() {
		if err := vm1.Shutdown(); err != nil {
			t.Fatal(err)
		}
		if err := vm2.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	key, err := coreth.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	// Import 1 AVAX
	importAmount := uint64(1000000000)
	utxoID := avax.UTXOID{
		TxID: ids.ID{
			0x0f, 0x2f, 0x4f, 0x6f, 0x8e, 0xae, 0xce, 0xee,
			0x0d, 0x2d, 0x4d, 0x6d, 0x8c, 0xac, 0xcc, 0xec,
			0x0b, 0x2b, 0x4b, 0x6b, 0x8a, 0xaa, 0xca, 0xea,
			0x09, 0x29, 0x49, 0x69, 0x88, 0xa8, 0xc8, 0xe8,
		},
	}

	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  avax.Asset{ID: vm1.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: importAmount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{testKeys[0].PublicKey().Address()},
			},
		},
	}
	utxoBytes, err := vm1.codec.Marshal(codecVersion, utxo)
	if err != nil {
		t.Fatal(err)
	}

	xChainSharedMemory1 := sharedMemory1.NewSharedMemory(vm1.ctx.XChainID)
	xChainSharedMemory2 := sharedMemory2.NewSharedMemory(vm2.ctx.XChainID)
	inputID := utxo.InputID()
	if err := xChainSharedMemory1.Put(vm1.ctx.ChainID, []*atomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			testKeys[0].PublicKey().Address().Bytes(),
		},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := xChainSharedMemory2.Put(vm2.ctx.ChainID, []*atomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			testKeys[0].PublicKey().Address().Bytes(),
		},
	}}); err != nil {
		t.Fatal(err)
	}

	importTx, err := vm1.newImportTx(vm1.ctx.XChainID, key.Address, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1.issueTx(importTx); err != nil {
		t.Fatal(err)
	}

	<-issuer1

	vm1BlkA, err := vm1.BuildBlock()
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	if err := vm1BlkA.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}

	if status := vm1BlkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	vm1.SetPreference(vm1BlkA.ID())

	vm2BlkA, err := vm2.ParseBlock(vm1BlkA.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	if err := vm2BlkA.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM2: %s", err)
	}
	if status := vm2BlkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of block on VM2 to be %s, but found %s", choices.Processing, status)
	}
	vm2.SetPreference(vm2BlkA.ID())

	if err := vm1BlkA.Accept(); err != nil {
		t.Fatalf("VM1 failed to accept block: %s", err)
	}
	if err := vm2BlkA.Accept(); err != nil {
		t.Fatalf("VM2 failed to accept block: %s", err)
	}

	// Create list of 10 successive transactions to build block A on vm1
	// and to be split into two separate blocks on VM2
	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), key.Address, big.NewInt(10), 21000, params.MinGasPrice, nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainID), key.PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	var (
		errs []error
	)

	// Add the remote transactions, build the block, and set VM1's preference for block A
	errs = vm1.chain.AddRemoteTxs(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM1 at index %d: %s", i, err)
		}
	}

	<-issuer1

	vm1BlkB, err := vm1.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkB.Verify(); err != nil {
		t.Fatal(err)
	}

	if status := vm1BlkB.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	vm1.SetPreference(vm1BlkB.ID())

	// Split the transactions over two blocks, and set VM2's preference to them in sequence
	// after building each block
	// Block C
	errs = vm2.chain.AddRemoteTxs(txs[0:5])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	<-issuer2
	vm2BlkC, err := vm2.BuildBlock()
	if err != nil {
		t.Fatalf("Failed to build BlkC on VM2: %s", err)
	}

	if err := vm2BlkC.Verify(); err != nil {
		t.Fatalf("BlkC failed verification on VM2: %s", err)
	}

	if status := vm2BlkC.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block C to be %s, but found %s", choices.Processing, status)
	}

	vm2.SetPreference(vm2BlkC.ID())

	// Block D
	errs = vm2.chain.AddRemoteTxs(txs[5:10])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM2 at index %d: %s", i, err)
		}
	}

	<-issuer2
	vm2BlkD, err := vm2.BuildBlock()
	if err != nil {
		t.Fatalf("Failed to build BlkD on VM2: %s", err)
	}

	if err := vm2BlkD.Verify(); err != nil {
		t.Fatalf("BlkD failed verification on VM2: %s", err)
	}

	if status := vm2BlkD.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block D to be %s, but found %s", choices.Processing, status)
	}

	vm2.SetPreference(vm2BlkD.ID())

	// VM1 receives blkC and blkD from VM1
	// and happens to call SetPreference on blkD without ever calling SetPreference
	// on blkC
	// Here we parse them in reverse order to simulate receiving a chain from the tip
	// back to the last accepted block as would typically be the case in the consensus
	// engine
	vm1BlkD, err := vm1.ParseBlock(vm2BlkD.Bytes())
	if err != nil {
		t.Fatalf("VM1 errored parsing blkD: %s", err)
	}
	vm1BlkC, err := vm1.ParseBlock(vm2BlkC.Bytes())
	if err != nil {
		t.Fatalf("VM1 errored parsing blkC: %s", err)
	}

	// The blocks must be verified in order. This invariant is maintained
	// in the consensus engine.
	if err := vm1BlkC.Verify(); err != nil {
		t.Fatalf("VM1 BlkC failed verification: %s", err)
	}
	if err := vm1BlkD.Verify(); err != nil {
		t.Fatalf("VM1 BlkD failed verification: %s", err)
	}

	// Set VM1's preference to blockD, skipping blockC
	vm1.SetPreference(vm1BlkD.ID())

	// Accept the longer chain on both VMs and ensure there are no errors
	// VM1 Accepts the blocks in order
	if err := vm1BlkC.Accept(); err != nil {
		t.Fatalf("VM1 BlkC failed on accept: %s", err)
	}
	if err := vm1BlkD.Accept(); err != nil {
		t.Fatalf("VM1 BlkC failed on accept: %s", err)
	}

	// VM2 Accepts the blocks in order
	if err := vm2BlkC.Accept(); err != nil {
		t.Fatalf("VM2 BlkC failed on accept: %s", err)
	}
	if err := vm2BlkD.Accept(); err != nil {
		t.Fatalf("VM2 BlkC failed on accept: %s", err)
	}

	log.Info("Validating canonical chain")
	// Verify the Canonical Chain for Both VMs
	if err := vm2.chain.ValidateCanonicalChain(); err != nil {
		t.Fatalf("VM2 failed canonical chain verification due to: %s", err)
	}

	if err := vm1.chain.ValidateCanonicalChain(); err != nil {
		t.Fatalf("VM1 failed canonical chain verification due to: %s", err)
	}
}

func TestGenesisStatus(t *testing.T) {
	_, vm, _, _ := GenesisVM(t, true)

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	genesisID := vm.LastAccepted()
	genesis, err := vm.GetBlock(genesisID)
	if err != nil {
		t.Fatal(err)
	}
	genesisStatus := genesis.Status()
	if genesisStatus != choices.Accepted {
		t.Fatalf("expected genesis status to be %s but was %s", choices.Accepted, genesisStatus)
	}
}

func TestConflictingTransitiveAncestryWithGap(t *testing.T) {
	issuer, vm, _, atomicMemory := GenesisVM(t, true)

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	key, err := coreth.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	key0 := testKeys[0]
	addr0 := key0.PublicKey().Address()

	key1 := testKeys[1]
	addr1 := key1.PublicKey().Address()

	importAmount := uint64(1000000000)

	utxo0ID := avax.UTXOID{}
	utxo1ID := avax.UTXOID{OutputIndex: 1}

	input0ID := utxo0ID.InputID()
	input1ID := utxo1ID.InputID()

	utxo0 := &avax.UTXO{
		UTXOID: utxo0ID,
		Asset:  avax.Asset{ID: vm.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: importAmount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{addr0},
			},
		},
	}
	utxo1 := &avax.UTXO{
		UTXOID: utxo1ID,
		Asset:  avax.Asset{ID: vm.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: importAmount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{addr1},
			},
		},
	}
	utxo0Bytes, err := vm.codec.Marshal(codecVersion, utxo0)
	if err != nil {
		t.Fatal(err)
	}
	utxo1Bytes, err := vm.codec.Marshal(codecVersion, utxo1)
	if err != nil {
		t.Fatal(err)
	}

	xChainSharedMemory := atomicMemory.NewSharedMemory(vm.ctx.XChainID)
	if err := xChainSharedMemory.Put(vm.ctx.ChainID, []*atomic.Element{
		{
			Key:   input0ID[:],
			Value: utxo0Bytes,
			Traits: [][]byte{
				addr0.Bytes(),
			},
		},
		{
			Key:   input1ID[:],
			Value: utxo1Bytes,
			Traits: [][]byte{
				addr1.Bytes(),
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	importTx0, err := vm.newImportTx(vm.ctx.XChainID, key.Address, []*crypto.PrivateKeySECP256K1R{key0})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.issueTx(importTx0); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blk0, err := vm.BuildBlock()
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	if err := blk0.Verify(); err != nil {
		t.Fatalf("Block failed verification: %s", err)
	}

	vm.SetPreference(blk0.ID())

	tx := types.NewTransaction(0, key.Address, big.NewInt(10), 21000, params.MinGasPrice, nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainID), key.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}

	// Add the remote transactions, build the block, and set VM1's preference for block A
	errs := vm.chain.AddRemoteTxs([]*types.Transaction{signedTx})
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM1 at index %d: %s", i, err)
		}
	}

	<-issuer

	blk1, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := blk1.Verify(); err != nil {
		t.Fatal(err)
	}

	vm.SetPreference(blk1.ID())

	importTx1, err := vm.newImportTx(vm.ctx.XChainID, key.Address, []*crypto.PrivateKeySECP256K1R{key1})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.issueTx(importTx1); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blk2, err := vm.BuildBlock()
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	if err := blk2.Verify(); err != nil {
		t.Fatalf("Block failed verification: %s", err)
	}

	vm.SetPreference(blk2.ID())

	if err := vm.issueTx(importTx0); err != nil {
		t.Fatal(err)
	}

	<-issuer

	_, err = vm.BuildBlock()
	if err == nil {
		t.Fatal("Shouldn't have been able to build an invalid block")
	}
}

func TestBonusBlocksTxs(t *testing.T) {
	issuer, vm, _, sharedMemory := GenesisVM(t, true)

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	importAmount := uint64(10000000)
	utxoID := avax.UTXOID{
		TxID: ids.ID{
			0x0f, 0x2f, 0x4f, 0x6f, 0x8e, 0xae, 0xce, 0xee,
			0x0d, 0x2d, 0x4d, 0x6d, 0x8c, 0xac, 0xcc, 0xec,
			0x0b, 0x2b, 0x4b, 0x6b, 0x8a, 0xaa, 0xca, 0xea,
			0x09, 0x29, 0x49, 0x69, 0x88, 0xa8, 0xc8, 0xe8,
		},
	}

	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  avax.Asset{ID: vm.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: importAmount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{testKeys[0].PublicKey().Address()},
			},
		},
	}
	utxoBytes, err := vm.codec.Marshal(codecVersion, utxo)
	if err != nil {
		t.Fatal(err)
	}

	xChainSharedMemory := sharedMemory.NewSharedMemory(vm.ctx.XChainID)
	inputID := utxo.InputID()
	if err := xChainSharedMemory.Put(vm.ctx.ChainID, []*atomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			testKeys[0].PublicKey().Address().Bytes(),
		},
	}}); err != nil {
		t.Fatal(err)
	}

	importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[0], []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.issueTx(importTx); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blk, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := xChainSharedMemory.Remove(vm.ctx.ChainID, [][]byte{inputID[:]}); err != nil {
		t.Fatal(err)
	}

	// TODO: better set/make bonus blocks dynamic
	evmBlock := blk.(*Block)
	evmBlock.id = ids.ID{10}
	vm.blockCache.Put(evmBlock.id, evmBlock)

	if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	if status := blk.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	vm.SetPreference(evmBlock.id)

	if err := blk.Accept(); err != nil {
		t.Fatal(err)
	}

	if status := blk.Status(); status != choices.Accepted {
		t.Fatalf("Expected status of accepted block to be %s, but found %s", choices.Accepted, status)
	}

	lastAcceptedID := vm.LastAccepted()
	if lastAcceptedID != evmBlock.id {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk.ID(), lastAcceptedID)
	}
}
