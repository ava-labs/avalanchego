// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
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
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	accountKeystore "github.com/ava-labs/coreth/accounts/keystore"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/rpc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie"
)

var (
	testNetworkID            uint32 = 10
	testCChainID                    = ids.ID{'c', 'c', 'h', 'a', 'i', 'n', 't', 'e', 's', 't'}
	testXChainID                    = ids.ID{'t', 'e', 's', 't', 'x'}
	nonExistentID                   = ids.ID{'F'}
	testTxFee                       = uint64(1000)
	testKeys                 []*crypto.PrivateKeySECP256K1R
	testEthAddrs             []common.Address // testEthAddrs[i] corresponds to testKeys[i]
	testShortIDAddrs         []ids.ShortID
	testAvaxAssetID          = ids.ID{1, 2, 3}
	username                 = "Johns"
	password                 = "CjasdjhiPeirbSenfeI13" // #nosec G101
	genesisJSONApricotPhase0 = "{\"config\":{\"chainId\":43112,\"homesteadBlock\":0,\"daoForkBlock\":0,\"daoForkSupport\":true,\"eip150Block\":0,\"eip150Hash\":\"0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0\",\"eip155Block\":0,\"eip158Block\":0,\"byzantiumBlock\":0,\"constantinopleBlock\":0,\"petersburgBlock\":0,\"istanbulBlock\":0,\"muirGlacierBlock\":0},\"nonce\":\"0x0\",\"timestamp\":\"0x0\",\"extraData\":\"0x00\",\"gasLimit\":\"0x5f5e100\",\"difficulty\":\"0x0\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"coinbase\":\"0x0000000000000000000000000000000000000000\",\"alloc\":{\"0100000000000000000000000000000000000000\":{\"code\":\"0x7300000000000000000000000000000000000000003014608060405260043610603d5760003560e01c80631e010439146042578063b6510bb314606e575b600080fd5b605c60048036036020811015605657600080fd5b503560b1565b60408051918252519081900360200190f35b818015607957600080fd5b5060af60048036036080811015608e57600080fd5b506001600160a01b03813516906020810135906040810135906060013560b6565b005b30cd90565b836001600160a01b031681836108fc8690811502906040516000604051808303818888878c8acf9550505050505015801560f4573d6000803e3d6000fd5b505050505056fea26469706673582212201eebce970fe3f5cb96bf8ac6ba5f5c133fc2908ae3dcd51082cfee8f583429d064736f6c634300060a0033\",\"balance\":\"0x0\"}},\"number\":\"0x0\",\"gasUsed\":\"0x0\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\"}"
	genesisJSONApricotPhase1 = "{\"config\":{\"chainId\":43112,\"homesteadBlock\":0,\"daoForkBlock\":0,\"daoForkSupport\":true,\"eip150Block\":0,\"eip150Hash\":\"0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0\",\"eip155Block\":0,\"eip158Block\":0,\"byzantiumBlock\":0,\"constantinopleBlock\":0,\"petersburgBlock\":0,\"istanbulBlock\":0,\"muirGlacierBlock\":0,\"apricotPhase1BlockTimestamp\":0},\"nonce\":\"0x0\",\"timestamp\":\"0x0\",\"extraData\":\"0x00\",\"gasLimit\":\"0x5f5e100\",\"difficulty\":\"0x0\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"coinbase\":\"0x0000000000000000000000000000000000000000\",\"alloc\":{\"0100000000000000000000000000000000000000\":{\"code\":\"0x7300000000000000000000000000000000000000003014608060405260043610603d5760003560e01c80631e010439146042578063b6510bb314606e575b600080fd5b605c60048036036020811015605657600080fd5b503560b1565b60408051918252519081900360200190f35b818015607957600080fd5b5060af60048036036080811015608e57600080fd5b506001600160a01b03813516906020810135906040810135906060013560b6565b005b30cd90565b836001600160a01b031681836108fc8690811502906040516000604051808303818888878c8acf9550505050505015801560f4573d6000803e3d6000fd5b505050505056fea26469706673582212201eebce970fe3f5cb96bf8ac6ba5f5c133fc2908ae3dcd51082cfee8f583429d064736f6c634300060a0033\",\"balance\":\"0x0\"}},\"number\":\"0x0\",\"gasUsed\":\"0x0\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\"}"
	genesisJSONApricotPhase2 = "{\"config\":{\"chainId\":43112,\"homesteadBlock\":0,\"daoForkBlock\":0,\"daoForkSupport\":true,\"eip150Block\":0,\"eip150Hash\":\"0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0\",\"eip155Block\":0,\"eip158Block\":0,\"byzantiumBlock\":0,\"constantinopleBlock\":0,\"petersburgBlock\":0,\"istanbulBlock\":0,\"muirGlacierBlock\":0,\"apricotPhase1BlockTimestamp\":0,\"apricotPhase2BlockTimestamp\":0},\"nonce\":\"0x0\",\"timestamp\":\"0x0\",\"extraData\":\"0x00\",\"gasLimit\":\"0x5f5e100\",\"difficulty\":\"0x0\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"coinbase\":\"0x0000000000000000000000000000000000000000\",\"alloc\":{\"0100000000000000000000000000000000000000\":{\"code\":\"0x7300000000000000000000000000000000000000003014608060405260043610603d5760003560e01c80631e010439146042578063b6510bb314606e575b600080fd5b605c60048036036020811015605657600080fd5b503560b1565b60408051918252519081900360200190f35b818015607957600080fd5b5060af60048036036080811015608e57600080fd5b506001600160a01b03813516906020810135906040810135906060013560b6565b005b30cd90565b836001600160a01b031681836108fc8690811502906040516000604051808303818888878c8acf9550505050505015801560f4573d6000803e3d6000fd5b505050505056fea26469706673582212201eebce970fe3f5cb96bf8ac6ba5f5c133fc2908ae3dcd51082cfee8f583429d064736f6c634300060a0033\",\"balance\":\"0x0\"}},\"number\":\"0x0\",\"gasUsed\":\"0x0\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\"}"

	apricotRulesPhase0 = params.Rules{}
	apricotRulesPhase1 = params.Rules{IsApricotPhase1: true}
	apricotRulesPhase2 = params.Rules{IsApricotPhase1: true, IsApricotPhase2: true}
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
func BuildGenesisTest(t *testing.T, genesisJSON string) []byte {
	ss := StaticService{}

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
func GenesisVM(t *testing.T, finishBootstrapping bool, genesisJSON string) (chan engCommon.Message, *VM, []byte, *atomic.Memory) {
	genesisBytes := BuildGenesisTest(t, genesisJSON)
	ctx := NewContext()
	baseDB := memdb.New()

	m := &atomic.Memory{}
	m.Initialize(logging.NoLog{}, prefixdb.New([]byte{0}, baseDB))
	ctx.SharedMemory = m.NewSharedMemory(ctx.ChainID)

	// NB: this lock is intentionally left locked when this function returns.
	// The caller of this function is responsible for unlocking.
	ctx.Lock.Lock()

	userKeystore := keystore.New(logging.NoLog{}, memdb.New())
	if err := userKeystore.CreateUser(username, password); err != nil {
		t.Fatal(err)
	}
	ctx.Keystore = userKeystore.NewBlockchainKeyStore(ctx.ChainID)

	issuer := make(chan engCommon.Message, 1)
	vm := &VM{
		txFee: testTxFee,
	}
	if err := vm.Initialize(
		ctx,
		prefixdb.New([]byte{1}, baseDB),
		genesisBytes,
		issuer,
		[]*engCommon.Fx{},
	); err != nil {
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
	genesisTests := []struct {
		name    string
		genesis string
	}{
		{
			name:    "Apricot Phase 0",
			genesis: genesisJSONApricotPhase0,
		},
		{
			name:    "Apricot Phase 1",
			genesis: genesisJSONApricotPhase1,
		},
		{
			name:    "Apricot Phase 2",
			genesis: genesisJSONApricotPhase2,
		},
	}
	for _, test := range genesisTests {
		t.Run(test.name, func(t *testing.T) {
			_, vm, _, _ := GenesisVM(t, true, test.genesis)

			defer func() {
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
			}()

			lastAcceptedID, err := vm.LastAccepted()
			if err != nil {
				t.Fatal(err)
			}

			if lastAcceptedID != ids.ID(vm.genesisHash) {
				t.Fatal("Expected last accepted block to match the genesis block hash")
			}

			genesisBlk, err := vm.GetBlock(lastAcceptedID)
			if err != nil {
				t.Fatalf("Failed to get genesis block due to %s", err)
			}

			if _, err := vm.ParseBlock(genesisBlk.Bytes()); err != nil {
				t.Fatalf("Failed to parse genesis block due to %s", err)
			}

			genesisStatus := genesisBlk.Status()
			if genesisStatus != choices.Accepted {
				t.Fatalf("expected genesis status to be %s but was %s", choices.Accepted, genesisStatus)
			}
		})
	}
}

func TestIssueAtomicTxs(t *testing.T) {
	issuer, vm, _, sharedMemory := GenesisVM(t, true, genesisJSONApricotPhase2)

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	importAmount := uint64(50000000)
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

	if err := vm.SetPreference(blk.ID()); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(); err != nil {
		t.Fatal(err)
	}

	if status := blk.Status(); status != choices.Accepted {
		t.Fatalf("Expected status of accepted block to be %s, but found %s", choices.Accepted, status)
	}

	lastAcceptedID, err := vm.LastAccepted()
	if err != nil {
		t.Fatal(err)
	}
	if lastAcceptedID != blk.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk.ID(), lastAcceptedID)
	}

	exportTx, err := vm.newExportTx(vm.ctx.AVAXAssetID, importAmount-(2*vm.txFee), vm.ctx.XChainID, testShortIDAddrs[0], []*crypto.PrivateKeySECP256K1R{testKeys[0]})
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

	lastAcceptedID, err = vm.LastAccepted()
	if err != nil {
		t.Fatal(err)
	}
	if lastAcceptedID != blk2.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk2.ID(), lastAcceptedID)
	}
}

func TestBuildEthTxBlock(t *testing.T) {
	issuer, vm, _, sharedMemory := GenesisVM(t, true, genesisJSONApricotPhase2)

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	key, err := accountKeystore.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	importAmount := uint64(20000000)
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

	if err := vm.SetPreference(blk.ID()); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(); err != nil {
		t.Fatal(err)
	}

	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), key.Address, big.NewInt(10), 21000, params.LaunchMinGasPrice, nil)
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

	lastAcceptedID, err := vm.LastAccepted()
	if err != nil {
		t.Fatal(err)
	}
	if lastAcceptedID != blk.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk.ID(), lastAcceptedID)
	}
}

func TestConflictingImportTxs(t *testing.T) {
	issuer, vm, _, sharedMemory := GenesisVM(t, true, genesisJSONApricotPhase0)

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	conflictKey, err := accountKeystore.NewKey(rand.Reader)
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

	expectedParentBlkID, err := vm.LastAccepted()
	if err != nil {
		t.Fatal(err)
	}
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
		if err := vm.SetPreference(blk.ID()); err != nil {
			t.Fatal(err)
		}
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
	issuer1, vm1, _, sharedMemory1 := GenesisVM(t, true, genesisJSONApricotPhase0)
	issuer2, vm2, _, sharedMemory2 := GenesisVM(t, true, genesisJSONApricotPhase0)

	defer func() {
		if err := vm1.Shutdown(); err != nil {
			t.Fatal(err)
		}

		if err := vm2.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	key, err := accountKeystore.NewKey(rand.Reader)
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

	if err := vm1.SetPreference(vm1BlkA.ID()); err != nil {
		t.Fatal(err)
	}

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
	if err := vm2.SetPreference(vm2BlkA.ID()); err != nil {
		t.Fatal(err)
	}

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
		tx := types.NewTransaction(uint64(i), key.Address, big.NewInt(10), 21000, params.LaunchMinGasPrice, nil)
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

	if err := vm1.SetPreference(vm1BlkB.ID()); err != nil {
		t.Fatal(err)
	}

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

	if err := vm2.SetPreference(vm2BlkC.ID()); err != nil {
		t.Fatal(err)
	}

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

	if err := vm2.SetPreference(vm2BlkD.ID()); err != nil {
		t.Fatal(err)
	}

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
	if err := vm1.SetPreference(vm1BlkD.ID()); err != nil {
		t.Fatal(err)
	}

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

func TestConflictingTransitiveAncestryWithGap(t *testing.T) {
	issuer, vm, _, atomicMemory := GenesisVM(t, true, genesisJSONApricotPhase0)

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	key, err := accountKeystore.NewKey(rand.Reader)
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

	if err := vm.SetPreference(blk0.ID()); err != nil {
		t.Fatal(err)
	}

	tx := types.NewTransaction(0, key.Address, big.NewInt(10), 21000, params.LaunchMinGasPrice, nil)
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

	if err := vm.SetPreference(blk1.ID()); err != nil {
		t.Fatal(err)
	}

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

	if err := vm.SetPreference(blk2.ID()); err != nil {
		t.Fatal(err)
	}

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
	issuer, vm, _, sharedMemory := GenesisVM(t, true, genesisJSONApricotPhase0)

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

	evmBlock := blk.(*Block)
	evmBlock.id = bonusBlocks.CappedList(1)[0]
	vm.blockCache.Put(evmBlock.id, evmBlock)

	if err := vm.ctx.SharedMemory.Remove(vm.ctx.XChainID, [][]byte{inputID[:]}); err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	if status := blk.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := vm.SetPreference(evmBlock.id); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(); err != nil {
		t.Fatal(err)
	}

	if status := blk.Status(); status != choices.Accepted {
		t.Fatalf("Expected status of accepted block to be %s, but found %s", choices.Accepted, status)
	}

	lastAcceptedID, err := vm.LastAccepted()
	if err != nil {
		t.Fatal(err)
	}
	if lastAcceptedID != evmBlock.id {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk.ID(), lastAcceptedID)
	}
}

// Regression test to ensure that a VM that accepts block A and B
// will not attempt to orphan either when verifying blocks C and D
// from another VM (which have a common ancestor under the finalized
// frontier).
//   A
//  / \
// B   C
//     |
//     D
func TestReorgProtection(t *testing.T) {
	issuer1, vm1, _, sharedMemory1 := GenesisVM(t, true, genesisJSONApricotPhase0)
	issuer2, vm2, _, sharedMemory2 := GenesisVM(t, true, genesisJSONApricotPhase0)

	defer func() {
		if err := vm1.Shutdown(); err != nil {
			t.Fatal(err)
		}

		if err := vm2.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	key, err := accountKeystore.NewKey(rand.Reader)
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

	if err := vm1.SetPreference(vm1BlkA.ID()); err != nil {
		t.Fatal(err)
	}

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
	if err := vm2.SetPreference(vm2BlkA.ID()); err != nil {
		t.Fatal(err)
	}

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
		tx := types.NewTransaction(uint64(i), key.Address, big.NewInt(10), 21000, params.LaunchMinGasPrice, nil)
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

	if err := vm1.SetPreference(vm1BlkB.ID()); err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkB.Accept(); err != nil {
		t.Fatalf("VM1 failed to accept block: %s", err)
	}

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
		t.Fatalf("Block failed verification on VM2: %s", err)
	}
	if status := vm2BlkC.Status(); status != choices.Processing {
		t.Fatalf("Expected status of block on VM2 to be %s, but found %s", choices.Processing, status)
	}

	vm1BlkC, err := vm1.ParseBlock(vm2BlkC.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	if err := vm1BlkC.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}

	// The below (setting preference blocks that have a common ancestor
	// with the preferred chain lower than the last finalized block)
	// should NEVER happen. However, the VM defends against this
	// just in case.
	if err := vm1.SetPreference(vm1BlkC.ID()); !strings.Contains(err.Error(), "cannot orphan finalized block") {
		t.Fatalf("Unexpected error when setting preference that would trigger reorg: %s", err)
	}

	if err := vm1BlkC.Accept(); !strings.Contains(err.Error(), "expected accepted parent block hash") {
		t.Fatalf("Unexpected error when setting block at finalized height: %s", err)
	}
}

// Regression test to ensure that a VM that accepts block C while preferring
// block B will trigger a reorg.
//   A
//  / \
// B   C
func TestNonCanonicalAccept(t *testing.T) {
	issuer1, vm1, _, sharedMemory1 := GenesisVM(t, true, genesisJSONApricotPhase0)
	issuer2, vm2, _, sharedMemory2 := GenesisVM(t, true, genesisJSONApricotPhase0)

	defer func() {
		if err := vm1.Shutdown(); err != nil {
			t.Fatal(err)
		}

		if err := vm2.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	key, err := accountKeystore.NewKey(rand.Reader)
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

	if err := vm1.SetPreference(vm1BlkA.ID()); err != nil {
		t.Fatal(err)
	}

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
	if err := vm2.SetPreference(vm2BlkA.ID()); err != nil {
		t.Fatal(err)
	}

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
		tx := types.NewTransaction(uint64(i), key.Address, big.NewInt(10), 21000, params.LaunchMinGasPrice, nil)
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

	if err := vm1.SetPreference(vm1BlkB.ID()); err != nil {
		t.Fatal(err)
	}

	vm1.chain.BlockChain().GetVMConfig().AllowUnfinalizedQueries = true

	blkBHeight := vm1BlkB.Height()
	blkBHash := vm1BlkB.(*Block).ethBlock.Hash()
	if b := vm1.chain.GetBlockByNumber(blkBHeight); b.Hash() != blkBHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkBHash.Hex(), b.Hash().Hex())
	}

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

	vm1BlkC, err := vm1.ParseBlock(vm2BlkC.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}

	if err := vm1BlkC.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}

	if err := vm1BlkC.Accept(); err != nil {
		t.Fatalf("VM1 failed to accept block: %s", err)
	}

	blkCHash := vm1BlkC.(*Block).ethBlock.Hash()
	if b := vm1.chain.GetBlockByNumber(blkBHeight); b.Hash() != blkCHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkCHash.Hex(), b.Hash().Hex())
	}
}

// Regression test to ensure that a VM that verifies block B, C, then
// D (preferring block B) does not trigger a reorg through the re-verification
// of block C or D.
//   A
//  / \
// B   C
//     |
//     D
func TestStickyPreference(t *testing.T) {
	issuer1, vm1, _, sharedMemory1 := GenesisVM(t, true, genesisJSONApricotPhase0)
	issuer2, vm2, _, sharedMemory2 := GenesisVM(t, true, genesisJSONApricotPhase0)

	defer func() {
		if err := vm1.Shutdown(); err != nil {
			t.Fatal(err)
		}

		if err := vm2.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	key, err := accountKeystore.NewKey(rand.Reader)
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

	if err := vm1.SetPreference(vm1BlkA.ID()); err != nil {
		t.Fatal(err)
	}

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
	if err := vm2.SetPreference(vm2BlkA.ID()); err != nil {
		t.Fatal(err)
	}

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
		tx := types.NewTransaction(uint64(i), key.Address, big.NewInt(10), 21000, params.LaunchMinGasPrice, nil)
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

	if err := vm1.SetPreference(vm1BlkB.ID()); err != nil {
		t.Fatal(err)
	}

	vm1.chain.BlockChain().GetVMConfig().AllowUnfinalizedQueries = true

	blkBHeight := vm1BlkB.Height()
	blkBHash := vm1BlkB.(*Block).ethBlock.Hash()
	if b := vm1.chain.GetBlockByNumber(blkBHeight); b.Hash() != blkBHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkBHash.Hex(), b.Hash().Hex())
	}

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

	if err := vm2.SetPreference(vm2BlkC.ID()); err != nil {
		t.Fatal(err)
	}

	errs = vm2.chain.AddRemoteTxs(txs[5:])
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

	// Parse blocks produced in vm2
	vm1BlkC, err := vm1.ParseBlock(vm2BlkC.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	blkCHash := vm1BlkC.(*Block).ethBlock.Hash()

	vm1BlkD, err := vm1.ParseBlock(vm2BlkD.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	blkDHeight := vm1BlkD.Height()
	blkDHash := vm1BlkD.(*Block).ethBlock.Hash()

	// Should be no-ops
	if err := vm1BlkC.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}
	if err := vm1BlkD.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}
	if b := vm1.chain.GetBlockByNumber(blkBHeight); b.Hash() != blkBHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkBHash.Hex(), b.Hash().Hex())
	}
	if b := vm1.chain.GetBlockByNumber(blkDHeight); b != nil {
		t.Fatalf("expected block at %d to be nil but got %s", blkDHeight, b.Hash().Hex())
	}
	if b := vm1.chain.BlockChain().CurrentBlock(); b.Hash() != blkBHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkBHash.Hex(), b.Hash().Hex())
	}

	// Should still be no-ops on re-verify
	if err := vm1BlkC.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}
	if err := vm1BlkD.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}
	if b := vm1.chain.GetBlockByNumber(blkBHeight); b.Hash() != blkBHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkBHash.Hex(), b.Hash().Hex())
	}
	if b := vm1.chain.GetBlockByNumber(blkDHeight); b != nil {
		t.Fatalf("expected block at %d to be nil but got %s", blkDHeight, b.Hash().Hex())
	}
	if b := vm1.chain.BlockChain().CurrentBlock(); b.Hash() != blkBHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkBHash.Hex(), b.Hash().Hex())
	}

	// Should be queryable after setting preference to side chain
	if err := vm1.SetPreference(vm1BlkD.ID()); err != nil {
		t.Fatal(err)
	}

	if b := vm1.chain.GetBlockByNumber(blkBHeight); b.Hash() != blkCHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkCHash.Hex(), b.Hash().Hex())
	}
	if b := vm1.chain.GetBlockByNumber(blkDHeight); b.Hash() != blkDHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkDHeight, blkDHash.Hex(), b.Hash().Hex())
	}
	if b := vm1.chain.BlockChain().CurrentBlock(); b.Hash() != blkDHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkDHash.Hex(), b.Hash().Hex())
	}

	// Attempt to accept out of order
	if err := vm1BlkD.Accept(); !strings.Contains(err.Error(), "expected accepted parent block hash") {
		t.Fatalf("unexpected error when accepting out of order block: %s", err)
	}

	// Accept in order
	if err := vm1BlkC.Accept(); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}
	if err := vm1BlkD.Accept(); err != nil {
		t.Fatalf("Block failed acceptance on VM1: %s", err)
	}

	// Ensure queryable after accepting
	if b := vm1.chain.GetBlockByNumber(blkBHeight); b.Hash() != blkCHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkCHash.Hex(), b.Hash().Hex())
	}
	if b := vm1.chain.GetBlockByNumber(blkDHeight); b.Hash() != blkDHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkDHeight, blkDHash.Hex(), b.Hash().Hex())
	}
	if b := vm1.chain.BlockChain().CurrentBlock(); b.Hash() != blkDHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkDHash.Hex(), b.Hash().Hex())
	}
}

// Regression test to ensure that a VM that prefers block B is able to parse
// block C but unable to parse block D because it names B as an uncle, which
// are not supported.
//   A
//  / \
// B   C
//     |
//     D
func TestUncleBlock(t *testing.T) {
	issuer1, vm1, _, sharedMemory1 := GenesisVM(t, true, genesisJSONApricotPhase0)
	issuer2, vm2, _, sharedMemory2 := GenesisVM(t, true, genesisJSONApricotPhase0)

	defer func() {
		if err := vm1.Shutdown(); err != nil {
			t.Fatal(err)
		}
		if err := vm2.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	key, err := accountKeystore.NewKey(rand.Reader)
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

	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), key.Address, big.NewInt(10), 21000, params.LaunchMinGasPrice, nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainID), key.PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	var (
		errs []error
	)

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

	// Create uncle block from blkD
	blkDEthBlock := vm2BlkD.(*Block).ethBlock
	uncles := []*types.Header{vm1BlkB.(*Block).ethBlock.Header()}
	uncleBlockHeader := types.CopyHeader(blkDEthBlock.Header())
	uncleBlockHeader.UncleHash = types.CalcUncleHash(uncles)

	uncleEthBlock := types.NewBlock(
		uncleBlockHeader,
		blkDEthBlock.Transactions(),
		uncles,
		nil,
		new(trie.Trie),
		blkDEthBlock.ExtData(),
		false,
	)
	uncleBlock := &Block{
		vm:       vm2,
		ethBlock: uncleEthBlock,
		id:       ids.ID(uncleEthBlock.Hash()),
	}
	if err := uncleBlock.Verify(); !errors.Is(err, errUnclesUnsupported) {
		t.Fatalf("VM2 should have failed with %q but got %q", errUnclesUnsupported, err.Error())
	}
	if _, err := vm1.ParseBlock(vm2BlkC.Bytes()); err != nil {
		t.Fatalf("VM1 errored parsing blkC: %s", err)
	}
	if _, err := vm1.ParseBlock(uncleBlock.Bytes()); !errors.Is(err, errUnclesUnsupported) {
		t.Fatalf("VM1 should have failed with %q but got %q", errUnclesUnsupported, err.Error())
	}
}

// Regression test to ensure that a VM that is not able to parse a block that
// contains no transactions.
func TestEmptyBlock(t *testing.T) {
	issuer, vm, _, sharedMemory := GenesisVM(t, true, genesisJSONApricotPhase0)

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	key, err := accountKeystore.NewKey(rand.Reader)
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

	xChainSharedMemory1 := sharedMemory.NewSharedMemory(vm.ctx.XChainID)
	inputID := utxo.InputID()
	if err := xChainSharedMemory1.Put(vm.ctx.ChainID, []*atomic.Element{{
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
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	// Create empty block from blkA
	ethBlock := blk.(*Block).ethBlock

	emptyEthBlock := types.NewBlock(
		types.CopyHeader(ethBlock.Header()),
		nil,
		nil,
		nil,
		new(trie.Trie),
		nil,
		false,
	)

	if len(emptyEthBlock.ExtData()) != 0 || emptyEthBlock.Header().ExtDataHash != (common.Hash{}) {
		t.Fatalf("emptyEthBlock should not have any extra data")
	}

	emptyBlock := &Block{
		vm:       vm,
		ethBlock: emptyEthBlock,
		id:       ids.ID(emptyEthBlock.Hash()),
	}

	if _, err := vm.ParseBlock(emptyBlock.Bytes()); !errors.Is(err, errEmptyBlock) {
		t.Fatalf("VM should have failed with errEmptyBlock but got %s", err.Error())
	}
	if err := emptyBlock.Verify(); !errors.Is(err, errEmptyBlock) {
		t.Fatalf("block should have failed verification with errEmptyBlock but got %s", err.Error())
	}
}

// Regression test to ensure that a VM that verifies block B, C, then
// D (preferring block B) reorgs when C and then D are accepted.
//   A
//  / \
// B   C
//     |
//     D
func TestAcceptReorg(t *testing.T) {
	issuer1, vm1, _, sharedMemory1 := GenesisVM(t, true, genesisJSONApricotPhase0)
	issuer2, vm2, _, sharedMemory2 := GenesisVM(t, true, genesisJSONApricotPhase0)

	defer func() {
		if err := vm1.Shutdown(); err != nil {
			t.Fatal(err)
		}

		if err := vm2.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	key, err := accountKeystore.NewKey(rand.Reader)
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

	if err := vm1.SetPreference(vm1BlkA.ID()); err != nil {
		t.Fatal(err)
	}

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
	if err := vm2.SetPreference(vm2BlkA.ID()); err != nil {
		t.Fatal(err)
	}

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
		tx := types.NewTransaction(uint64(i), key.Address, big.NewInt(10), 21000, params.LaunchMinGasPrice, nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainID), key.PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	// Add the remote transactions, build the block, and set VM1's preference
	// for block B
	errs := vm1.chain.AddRemoteTxs(txs)
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

	if err := vm1.SetPreference(vm1BlkB.ID()); err != nil {
		t.Fatal(err)
	}

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

	if err := vm2.SetPreference(vm2BlkC.ID()); err != nil {
		t.Fatal(err)
	}

	errs = vm2.chain.AddRemoteTxs(txs[5:])
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

	// Parse blocks produced in vm2
	vm1BlkC, err := vm1.ParseBlock(vm2BlkC.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}

	vm1BlkD, err := vm1.ParseBlock(vm2BlkD.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}

	if err := vm1BlkC.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}
	if err := vm1BlkD.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM1: %s", err)
	}

	blkBHash := vm1BlkB.(*Block).ethBlock.Hash()
	if b := vm1.chain.BlockChain().CurrentBlock(); b.Hash() != blkBHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkBHash.Hex(), b.Hash().Hex())
	}

	if err := vm1BlkC.Accept(); err != nil {
		t.Fatal(err)
	}

	blkCHash := vm1BlkC.(*Block).ethBlock.Hash()
	if b := vm1.chain.BlockChain().CurrentBlock(); b.Hash() != blkCHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkCHash.Hex(), b.Hash().Hex())
	}

	if err := vm1BlkD.Accept(); err != nil {
		t.Fatal(err)
	}

	blkDHash := vm1BlkD.(*Block).ethBlock.Hash()
	if b := vm1.chain.BlockChain().CurrentBlock(); b.Hash() != blkDHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkDHash.Hex(), b.Hash().Hex())
	}
}

func TestFutureBlock(t *testing.T) {
	issuer, vm, _, sharedMemory := GenesisVM(t, true, genesisJSONApricotPhase0)

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	key, err := accountKeystore.NewKey(rand.Reader)
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

	blkA, err := vm.BuildBlock()
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	// Create empty block from blkA
	blkAEthBlock := blkA.(*Block).ethBlock

	modifiedHeader := types.CopyHeader(blkAEthBlock.Header())
	// Set the VM's clock to the time of the produced block
	vm.clock.Set(time.Unix(int64(modifiedHeader.Time), 0))
	// Set the modified time to exceed the allowed future time
	modifiedTime := modifiedHeader.Time + uint64(maxFutureBlockTime.Seconds()+1)
	modifiedHeader.Time = modifiedTime
	modifiedBlock := types.NewBlock(
		modifiedHeader,
		nil,
		nil,
		nil,
		new(trie.Trie),
		blkAEthBlock.ExtData(),
		false,
	)

	futureBlock := &Block{
		vm:       vm,
		ethBlock: modifiedBlock,
		id:       ids.ID(modifiedBlock.Hash()),
	}

	if err := futureBlock.Verify(); err == nil {
		t.Fatal("Future block should have failed verification due to block timestamp too far in the future")
	} else if !strings.Contains(err.Error(), "block timestamp is too far in the future") {
		t.Fatalf("Expected error to be block timestamp too far in the future but found %s", err)
	}
}

// Regression test to ensure we can build blocks if we are starting with the
// Apricot Phase 1 ruleset in genesis.
func TestBuildApricotPhase1Block(t *testing.T) {
	issuer, vm, _, sharedMemory := GenesisVM(t, true, genesisJSONApricotPhase1)

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	key, err := accountKeystore.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

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

	if err := vm.SetPreference(blk.ID()); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(); err != nil {
		t.Fatal(err)
	}

	txs := make([]*types.Transaction, 10)
	for i := 0; i < 5; i++ {
		tx := types.NewTransaction(uint64(i), key.Address, big.NewInt(10), 21000, params.LaunchMinGasPrice, nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainID), key.PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}
	for i := 5; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), key.Address, big.NewInt(10), 21000, params.ApricotPhase1MinGasPrice, nil)
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

	lastAcceptedID, err := vm.LastAccepted()
	if err != nil {
		t.Fatal(err)
	}
	if lastAcceptedID != blk.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk.ID(), lastAcceptedID)
	}

	// Confirm all txs are present
	ethBlkTxs := vm.chain.GetBlockByNumber(2).Transactions()
	for i, tx := range txs {
		if len(ethBlkTxs) <= i {
			t.Fatalf("missing transactions expected: %d but found: %d", len(txs), len(ethBlkTxs))
		}
		if ethBlkTxs[i].Hash() != tx.Hash() {
			t.Fatalf("expected tx at index %d to have hash: %x but has: %x", i, txs[i].Hash(), tx.Hash())
		}
	}
}

// Regression test to ensure we can continue producing blocks across a transition from
// Apricot Phase 0 to Apricot Phase 1. This test all ensures another VM can
// sync blocks across a transition (where different verifications are applied).
func TestApricotPhase1Transition(t *testing.T) {
	// Setup custom JSON
	genesis := &core.Genesis{}
	if err := json.Unmarshal([]byte(genesisJSONApricotPhase1), genesis); err != nil {
		t.Fatalf("Problem unmarshaling genesis JSON: %s", err)
	}
	genesis.Config.ApricotPhase1BlockTimestamp = big.NewInt(time.Now().Add(5 * time.Second).Unix())
	customGenesisJSON, err := json.Marshal(genesis)
	if err != nil {
		t.Fatalf("Problem marshaling custom genesis JSON: %s", err)
	}

	// Initialize VMs
	issuer1, vm1, _, sharedMemory1 := GenesisVM(t, true, string(customGenesisJSON))
	defer func() {
		if err := vm1.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	key, err := accountKeystore.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

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

	importTx, err := vm1.newImportTx(vm1.ctx.XChainID, key.Address, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1.issueTx(importTx); err != nil {
		t.Fatal(err)
	}

	<-issuer1

	blkA, err := vm1.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := blkA.Verify(); err != nil {
		t.Fatal(err)
	}

	if status := blkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := vm1.SetPreference(blkA.ID()); err != nil {
		t.Fatal(err)
	}

	if err := blkA.Accept(); err != nil {
		t.Fatal(err)
	}

	txs := make([]*types.Transaction, 10)
	for i := 0; i < 5; i++ {
		tx := types.NewTransaction(uint64(i), key.Address, big.NewInt(10), 21000, params.LaunchMinGasPrice, nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainID), key.PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}
	for i := 5; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), key.Address, big.NewInt(10), 21000, params.ApricotPhase1MinGasPrice, nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainID), key.PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}
	errs := vm1.chain.AddRemoteTxs(txs[:5])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	<-issuer1

	blkB, err := vm1.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := blkB.Verify(); err != nil {
		t.Fatal(err)
	}

	// Wait for transition
	time.Sleep(5 * time.Second)

	if status := blkB.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := blkB.Accept(); err != nil {
		t.Fatal(err)
	}

	if status := blkB.Status(); status != choices.Accepted {
		t.Fatalf("Expected status of accepted block to be %s, but found %s", choices.Accepted, status)
	}

	lastAcceptedID, err := vm1.LastAccepted()
	if err != nil {
		t.Fatal(err)
	}
	if lastAcceptedID != blkB.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blkB.ID(), lastAcceptedID)
	}

	// Confirm all txs are present
	ethBlkTxs := vm1.chain.GetBlockByNumber(2).Transactions()
	for i, tx := range txs[:5] {
		if len(ethBlkTxs) <= i {
			t.Fatalf("missing transactions expected: %d but found: %d", len(txs), len(ethBlkTxs))
		}
		if ethBlkTxs[i].Hash() != tx.Hash() {
			t.Fatalf("expected tx at index %d to have hash: %x but has: %x", i, txs[i].Hash(), tx.Hash())
		}
	}

	errs = vm1.chain.AddRemoteTxs(txs[5:])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
	}

	<-issuer1

	blkC, err := vm1.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := blkC.Verify(); err != nil {
		t.Fatal(err)
	}

	if status := blkC.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := blkC.Accept(); err != nil {
		t.Fatal(err)
	}

	if status := blkC.Status(); status != choices.Accepted {
		t.Fatalf("Expected status of accepted block to be %s, but found %s", choices.Accepted, status)
	}

	lastAcceptedID, err = vm1.LastAccepted()
	if err != nil {
		t.Fatal(err)
	}
	if lastAcceptedID != blkC.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blkC.ID(), lastAcceptedID)
	}

	// Confirm all txs are present
	ethBlkTxs = vm1.chain.GetBlockByNumber(3).Transactions()
	for i, tx := range txs[5:] {
		if len(ethBlkTxs) <= i {
			t.Fatalf("missing transactions expected: %d but found: %d", len(txs), len(ethBlkTxs))
		}
		if ethBlkTxs[i].Hash() != tx.Hash() {
			t.Fatalf("expected tx at index %d to have hash: %x but has: %x", i, txs[i].Hash(), tx.Hash())
		}
	}

	// Sync up other genesis VM (after transition)
	_, vm2, _, sharedMemory2 := GenesisVM(t, true, string(customGenesisJSON))
	defer func() {
		if err := vm2.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()
	xChainSharedMemory2 := sharedMemory2.NewSharedMemory(vm2.ctx.XChainID)
	if err := xChainSharedMemory2.Put(vm2.ctx.ChainID, []*atomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			testKeys[0].PublicKey().Address().Bytes(),
		},
	}}); err != nil {
		t.Fatal(err)
	}

	vm2BlkA, err := vm2.ParseBlock(blkA.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	if err := vm2BlkA.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM2: %s", err)
	}
	if status := vm2BlkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of block on VM2 to be %s, but found %s", choices.Processing, status)
	}
	if err := vm2.SetPreference(vm2BlkA.ID()); err != nil {
		t.Fatal(err)
	}
	if err := vm2BlkA.Accept(); err != nil {
		t.Fatalf("VM2 failed to accept block: %s", err)
	}

	vm2BlkB, err := vm2.ParseBlock(blkB.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	if err := vm2BlkB.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM2: %s", err)
	}
	if status := vm2BlkB.Status(); status != choices.Processing {
		t.Fatalf("Expected status of block on VM2 to be %s, but found %s", choices.Processing, status)
	}
	if err := vm2.SetPreference(vm2BlkB.ID()); err != nil {
		t.Fatal(err)
	}
	if err := vm2BlkB.Accept(); err != nil {
		t.Fatalf("VM2 failed to accept block: %s", err)
	}

	vm2BlkC, err := vm2.ParseBlock(blkC.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	if err := vm2BlkC.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM2: %s", err)
	}
	if status := vm2BlkC.Status(); status != choices.Processing {
		t.Fatalf("Expected status of block on VM2 to be %s, but found %s", choices.Processing, status)
	}
	if err := vm2.SetPreference(vm2BlkC.ID()); err != nil {
		t.Fatal(err)
	}
	if err := vm2BlkC.Accept(); err != nil {
		t.Fatalf("VM2 failed to accept block: %s", err)
	}
}

func TestLastAcceptedBlockNumberAllow(t *testing.T) {
	issuer, vm, _, sharedMemory := GenesisVM(t, true, genesisJSONApricotPhase0)

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	key, err := accountKeystore.NewKey(rand.Reader)
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
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	if err := blk.Verify(); err != nil {
		t.Fatalf("Block failed verification on VM: %s", err)
	}

	if status := blk.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := vm.SetPreference(blk.ID()); err != nil {
		t.Fatal(err)
	}

	blkHeight := blk.Height()
	blkHash := blk.(*Block).ethBlock.Hash()

	vm.chain.BlockChain().GetVMConfig().AllowUnfinalizedQueries = true

	ctx := context.Background()
	b, err := vm.chain.APIBackend().BlockByNumber(ctx, rpc.BlockNumber(blkHeight))
	if err != nil {
		t.Fatal(err)
	}
	if b.Hash() != blkHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkHeight, blkHash.Hex(), b.Hash().Hex())
	}

	vm.chain.BlockChain().GetVMConfig().AllowUnfinalizedQueries = false

	_, err = vm.chain.APIBackend().BlockByNumber(ctx, rpc.BlockNumber(blkHeight))
	if !errors.Is(err, eth.ErrUnfinalizedData) {
		t.Fatalf("expected ErrUnfinalizedData but got %s", err.Error())
	}

	if err := blk.Accept(); err != nil {
		t.Fatalf("VM failed to accept block: %s", err)
	}

	if b := vm.chain.GetBlockByNumber(blkHeight); b.Hash() != blkHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkHeight, blkHash.Hex(), b.Hash().Hex())
	}
}

func TestBuildInvalidBlockHead(t *testing.T) {
	issuer, vm, _, _ := GenesisVM(t, true, genesisJSONApricotPhase0)

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	key0 := testKeys[0]
	addr0 := key0.PublicKey().Address()

	// Create the transaction
	utx := &UnsignedImportTx{
		NetworkID:    vm.ctx.NetworkID,
		BlockchainID: vm.ctx.ChainID,
		Outs: []EVMOutput{{
			Address: common.Address(addr0),
			Amount:  1 * units.Avax,
			AssetID: vm.ctx.AVAXAssetID,
		}},
		ImportedInputs: []*avax.TransferableInput{
			{
				Asset: avax.Asset{ID: vm.ctx.AVAXAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: 1 * units.Avax,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{0},
					},
				},
			},
		},
		SourceChain: vm.ctx.XChainID,
	}
	tx := &Tx{UnsignedAtomicTx: utx}
	if err := tx.Sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{key0}}); err != nil {
		t.Fatal(err)
	}

	currentBlock := vm.chain.BlockChain().CurrentBlock()

	if err := vm.issueTx(tx); err != nil {
		t.Fatal(err)
	}

	<-issuer

	if _, err := vm.BuildBlock(); err == nil {
		t.Fatalf("Unexpectedly created a block")
	}

	newCurrentBlock := vm.chain.BlockChain().CurrentBlock()

	if currentBlock.Hash() != newCurrentBlock.Hash() {
		t.Fatal("current block changed")
	}
}
