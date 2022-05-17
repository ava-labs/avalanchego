// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/api/keystore"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	engCommon "github.com/ava-labs/avalanchego/snow/engine/common"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/rpc"

	accountKeystore "github.com/ava-labs/coreth/accounts/keystore"
)

var (
	testNetworkID    uint32 = 10
	testCChainID            = ids.ID{'c', 'c', 'h', 'a', 'i', 'n', 't', 'e', 's', 't'}
	testXChainID            = ids.ID{'t', 'e', 's', 't', 'x'}
	nonExistentID           = ids.ID{'F'}
	testKeys         []*crypto.PrivateKeySECP256K1R
	testEthAddrs     []common.Address // testEthAddrs[i] corresponds to testKeys[i]
	testShortIDAddrs []ids.ShortID
	testAvaxAssetID  = ids.ID{1, 2, 3}
	username         = "Johns"
	password         = "CjasdjhiPeirbSenfeI13" // #nosec G101
	// Use chainId: 43111, so that it does not overlap with any Avalanche ChainIDs, which may have their
	// config overridden in vm.Initialize.
	genesisJSONApricotPhase0 = "{\"config\":{\"chainId\":43111,\"homesteadBlock\":0,\"daoForkBlock\":0,\"daoForkSupport\":true,\"eip150Block\":0,\"eip150Hash\":\"0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0\",\"eip155Block\":0,\"eip158Block\":0,\"byzantiumBlock\":0,\"constantinopleBlock\":0,\"petersburgBlock\":0,\"istanbulBlock\":0,\"muirGlacierBlock\":0},\"nonce\":\"0x0\",\"timestamp\":\"0x0\",\"extraData\":\"0x00\",\"gasLimit\":\"0x5f5e100\",\"difficulty\":\"0x0\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"coinbase\":\"0x0000000000000000000000000000000000000000\",\"alloc\":{\"0100000000000000000000000000000000000000\":{\"code\":\"0x7300000000000000000000000000000000000000003014608060405260043610603d5760003560e01c80631e010439146042578063b6510bb314606e575b600080fd5b605c60048036036020811015605657600080fd5b503560b1565b60408051918252519081900360200190f35b818015607957600080fd5b5060af60048036036080811015608e57600080fd5b506001600160a01b03813516906020810135906040810135906060013560b6565b005b30cd90565b836001600160a01b031681836108fc8690811502906040516000604051808303818888878c8acf9550505050505015801560f4573d6000803e3d6000fd5b505050505056fea26469706673582212201eebce970fe3f5cb96bf8ac6ba5f5c133fc2908ae3dcd51082cfee8f583429d064736f6c634300060a0033\",\"balance\":\"0x0\"}},\"number\":\"0x0\",\"gasUsed\":\"0x0\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\"}"
	genesisJSONApricotPhase1 = "{\"config\":{\"chainId\":43111,\"homesteadBlock\":0,\"daoForkBlock\":0,\"daoForkSupport\":true,\"eip150Block\":0,\"eip150Hash\":\"0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0\",\"eip155Block\":0,\"eip158Block\":0,\"byzantiumBlock\":0,\"constantinopleBlock\":0,\"petersburgBlock\":0,\"istanbulBlock\":0,\"muirGlacierBlock\":0,\"apricotPhase1BlockTimestamp\":0},\"nonce\":\"0x0\",\"timestamp\":\"0x0\",\"extraData\":\"0x00\",\"gasLimit\":\"0x5f5e100\",\"difficulty\":\"0x0\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"coinbase\":\"0x0000000000000000000000000000000000000000\",\"alloc\":{\"0100000000000000000000000000000000000000\":{\"code\":\"0x7300000000000000000000000000000000000000003014608060405260043610603d5760003560e01c80631e010439146042578063b6510bb314606e575b600080fd5b605c60048036036020811015605657600080fd5b503560b1565b60408051918252519081900360200190f35b818015607957600080fd5b5060af60048036036080811015608e57600080fd5b506001600160a01b03813516906020810135906040810135906060013560b6565b005b30cd90565b836001600160a01b031681836108fc8690811502906040516000604051808303818888878c8acf9550505050505015801560f4573d6000803e3d6000fd5b505050505056fea26469706673582212201eebce970fe3f5cb96bf8ac6ba5f5c133fc2908ae3dcd51082cfee8f583429d064736f6c634300060a0033\",\"balance\":\"0x0\"}},\"number\":\"0x0\",\"gasUsed\":\"0x0\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\"}"
	genesisJSONApricotPhase2 = "{\"config\":{\"chainId\":43111,\"homesteadBlock\":0,\"daoForkBlock\":0,\"daoForkSupport\":true,\"eip150Block\":0,\"eip150Hash\":\"0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0\",\"eip155Block\":0,\"eip158Block\":0,\"byzantiumBlock\":0,\"constantinopleBlock\":0,\"petersburgBlock\":0,\"istanbulBlock\":0,\"muirGlacierBlock\":0,\"apricotPhase1BlockTimestamp\":0,\"apricotPhase2BlockTimestamp\":0},\"nonce\":\"0x0\",\"timestamp\":\"0x0\",\"extraData\":\"0x00\",\"gasLimit\":\"0x5f5e100\",\"difficulty\":\"0x0\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"coinbase\":\"0x0000000000000000000000000000000000000000\",\"alloc\":{\"0100000000000000000000000000000000000000\":{\"code\":\"0x7300000000000000000000000000000000000000003014608060405260043610603d5760003560e01c80631e010439146042578063b6510bb314606e575b600080fd5b605c60048036036020811015605657600080fd5b503560b1565b60408051918252519081900360200190f35b818015607957600080fd5b5060af60048036036080811015608e57600080fd5b506001600160a01b03813516906020810135906040810135906060013560b6565b005b30cd90565b836001600160a01b031681836108fc8690811502906040516000604051808303818888878c8acf9550505050505015801560f4573d6000803e3d6000fd5b505050505056fea26469706673582212201eebce970fe3f5cb96bf8ac6ba5f5c133fc2908ae3dcd51082cfee8f583429d064736f6c634300060a0033\",\"balance\":\"0x0\"}},\"number\":\"0x0\",\"gasUsed\":\"0x0\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\"}"
	genesisJSONApricotPhase3 = "{\"config\":{\"chainId\":43111,\"homesteadBlock\":0,\"daoForkBlock\":0,\"daoForkSupport\":true,\"eip150Block\":0,\"eip150Hash\":\"0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0\",\"eip155Block\":0,\"eip158Block\":0,\"byzantiumBlock\":0,\"constantinopleBlock\":0,\"petersburgBlock\":0,\"istanbulBlock\":0,\"muirGlacierBlock\":0,\"apricotPhase1BlockTimestamp\":0,\"apricotPhase2BlockTimestamp\":0,\"apricotPhase3BlockTimestamp\":0},\"nonce\":\"0x0\",\"timestamp\":\"0x0\",\"extraData\":\"0x00\",\"gasLimit\":\"0x5f5e100\",\"difficulty\":\"0x0\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"coinbase\":\"0x0000000000000000000000000000000000000000\",\"alloc\":{\"0100000000000000000000000000000000000000\":{\"code\":\"0x7300000000000000000000000000000000000000003014608060405260043610603d5760003560e01c80631e010439146042578063b6510bb314606e575b600080fd5b605c60048036036020811015605657600080fd5b503560b1565b60408051918252519081900360200190f35b818015607957600080fd5b5060af60048036036080811015608e57600080fd5b506001600160a01b03813516906020810135906040810135906060013560b6565b005b30cd90565b836001600160a01b031681836108fc8690811502906040516000604051808303818888878c8acf9550505050505015801560f4573d6000803e3d6000fd5b505050505056fea26469706673582212201eebce970fe3f5cb96bf8ac6ba5f5c133fc2908ae3dcd51082cfee8f583429d064736f6c634300060a0033\",\"balance\":\"0x0\"}},\"number\":\"0x0\",\"gasUsed\":\"0x0\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\"}"
	genesisJSONApricotPhase4 = "{\"config\":{\"chainId\":43111,\"homesteadBlock\":0,\"daoForkBlock\":0,\"daoForkSupport\":true,\"eip150Block\":0,\"eip150Hash\":\"0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0\",\"eip155Block\":0,\"eip158Block\":0,\"byzantiumBlock\":0,\"constantinopleBlock\":0,\"petersburgBlock\":0,\"istanbulBlock\":0,\"muirGlacierBlock\":0,\"apricotPhase1BlockTimestamp\":0,\"apricotPhase2BlockTimestamp\":0,\"apricotPhase3BlockTimestamp\":0,\"apricotPhase4BlockTimestamp\":0},\"nonce\":\"0x0\",\"timestamp\":\"0x0\",\"extraData\":\"0x00\",\"gasLimit\":\"0x5f5e100\",\"difficulty\":\"0x0\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"coinbase\":\"0x0000000000000000000000000000000000000000\",\"alloc\":{\"0100000000000000000000000000000000000000\":{\"code\":\"0x7300000000000000000000000000000000000000003014608060405260043610603d5760003560e01c80631e010439146042578063b6510bb314606e575b600080fd5b605c60048036036020811015605657600080fd5b503560b1565b60408051918252519081900360200190f35b818015607957600080fd5b5060af60048036036080811015608e57600080fd5b506001600160a01b03813516906020810135906040810135906060013560b6565b005b30cd90565b836001600160a01b031681836108fc8690811502906040516000604051808303818888878c8acf9550505050505015801560f4573d6000803e3d6000fd5b505050505056fea26469706673582212201eebce970fe3f5cb96bf8ac6ba5f5c133fc2908ae3dcd51082cfee8f583429d064736f6c634300060a0033\",\"balance\":\"0x0\"}},\"number\":\"0x0\",\"gasUsed\":\"0x0\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\"}"
	genesisJSONApricotPhase5 = "{\"config\":{\"chainId\":43111,\"homesteadBlock\":0,\"daoForkBlock\":0,\"daoForkSupport\":true,\"eip150Block\":0,\"eip150Hash\":\"0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0\",\"eip155Block\":0,\"eip158Block\":0,\"byzantiumBlock\":0,\"constantinopleBlock\":0,\"petersburgBlock\":0,\"istanbulBlock\":0,\"muirGlacierBlock\":0,\"apricotPhase1BlockTimestamp\":0,\"apricotPhase2BlockTimestamp\":0,\"apricotPhase3BlockTimestamp\":0,\"apricotPhase4BlockTimestamp\":0, \"apricotPhase5BlockTimestamp\":0},\"nonce\":\"0x0\",\"timestamp\":\"0x0\",\"extraData\":\"0x00\",\"gasLimit\":\"0x5f5e100\",\"difficulty\":\"0x0\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"coinbase\":\"0x0000000000000000000000000000000000000000\",\"alloc\":{\"0100000000000000000000000000000000000000\":{\"code\":\"0x7300000000000000000000000000000000000000003014608060405260043610603d5760003560e01c80631e010439146042578063b6510bb314606e575b600080fd5b605c60048036036020811015605657600080fd5b503560b1565b60408051918252519081900360200190f35b818015607957600080fd5b5060af60048036036080811015608e57600080fd5b506001600160a01b03813516906020810135906040810135906060013560b6565b005b30cd90565b836001600160a01b031681836108fc8690811502906040516000604051808303818888878c8acf9550505050505015801560f4573d6000803e3d6000fd5b505050505056fea26469706673582212201eebce970fe3f5cb96bf8ac6ba5f5c133fc2908ae3dcd51082cfee8f583429d064736f6c634300060a0033\",\"balance\":\"0x0\"}},\"number\":\"0x0\",\"gasUsed\":\"0x0\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\"}"

	apricotRulesPhase0 = params.Rules{}
	apricotRulesPhase1 = params.Rules{IsApricotPhase1: true}
	apricotRulesPhase2 = params.Rules{IsApricotPhase1: true, IsApricotPhase2: true}
	apricotRulesPhase3 = params.Rules{IsApricotPhase1: true, IsApricotPhase2: true, IsApricotPhase3: true}
	apricotRulesPhase4 = params.Rules{IsApricotPhase1: true, IsApricotPhase2: true, IsApricotPhase3: true, IsApricotPhase4: true}
	apricotRulesPhase5 = params.Rules{IsApricotPhase1: true, IsApricotPhase2: true, IsApricotPhase3: true, IsApricotPhase4: true, IsApricotPhase5: true}
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

	minBlockTime = time.Millisecond
	maxBlockTime = time.Millisecond
	minBlockTimeAP4 = time.Millisecond
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
	ctx.NodeID = ids.GenerateTestNodeID()
	ctx.NetworkID = testNetworkID
	ctx.ChainID = testCChainID
	ctx.AVAXAssetID = testAvaxAssetID
	ctx.XChainID = testXChainID
	ctx.SharedMemory = testSharedMemory()
	aliaser := ctx.BCLookup.(ids.Aliaser)
	_ = aliaser.Alias(testCChainID, "C")
	_ = aliaser.Alias(testCChainID, testCChainID.String())
	_ = aliaser.Alias(testXChainID, "X")
	_ = aliaser.Alias(testXChainID, testXChainID.String())
	ctx.SNLookup = &snLookup{
		chainsToSubnet: map[ids.ID]ids.ID{
			constants.PlatformChainID: constants.PrimaryNetworkID,
			testXChainID:              constants.PrimaryNetworkID,
			testCChainID:              constants.PrimaryNetworkID,
		},
	}
	return ctx
}

type snLookup struct {
	chainsToSubnet map[ids.ID]ids.ID
}

func (sn *snLookup) SubnetID(chainID ids.ID) (ids.ID, error) {
	subnetID, ok := sn.chainsToSubnet[chainID]
	if !ok {
		return ids.ID{}, errors.New("unknown chain")
	}
	return subnetID, nil
}

func setupGenesis(t *testing.T,
	genesisJSON string,
) (*snow.Context,
	manager.Manager,
	[]byte,
	chan engCommon.Message,
	*atomic.Memory) {
	genesisBytes := BuildGenesisTest(t, genesisJSON)
	ctx := NewContext()

	baseDBManager := manager.NewMemDB(version.NewDefaultVersion(1, 4, 5))

	m := &atomic.Memory{}
	m.Initialize(logging.NoLog{}, prefixdb.New([]byte{0}, baseDBManager.Current().Database))
	ctx.SharedMemory = m.NewSharedMemory(ctx.ChainID)

	// NB: this lock is intentionally left locked when this function returns.
	// The caller of this function is responsible for unlocking.
	ctx.Lock.Lock()

	userKeystore := keystore.New(logging.NoLog{}, manager.NewMemDB(version.NewDefaultVersion(1, 4, 5)))
	if err := userKeystore.CreateUser(username, password); err != nil {
		t.Fatal(err)
	}
	ctx.Keystore = userKeystore.NewBlockchainKeyStore(ctx.ChainID)

	issuer := make(chan engCommon.Message, 1)
	prefixedDBManager := baseDBManager.NewPrefixDBManager([]byte{1})
	return ctx, prefixedDBManager, genesisBytes, issuer, m
}

// GenesisVM creates a VM instance with the genesis test bytes and returns
// the channel use to send messages to the engine, the vm, and atomic memory
func GenesisVM(t *testing.T,
	finishBootstrapping bool,
	genesisJSON string,
	configJSON string,
	upgradeJSON string,
) (chan engCommon.Message,
	*VM, manager.Manager,
	*atomic.Memory,
	*engCommon.SenderTest) {
	vm := &VM{}
	ctx, dbManager, genesisBytes, issuer, m := setupGenesis(t, genesisJSON)
	appSender := &engCommon.SenderTest{T: t}
	appSender.CantSendAppGossip = true
	appSender.SendAppGossipF = func([]byte) error { return nil }
	if err := vm.Initialize(
		ctx,
		dbManager,
		genesisBytes,
		[]byte(upgradeJSON),
		[]byte(configJSON),
		issuer,
		[]*engCommon.Fx{},
		appSender,
	); err != nil {
		t.Fatal(err)
	}

	if finishBootstrapping {
		assert.NoError(t, vm.SetState(snow.Bootstrapping))
		assert.NoError(t, vm.SetState(snow.NormalOp))
	}

	return issuer, vm, dbManager, m, appSender
}

func addUTXO(sharedMemory *atomic.Memory, ctx *snow.Context, txID ids.ID, index uint32, assetID ids.ID, amount uint64, addr ids.ShortID) (*avax.UTXO, error) {
	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID:        txID,
			OutputIndex: index,
		},
		Asset: avax.Asset{ID: assetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: amount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{addr},
			},
		},
	}
	utxoBytes, err := Codec.Marshal(codecVersion, utxo)
	if err != nil {
		return nil, err
	}

	xChainSharedMemory := sharedMemory.NewSharedMemory(ctx.XChainID)
	inputID := utxo.InputID()
	if err := xChainSharedMemory.Apply(map[ids.ID]*atomic.Requests{ctx.ChainID: {PutRequests: []*atomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			addr.Bytes(),
		},
	}}}}); err != nil {
		return nil, err
	}

	return utxo, nil
}

// GenesisVMWithUTXOs creates a GenesisVM and generates UTXOs in the X-Chain Shared Memory containing AVAX based on the [utxos] map
// Generates UTXOIDs by using a hash of the address in the [utxos] map such that the UTXOs will be generated deterministically.
func GenesisVMWithUTXOs(t *testing.T, finishBootstrapping bool, genesisJSON string, configJSON string, upgradeJSON string, utxos map[ids.ShortID]uint64) (chan engCommon.Message, *VM, manager.Manager, *atomic.Memory, *engCommon.SenderTest) {
	issuer, vm, dbManager, sharedMemory, sender := GenesisVM(t, finishBootstrapping, genesisJSON, configJSON, upgradeJSON)
	for addr, avaxAmount := range utxos {
		txID, err := ids.ToID(hashing.ComputeHash256(addr.Bytes()))
		if err != nil {
			t.Fatalf("Failed to generate txID from addr: %s", err)
		}
		if _, err := addUTXO(sharedMemory, vm.ctx, txID, 0, vm.ctx.AVAXAssetID, avaxAmount, addr); err != nil {
			t.Fatalf("Failed to add UTXO to shared memory: %s", err)
		}
	}

	return issuer, vm, dbManager, sharedMemory, sender
}

func TestVMConfig(t *testing.T) {
	txFeeCap := float64(11)
	enabledEthAPIs := []string{"internal-private-debug"}
	configJSON := fmt.Sprintf("{\"rpc-tx-fee-cap\": %g,\"eth-apis\": %s}", txFeeCap, fmt.Sprintf("[%q]", enabledEthAPIs[0]))
	_, vm, _, _, _ := GenesisVM(t, false, genesisJSONApricotPhase0, configJSON, "")
	assert.Equal(t, vm.config.RPCTxFeeCap, txFeeCap, "Tx Fee Cap should be set")
	assert.Equal(t, vm.config.EthAPIs(), enabledEthAPIs, "EnabledEthAPIs should be set")
	assert.NoError(t, vm.Shutdown())
}

func TestVMConfigDefaults(t *testing.T) {
	txFeeCap := float64(11)
	enabledEthAPIs := []string{"internal-private-debug"}
	configJSON := fmt.Sprintf("{\"rpc-tx-fee-cap\": %g,\"eth-apis\": %s}", txFeeCap, fmt.Sprintf("[%q]", enabledEthAPIs[0]))
	_, vm, _, _, _ := GenesisVM(t, false, genesisJSONApricotPhase0, configJSON, "")

	var vmConfig Config
	vmConfig.SetDefaults()
	vmConfig.RPCTxFeeCap = txFeeCap
	vmConfig.EnabledEthAPIs = enabledEthAPIs
	assert.Equal(t, vmConfig, vm.config, "VM Config should match default with overrides")
	assert.NoError(t, vm.Shutdown())
}

func TestVMNilConfig(t *testing.T) {
	_, vm, _, _, _ := GenesisVM(t, false, genesisJSONApricotPhase0, "", "")

	// VM Config should match defaults if no config is passed in
	var vmConfig Config
	vmConfig.SetDefaults()
	assert.Equal(t, vmConfig, vm.config, "VM Config should match default config")
	assert.NoError(t, vm.Shutdown())
}

func TestVMContinuosProfiler(t *testing.T) {
	profilerDir := t.TempDir()
	profilerFrequency := 500 * time.Millisecond
	configJSON := fmt.Sprintf("{\"continuous-profiler-dir\": %q,\"continuous-profiler-frequency\": \"500ms\"}", profilerDir)
	_, vm, _, _, _ := GenesisVM(t, false, genesisJSONApricotPhase0, configJSON, "")
	assert.Equal(t, vm.config.ContinuousProfilerDir, profilerDir, "profiler dir should be set")
	assert.Equal(t, vm.config.ContinuousProfilerFrequency.Duration, profilerFrequency, "profiler frequency should be set")

	// Sleep for twice the frequency of the profiler to give it time
	// to generate the first profile.
	time.Sleep(2 * time.Second)
	assert.NoError(t, vm.Shutdown())

	// Check that the first profile was generated
	expectedFileName := filepath.Join(profilerDir, "cpu.profile.1")
	_, err := os.Stat(expectedFileName)
	assert.NoError(t, err, "Expected continuous profiler to generate the first CPU profile at %s", expectedFileName)
}

func TestVMUpgrades(t *testing.T) {
	genesisTests := []struct {
		name             string
		genesis          string
		expectedGasPrice *big.Int
	}{
		{
			name:             "Apricot Phase 0",
			genesis:          genesisJSONApricotPhase0,
			expectedGasPrice: big.NewInt(params.LaunchMinGasPrice),
		},
		{
			name:             "Apricot Phase 1",
			genesis:          genesisJSONApricotPhase1,
			expectedGasPrice: big.NewInt(params.ApricotPhase1MinGasPrice),
		},
		{
			name:             "Apricot Phase 2",
			genesis:          genesisJSONApricotPhase2,
			expectedGasPrice: big.NewInt(params.ApricotPhase1MinGasPrice),
		},
		{
			name:             "Apricot Phase 3",
			genesis:          genesisJSONApricotPhase3,
			expectedGasPrice: big.NewInt(0),
		},
		{
			name:             "Apricot Phase 4",
			genesis:          genesisJSONApricotPhase4,
			expectedGasPrice: big.NewInt(0),
		},
		{
			name:             "Apricot Phase 5",
			genesis:          genesisJSONApricotPhase5,
			expectedGasPrice: big.NewInt(0),
		},
	}
	for _, test := range genesisTests {
		t.Run(test.name, func(t *testing.T) {
			_, vm, _, _, _ := GenesisVM(t, true, test.genesis, "", "")

			if gasPrice := vm.chain.GetTxPool().GasPrice(); gasPrice.Cmp(test.expectedGasPrice) != 0 {
				t.Fatalf("Expected pool gas price to be %d but found %d", test.expectedGasPrice, gasPrice)
			}
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

			if height := genesisBlk.Height(); height != 0 {
				t.Fatalf("Expected height of geneiss block to be 0, found: %d", height)
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

// Simple test to ensure we can issue an import transaction followed by an export transaction
// and they will be indexed correctly when accepted.
func TestIssueAtomicTxs(t *testing.T) {
	importAmount := uint64(50000000)
	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase2, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.issueTx(importTx, true /*=local*/); err != nil {
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

	if lastAcceptedID, err := vm.LastAccepted(); err != nil {
		t.Fatal(err)
	} else if lastAcceptedID != blk.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk.ID(), lastAcceptedID)
	}

	exportTx, err := vm.newExportTx(vm.ctx.AVAXAssetID, importAmount-(2*params.AvalancheAtomicTxFee), vm.ctx.XChainID, testShortIDAddrs[0], initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.issueTx(exportTx, true /*=local*/); err != nil {
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

	if lastAcceptedID, err := vm.LastAccepted(); err != nil {
		t.Fatal(err)
	} else if lastAcceptedID != blk2.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk2.ID(), lastAcceptedID)
	}

	// Check that both atomic transactions were indexed as expected.
	indexedImportTx, status, height, err := vm.getAtomicTx(importTx.ID())
	assert.NoError(t, err)
	assert.Equal(t, Accepted, status)
	assert.Equal(t, uint64(1), height, "expected height of indexed import tx to be 1")
	assert.Equal(t, indexedImportTx.ID(), importTx.ID(), "expected ID of indexed import tx to match original txID")

	indexedExportTx, status, height, err := vm.getAtomicTx(exportTx.ID())
	assert.NoError(t, err)
	assert.Equal(t, Accepted, status)
	assert.Equal(t, uint64(2), height, "expected height of indexed export tx to be 2")
	assert.Equal(t, indexedExportTx.ID(), exportTx.ID(), "expected ID of indexed import tx to match original txID")
}

func TestBuildEthTxBlock(t *testing.T) {
	importAmount := uint64(20000000)
	issuer, vm, dbManager, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase2, "{\"pruning-enabled\":true}", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan)

	importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.issueTx(importTx, true /*=local*/); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blk1, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := blk1.Verify(); err != nil {
		t.Fatal(err)
	}

	if status := blk1.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := vm.SetPreference(blk1.ID()); err != nil {
		t.Fatal(err)
	}

	if err := blk1.Accept(); err != nil {
		t.Fatal(err)
	}

	newHead := <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk1.ID()) {
		t.Fatalf("Expected new block to match")
	}

	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), testEthAddrs[0], big.NewInt(10), 21000, big.NewInt(params.LaunchMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainID), testKeys[0].ToECDSA())
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}
	errs := vm.chain.AddRemoteTxsSync(txs)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add tx at index %d: %s", i, err)
		}
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

	newHead = <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk2.ID()) {
		t.Fatalf("Expected new block to match")
	}

	if status := blk2.Status(); status != choices.Accepted {
		t.Fatalf("Expected status of accepted block to be %s, but found %s", choices.Accepted, status)
	}

	lastAcceptedID, err := vm.LastAccepted()
	if err != nil {
		t.Fatal(err)
	}
	if lastAcceptedID != blk2.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blk2.ID(), lastAcceptedID)
	}

	ethBlk1 := blk1.(*chain.BlockWrapper).Block.(*Block).ethBlock
	if ethBlk1Root := ethBlk1.Root(); !vm.chain.BlockChain().HasState(ethBlk1Root) {
		t.Fatalf("Expected blk1 state root to not yet be pruned after blk2 was accepted because of tip buffer")
	}

	// Clear the cache and ensure that GetBlock returns internal blocks with the correct status
	vm.State.Flush()
	blk2Refreshed, err := vm.GetBlockInternal(blk2.ID())
	if err != nil {
		t.Fatal(err)
	}
	if status := blk2Refreshed.Status(); status != choices.Accepted {
		t.Fatalf("Expected refreshed blk2 to be Accepted, but found status: %s", status)
	}

	blk1RefreshedID := blk2Refreshed.Parent()
	blk1Refreshed, err := vm.GetBlockInternal(blk1RefreshedID)
	if err != nil {
		t.Fatal(err)
	}
	if status := blk1Refreshed.Status(); status != choices.Accepted {
		t.Fatalf("Expected refreshed blk1 to be Accepted, but found status: %s", status)
	}

	if blk1Refreshed.ID() != blk1.ID() {
		t.Fatalf("Found unexpected blkID for parent of blk2")
	}

	restartedVM := &VM{}
	if err := restartedVM.Initialize(
		NewContext(),
		dbManager,
		[]byte(genesisJSONApricotPhase2),
		[]byte(""),
		[]byte("{\"pruning-enabled\":true}"),
		issuer,
		[]*engCommon.Fx{},
		nil,
	); err != nil {
		t.Fatal(err)
	}

	// State root should not have been committed and discarded on restart
	if ethBlk1Root := ethBlk1.Root(); restartedVM.chain.BlockChain().HasState(ethBlk1Root) {
		t.Fatalf("Expected blk1 state root to be pruned after blk2 was accepted on top of it in pruning mode")
	}

	// State root should be committed when accepted tip on shutdown
	ethBlk2 := blk2.(*chain.BlockWrapper).Block.(*Block).ethBlock
	if ethBlk2Root := ethBlk2.Root(); !restartedVM.chain.BlockChain().HasState(ethBlk2Root) {
		t.Fatalf("Expected blk2 state root to not be pruned after shutdown (last accepted tip should be committed)")
	}
}

func testConflictingImportTxs(t *testing.T, genesis string) {
	importAmount := uint64(10000000)
	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, genesis, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
		testShortIDAddrs[1]: importAmount,
		testShortIDAddrs[2]: importAmount,
	})

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	importTxs := make([]*Tx, 0, 3)
	conflictTxs := make([]*Tx, 0, 3)
	for i, key := range testKeys {
		importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[i], initialBaseFee, []*crypto.PrivateKeySECP256K1R{key})
		if err != nil {
			t.Fatal(err)
		}
		importTxs = append(importTxs, importTx)

		conflictAddr := testEthAddrs[(i+1)%len(testEthAddrs)]
		conflictTx, err := vm.newImportTx(vm.ctx.XChainID, conflictAddr, initialBaseFee, []*crypto.PrivateKeySECP256K1R{key})
		if err != nil {
			t.Fatal(err)
		}
		conflictTxs = append(conflictTxs, conflictTx)
	}

	expectedParentBlkID, err := vm.LastAccepted()
	if err != nil {
		t.Fatal(err)
	}
	for i, tx := range importTxs[:2] {
		if err := vm.issueTx(tx, true /*=local*/); err != nil {
			t.Fatal(err)
		}

		<-issuer

		vm.clock.Set(vm.clock.Time().Add(2 * time.Second))
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

		if parentID := blk.Parent(); parentID != expectedParentBlkID {
			t.Fatalf("Expected parent to have blockID %s, but found %s", expectedParentBlkID, parentID)
		}

		expectedParentBlkID = blk.ID()
		if err := vm.SetPreference(blk.ID()); err != nil {
			t.Fatal(err)
		}
	}

	// Check that for each conflict tx (whose conflict is in the chain ancestry)
	// the VM returns an error when it attempts to issue the conflict into the mempool
	// and when it attempts to build a block with the conflict force added to the mempool.
	for i, tx := range conflictTxs[:2] {
		if err := vm.issueTx(tx, true /*=local*/); err == nil {
			t.Fatal("Expected issueTx to fail due to conflicting transaction")
		}
		// Force issue transaction directly to the mempool
		if err := vm.mempool.ForceAddTx(tx); err != nil {
			t.Fatal(err)
		}
		<-issuer

		vm.clock.Set(vm.clock.Time().Add(2 * time.Second))
		_, err = vm.BuildBlock()
		// The new block is verified in BuildBlock, so
		// BuildBlock should fail due to an attempt to
		// double spend an atomic UTXO.
		if err == nil {
			t.Fatalf("Block verification should have failed in BuildBlock %d due to double spending atomic UTXO", i)
		}
	}

	// Generate one more valid block so that we can copy the header to create an invalid block
	// with modified extra data. This new block will be invalid for more than one reason (invalid merkle root)
	// so we check to make sure that the expected error is returned from block verification.
	if err := vm.issueTx(importTxs[2], true); err != nil {
		t.Fatal(err)
	}
	<-issuer
	vm.clock.Set(vm.clock.Time().Add(2 * time.Second))

	validBlock, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := validBlock.Verify(); err != nil {
		t.Fatal(err)
	}

	validEthBlock := validBlock.(*chain.BlockWrapper).Block.(*Block).ethBlock

	rules := vm.currentRules()
	var extraData []byte
	switch {
	case rules.IsApricotPhase5:
		extraData, err = vm.codec.Marshal(codecVersion, []*Tx{conflictTxs[1]})
	default:
		extraData, err = vm.codec.Marshal(codecVersion, conflictTxs[1])
	}
	if err != nil {
		t.Fatal(err)
	}

	conflictingAtomicTxBlock := types.NewBlock(
		types.CopyHeader(validEthBlock.Header()),
		nil,
		nil,
		nil,
		new(trie.Trie),
		extraData,
		true,
	)

	blockBytes, err := rlp.EncodeToBytes(conflictingAtomicTxBlock)
	if err != nil {
		t.Fatal(err)
	}

	parsedBlock, err := vm.ParseBlock(blockBytes)
	if err != nil {
		t.Fatal(err)
	}

	if err := parsedBlock.Verify(); !errors.Is(err, errConflictingAtomicInputs) {
		t.Fatalf("Expected to fail with err: %s, but found err: %s", errConflictingAtomicInputs, err)
	}

	if !rules.IsApricotPhase5 {
		return
	}

	extraData, err = vm.codec.Marshal(codecVersion, []*Tx{importTxs[2], conflictTxs[2]})
	if err != nil {
		t.Fatal(err)
	}

	header := types.CopyHeader(validEthBlock.Header())
	header.ExtDataGasUsed.Mul(common.Big2, header.ExtDataGasUsed)

	internalConflictBlock := types.NewBlock(
		header,
		nil,
		nil,
		nil,
		new(trie.Trie),
		extraData,
		true,
	)

	blockBytes, err = rlp.EncodeToBytes(internalConflictBlock)
	if err != nil {
		t.Fatal(err)
	}

	parsedBlock, err = vm.ParseBlock(blockBytes)
	if err != nil {
		t.Fatal(err)
	}

	if err := parsedBlock.Verify(); !errors.Is(err, errConflictingAtomicInputs) {
		t.Fatalf("Expected to fail with err: %s, but found err: %s", errConflictingAtomicInputs, err)
	}
}

func TestReissueAtomicTxHigherGasPrice(t *testing.T) {
	kc := secp256k1fx.NewKeychain(testKeys...)

	for name, issueTxs := range map[string]func(t *testing.T, vm *VM, sharedMemory *atomic.Memory) (issued []*Tx, discarded []*Tx){
		"single UTXO override": func(t *testing.T, vm *VM, sharedMemory *atomic.Memory) (issued []*Tx, evicted []*Tx) {
			utxo, err := addUTXO(sharedMemory, vm.ctx, ids.GenerateTestID(), 0, vm.ctx.AVAXAssetID, units.Avax, testShortIDAddrs[0])
			if err != nil {
				t.Fatal(err)
			}
			tx1, err := vm.newImportTxWithUTXOs(vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, kc, []*avax.UTXO{utxo})
			if err != nil {
				t.Fatal(err)
			}
			tx2, err := vm.newImportTxWithUTXOs(vm.ctx.XChainID, testEthAddrs[0], new(big.Int).Mul(common.Big2, initialBaseFee), kc, []*avax.UTXO{utxo})
			if err != nil {
				t.Fatal(err)
			}

			if err := vm.issueTx(tx1, true); err != nil {
				t.Fatal(err)
			}
			if err := vm.issueTx(tx2, true); err != nil {
				t.Fatal(err)
			}

			return []*Tx{tx2}, []*Tx{tx1}
		},
		"one of two UTXOs overrides": func(t *testing.T, vm *VM, sharedMemory *atomic.Memory) (issued []*Tx, evicted []*Tx) {
			utxo1, err := addUTXO(sharedMemory, vm.ctx, ids.GenerateTestID(), 0, vm.ctx.AVAXAssetID, units.Avax, testShortIDAddrs[0])
			if err != nil {
				t.Fatal(err)
			}
			utxo2, err := addUTXO(sharedMemory, vm.ctx, ids.GenerateTestID(), 0, vm.ctx.AVAXAssetID, units.Avax, testShortIDAddrs[0])
			if err != nil {
				t.Fatal(err)
			}
			tx1, err := vm.newImportTxWithUTXOs(vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, kc, []*avax.UTXO{utxo1, utxo2})
			if err != nil {
				t.Fatal(err)
			}
			tx2, err := vm.newImportTxWithUTXOs(vm.ctx.XChainID, testEthAddrs[0], new(big.Int).Mul(common.Big2, initialBaseFee), kc, []*avax.UTXO{utxo1})
			if err != nil {
				t.Fatal(err)
			}

			if err := vm.issueTx(tx1, true); err != nil {
				t.Fatal(err)
			}
			if err := vm.issueTx(tx2, true); err != nil {
				t.Fatal(err)
			}

			return []*Tx{tx2}, []*Tx{tx1}
		},
		"hola": func(t *testing.T, vm *VM, sharedMemory *atomic.Memory) (issued []*Tx, evicted []*Tx) {
			utxo1, err := addUTXO(sharedMemory, vm.ctx, ids.GenerateTestID(), 0, vm.ctx.AVAXAssetID, units.Avax, testShortIDAddrs[0])
			if err != nil {
				t.Fatal(err)
			}
			utxo2, err := addUTXO(sharedMemory, vm.ctx, ids.GenerateTestID(), 0, vm.ctx.AVAXAssetID, units.Avax, testShortIDAddrs[0])
			if err != nil {
				t.Fatal(err)
			}

			importTx1, err := vm.newImportTxWithUTXOs(vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, kc, []*avax.UTXO{utxo1})
			if err != nil {
				t.Fatal(err)
			}

			importTx2, err := vm.newImportTxWithUTXOs(vm.ctx.XChainID, testEthAddrs[0], new(big.Int).Mul(big.NewInt(3), initialBaseFee), kc, []*avax.UTXO{utxo2})
			if err != nil {
				t.Fatal(err)
			}

			reissuanceTx1, err := vm.newImportTxWithUTXOs(vm.ctx.XChainID, testEthAddrs[0], new(big.Int).Mul(big.NewInt(2), initialBaseFee), kc, []*avax.UTXO{utxo1, utxo2})
			if err != nil {
				t.Fatal(err)
			}
			if err := vm.issueTx(importTx1, true /*=local*/); err != nil {
				t.Fatal(err)
			}

			if err := vm.issueTx(importTx2, true /*=local*/); err != nil {
				t.Fatal(err)
			}

			if err := vm.issueTx(reissuanceTx1, true /*=local*/); !errors.Is(err, errConflictingAtomicTx) {
				t.Fatalf("Expected to fail with err: %s, but found err: %s", errConflictingAtomicTx, err)
			}

			assert.True(t, vm.mempool.has(importTx1.ID()))
			assert.True(t, vm.mempool.has(importTx2.ID()))
			assert.False(t, vm.mempool.has(reissuanceTx1.ID()))

			reissuanceTx2, err := vm.newImportTxWithUTXOs(vm.ctx.XChainID, testEthAddrs[0], new(big.Int).Mul(big.NewInt(4), initialBaseFee), kc, []*avax.UTXO{utxo1, utxo2})
			if err != nil {
				t.Fatal(err)
			}
			if err := vm.issueTx(reissuanceTx2, true /*=local*/); err != nil {
				t.Fatal(err)
			}

			return []*Tx{reissuanceTx2}, []*Tx{importTx1, importTx2}
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, vm, _, sharedMemory, _ := GenesisVM(t, true, genesisJSONApricotPhase5, "", "")
			issuedTxs, evictedTxs := issueTxs(t, vm, sharedMemory)

			for i, tx := range issuedTxs {
				_, issued := vm.mempool.txHeap.Get(tx.ID())
				assert.True(t, issued, "expected issued tx at index %d to be issued", i)
			}

			for i, tx := range evictedTxs {
				_, discarded := vm.mempool.discardedTxs.Get(tx.ID())
				assert.True(t, discarded, "expected discarded tx at index %d to be discarded", i)
			}
		})
	}
}

func TestConflictingImportTxsAcrossBlocks(t *testing.T) {
	for name, genesis := range map[string]string{
		"apricotPhase1": genesisJSONApricotPhase1,
		"apricotPhase2": genesisJSONApricotPhase2,
		"apricotPhase3": genesisJSONApricotPhase3,
		"apricotPhase4": genesisJSONApricotPhase4,
		"apricotPhase5": genesisJSONApricotPhase5,
	} {
		t.Run(name, func(t *testing.T) {
			testConflictingImportTxs(t, genesis)
		})
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
	importAmount := uint64(1000000000)
	issuer1, vm1, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, "{\"pruning-enabled\":true}", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})
	issuer2, vm2, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, "{\"pruning-enabled\":true}", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})

	defer func() {
		if err := vm1.Shutdown(); err != nil {
			t.Fatal(err)
		}

		if err := vm2.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan2)

	importTx, err := vm1.newImportTx(vm1.ctx.XChainID, testEthAddrs[1], initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1.issueTx(importTx, true /*=local*/); err != nil {
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

	newHead := <-newTxPoolHeadChan1
	if newHead.Head.Hash() != common.Hash(vm1BlkA.ID()) {
		t.Fatalf("Expected new block to match")
	}
	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkA.ID()) {
		t.Fatalf("Expected new block to match")
	}

	// Create list of 10 successive transactions to build block A on vm1
	// and to be split into two separate blocks on VM2
	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), testEthAddrs[1], big.NewInt(10), 21000, big.NewInt(params.LaunchMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainID), testKeys[1].ToECDSA())
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	var errs []error

	// Add the remote transactions, build the block, and set VM1's preference for block A
	errs = vm1.chain.AddRemoteTxsSync(txs)
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
	errs = vm2.chain.AddRemoteTxsSync(txs[0:5])
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

	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkC.ID()) {
		t.Fatalf("Expected new block to match")
	}

	// Block D
	errs = vm2.chain.AddRemoteTxsSync(txs[5:10])
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
	key, err := accountKeystore.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	key0 := testKeys[0]
	addr0 := key0.PublicKey().Address()

	key1 := testKeys[1]
	addr1 := key1.PublicKey().Address()

	importAmount := uint64(1000000000)

	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, "", "",
		map[ids.ShortID]uint64{
			addr0: importAmount,
			addr1: importAmount,
		})

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan)

	importTx0A, err := vm.newImportTx(vm.ctx.XChainID, key.Address, initialBaseFee, []*crypto.PrivateKeySECP256K1R{key0})
	if err != nil {
		t.Fatal(err)
	}
	// Create a conflicting transaction
	importTx0B, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[2], initialBaseFee, []*crypto.PrivateKeySECP256K1R{key0})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.issueTx(importTx0A, true /*=local*/); err != nil {
		t.Fatalf("Failed to issue importTx0A: %s", err)
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

	newHead := <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk0.ID()) {
		t.Fatalf("Expected new block to match")
	}

	tx := types.NewTransaction(0, key.Address, big.NewInt(10), 21000, big.NewInt(params.LaunchMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainID), key.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}

	// Add the remote transactions, build the block, and set VM1's preference for block A
	errs := vm.chain.AddRemoteTxsSync([]*types.Transaction{signedTx})
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Failed to add transaction to VM1 at index %d: %s", i, err)
		}
	}

	<-issuer

	blk1, err := vm.BuildBlock()
	if err != nil {
		t.Fatalf("Failed to build blk1: %s", err)
	}

	if err := blk1.Verify(); err != nil {
		t.Fatalf("blk1 failed verification due to %s", err)
	}

	if err := vm.SetPreference(blk1.ID()); err != nil {
		t.Fatal(err)
	}

	importTx1, err := vm.newImportTx(vm.ctx.XChainID, key.Address, initialBaseFee, []*crypto.PrivateKeySECP256K1R{key1})
	if err != nil {
		t.Fatalf("Failed to issue importTx1 due to: %s", err)
	}

	if err := vm.issueTx(importTx1, true /*=local*/); err != nil {
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

	if err := vm.issueTx(importTx0B, true /*=local*/); err == nil {
		t.Fatalf("Should not have been able to issue import tx with conflict")
	}
	// Force issue transaction directly into the mempool
	if err := vm.mempool.ForceAddTx(importTx0B); err != nil {
		t.Fatal(err)
	}
	<-issuer

	_, err = vm.BuildBlock()
	if err == nil {
		t.Fatal("Shouldn't have been able to build an invalid block")
	}
}

func TestBonusBlocksTxs(t *testing.T) {
	issuer, vm, _, sharedMemory, _ := GenesisVM(t, true, genesisJSONApricotPhase0, "", "")

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	importAmount := uint64(10000000)
	utxoID := avax.UTXOID{TxID: ids.GenerateTestID()}

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
	if err := xChainSharedMemory.Apply(map[ids.ID]*atomic.Requests{vm.ctx.ChainID: {PutRequests: []*atomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			testKeys[0].PublicKey().Address().Bytes(),
		},
	}}}}); err != nil {
		t.Fatal(err)
	}

	importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.issueTx(importTx, true /*=local*/); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blk, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	bonusBlocks.Add(blk.ID())

	// Remove the UTXOs from shared memory, so that non-bonus blocks will fail verification
	if err := vm.ctx.SharedMemory.Apply(map[ids.ID]*atomic.Requests{vm.ctx.XChainID: {RemoveRequests: [][]byte{inputID[:]}}}); err != nil {
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
}

// Regression test to ensure that a VM that accepts block A and B
// will not attempt to orphan either when verifying blocks C and D
// from another VM (which have a common ancestor under the finalized
// frontier).
//   A
//  / \
// B   C
//
// verifies block B and C, then Accepts block B. Then we test to ensure
// that the VM defends against any attempt to set the preference or to
// accept block C, which should be an orphaned block at this point and
// get rejected.
func TestReorgProtection(t *testing.T) {
	importAmount := uint64(1000000000)
	issuer1, vm1, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, "{\"pruning-enabled\":false}", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})
	issuer2, vm2, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, "{\"pruning-enabled\":false}", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})

	defer func() {
		if err := vm1.Shutdown(); err != nil {
			t.Fatal(err)
		}

		if err := vm2.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan2)

	key := testKeys[0].ToECDSA()
	address := testEthAddrs[0]

	importTx, err := vm1.newImportTx(vm1.ctx.XChainID, address, initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1.issueTx(importTx, true /*=local*/); err != nil {
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

	newHead := <-newTxPoolHeadChan1
	if newHead.Head.Hash() != common.Hash(vm1BlkA.ID()) {
		t.Fatalf("Expected new block to match")
	}
	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkA.ID()) {
		t.Fatalf("Expected new block to match")
	}

	// Create list of 10 successive transactions to build block A on vm1
	// and to be split into two separate blocks on VM2
	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(params.LaunchMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainID), key)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	var errs []error

	// Add the remote transactions, build the block, and set VM1's preference for block A
	errs = vm1.chain.AddRemoteTxsSync(txs)
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
	errs = vm2.chain.AddRemoteTxsSync(txs[0:5])
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

	// Accept B, such that block C should get Rejected.
	if err := vm1BlkB.Accept(); err != nil {
		t.Fatalf("VM1 failed to accept block: %s", err)
	}

	// The below (setting preference blocks that have a common ancestor
	// with the preferred chain lower than the last finalized block)
	// should NEVER happen. However, the VM defends against this
	// just in case.
	if err := vm1.SetPreference(vm1BlkC.ID()); !strings.Contains(err.Error(), "cannot orphan finalized block") {
		t.Fatalf("Unexpected error when setting preference that would trigger reorg: %s", err)
	}

	if err := vm1BlkC.Accept(); !strings.Contains(err.Error(), "expected accepted block to have parent") {
		t.Fatalf("Unexpected error when setting block at finalized height: %s", err)
	}
}

// Regression test to ensure that a VM that accepts block C while preferring
// block B will trigger a reorg.
//   A
//  / \
// B   C
func TestNonCanonicalAccept(t *testing.T) {
	importAmount := uint64(1000000000)
	issuer1, vm1, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})
	issuer2, vm2, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})

	defer func() {
		if err := vm1.Shutdown(); err != nil {
			t.Fatal(err)
		}

		if err := vm2.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan2)

	key := testKeys[0].ToECDSA()
	address := testEthAddrs[0]

	importTx, err := vm1.newImportTx(vm1.ctx.XChainID, address, initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1.issueTx(importTx, true /*=local*/); err != nil {
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

	newHead := <-newTxPoolHeadChan1
	if newHead.Head.Hash() != common.Hash(vm1BlkA.ID()) {
		t.Fatalf("Expected new block to match")
	}
	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkA.ID()) {
		t.Fatalf("Expected new block to match")
	}

	// Create list of 10 successive transactions to build block A on vm1
	// and to be split into two separate blocks on VM2
	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(params.LaunchMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainID), key)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	var errs []error

	// Add the remote transactions, build the block, and set VM1's preference for block A
	errs = vm1.chain.AddRemoteTxsSync(txs)
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
	blkBHash := vm1BlkB.(*chain.BlockWrapper).Block.(*Block).ethBlock.Hash()
	if b := vm1.chain.GetBlockByNumber(blkBHeight); b.Hash() != blkBHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkBHash.Hex(), b.Hash().Hex())
	}

	errs = vm2.chain.AddRemoteTxsSync(txs[0:5])
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

	blkCHash := vm1BlkC.(*chain.BlockWrapper).Block.(*Block).ethBlock.Hash()
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
	importAmount := uint64(1000000000)
	issuer1, vm1, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})
	issuer2, vm2, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})

	defer func() {
		if err := vm1.Shutdown(); err != nil {
			t.Fatal(err)
		}

		if err := vm2.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan2)

	key := testKeys[0].ToECDSA()
	address := testEthAddrs[0]

	importTx, err := vm1.newImportTx(vm1.ctx.XChainID, address, initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1.issueTx(importTx, true /*=local*/); err != nil {
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

	newHead := <-newTxPoolHeadChan1
	if newHead.Head.Hash() != common.Hash(vm1BlkA.ID()) {
		t.Fatalf("Expected new block to match")
	}
	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkA.ID()) {
		t.Fatalf("Expected new block to match")
	}

	// Create list of 10 successive transactions to build block A on vm1
	// and to be split into two separate blocks on VM2
	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(params.LaunchMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainID), key)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	var errs []error

	// Add the remote transactions, build the block, and set VM1's preference for block A
	errs = vm1.chain.AddRemoteTxsSync(txs)
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
	blkBHash := vm1BlkB.(*chain.BlockWrapper).Block.(*Block).ethBlock.Hash()
	if b := vm1.chain.GetBlockByNumber(blkBHeight); b.Hash() != blkBHash {
		t.Fatalf("expected block at %d to have hash %s but got %s", blkBHeight, blkBHash.Hex(), b.Hash().Hex())
	}

	errs = vm2.chain.AddRemoteTxsSync(txs[0:5])
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

	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkC.ID()) {
		t.Fatalf("Expected new block to match")
	}

	errs = vm2.chain.AddRemoteTxsSync(txs[5:])
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
	blkCHash := vm1BlkC.(*chain.BlockWrapper).Block.(*Block).ethBlock.Hash()

	vm1BlkD, err := vm1.ParseBlock(vm2BlkD.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing block from vm2: %s", err)
	}
	blkDHeight := vm1BlkD.Height()
	blkDHash := vm1BlkD.(*chain.BlockWrapper).Block.(*Block).ethBlock.Hash()

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
	if err := vm1BlkD.Accept(); !strings.Contains(err.Error(), "expected accepted block to have parent") {
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
	importAmount := uint64(1000000000)
	issuer1, vm1, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})
	issuer2, vm2, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})

	defer func() {
		if err := vm1.Shutdown(); err != nil {
			t.Fatal(err)
		}
		if err := vm2.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan2)

	key := testKeys[0].ToECDSA()
	address := testEthAddrs[0]

	importTx, err := vm1.newImportTx(vm1.ctx.XChainID, address, initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1.issueTx(importTx, true /*=local*/); err != nil {
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

	newHead := <-newTxPoolHeadChan1
	if newHead.Head.Hash() != common.Hash(vm1BlkA.ID()) {
		t.Fatalf("Expected new block to match")
	}
	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkA.ID()) {
		t.Fatalf("Expected new block to match")
	}

	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(params.LaunchMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainID), key)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	var errs []error

	errs = vm1.chain.AddRemoteTxsSync(txs)
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

	errs = vm2.chain.AddRemoteTxsSync(txs[0:5])
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

	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkC.ID()) {
		t.Fatalf("Expected new block to match")
	}

	errs = vm2.chain.AddRemoteTxsSync(txs[5:10])
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
	blkDEthBlock := vm2BlkD.(*chain.BlockWrapper).Block.(*Block).ethBlock
	uncles := []*types.Header{vm1BlkB.(*chain.BlockWrapper).Block.(*Block).ethBlock.Header()}
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
	importAmount := uint64(1000000000)
	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.issueTx(importTx, true /*=local*/); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blk, err := vm.BuildBlock()
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	// Create empty block from blkA
	ethBlock := blk.(*chain.BlockWrapper).Block.(*Block).ethBlock

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
	importAmount := uint64(1000000000)
	issuer1, vm1, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})
	issuer2, vm2, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})

	defer func() {
		if err := vm1.Shutdown(); err != nil {
			t.Fatal(err)
		}

		if err := vm2.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan1 := make(chan core.NewTxPoolReorgEvent, 1)
	vm1.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan1)
	newTxPoolHeadChan2 := make(chan core.NewTxPoolReorgEvent, 1)
	vm2.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan2)

	key := testKeys[0].ToECDSA()
	address := testEthAddrs[0]

	importTx, err := vm1.newImportTx(vm1.ctx.XChainID, address, initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm1.issueTx(importTx, true /*=local*/); err != nil {
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

	newHead := <-newTxPoolHeadChan1
	if newHead.Head.Hash() != common.Hash(vm1BlkA.ID()) {
		t.Fatalf("Expected new block to match")
	}
	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkA.ID()) {
		t.Fatalf("Expected new block to match")
	}

	// Create list of 10 successive transactions to build block A on vm1
	// and to be split into two separate blocks on VM2
	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(params.LaunchMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainID), key)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}

	// Add the remote transactions, build the block, and set VM1's preference
	// for block B
	errs := vm1.chain.AddRemoteTxsSync(txs)
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

	errs = vm2.chain.AddRemoteTxsSync(txs[0:5])
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

	newHead = <-newTxPoolHeadChan2
	if newHead.Head.Hash() != common.Hash(vm2BlkC.ID()) {
		t.Fatalf("Expected new block to match")
	}

	errs = vm2.chain.AddRemoteTxsSync(txs[5:])
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

	blkBHash := vm1BlkB.(*chain.BlockWrapper).Block.(*Block).ethBlock.Hash()
	if b := vm1.chain.BlockChain().CurrentBlock(); b.Hash() != blkBHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkBHash.Hex(), b.Hash().Hex())
	}

	if err := vm1BlkC.Accept(); err != nil {
		t.Fatal(err)
	}

	blkCHash := vm1BlkC.(*chain.BlockWrapper).Block.(*Block).ethBlock.Hash()
	if b := vm1.chain.BlockChain().CurrentBlock(); b.Hash() != blkCHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkCHash.Hex(), b.Hash().Hex())
	}
	if err := vm1BlkB.Reject(); err != nil {
		t.Fatal(err)
	}

	if err := vm1BlkD.Accept(); err != nil {
		t.Fatal(err)
	}
	blkDHash := vm1BlkD.(*chain.BlockWrapper).Block.(*Block).ethBlock.Hash()
	if b := vm1.chain.BlockChain().CurrentBlock(); b.Hash() != blkDHash {
		t.Fatalf("expected current block to have hash %s but got %s", blkDHash.Hex(), b.Hash().Hex())
	}
}

func TestFutureBlock(t *testing.T) {
	importAmount := uint64(1000000000)
	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.issueTx(importTx, true /*=local*/); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blkA, err := vm.BuildBlock()
	if err != nil {
		t.Fatalf("Failed to build block with import transaction: %s", err)
	}

	// Create empty block from blkA
	internalBlkA := blkA.(*chain.BlockWrapper).Block.(*Block)
	modifiedHeader := types.CopyHeader(internalBlkA.ethBlock.Header())
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
		internalBlkA.ethBlock.ExtData(),
		false,
	)

	futureBlock := &Block{
		vm:        vm,
		ethBlock:  modifiedBlock,
		id:        ids.ID(modifiedBlock.Hash()),
		atomicTxs: internalBlkA.atomicTxs,
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
	importAmount := uint64(1000000000)
	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase1, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan)

	key := testKeys[0].ToECDSA()
	address := testEthAddrs[0]

	importTx, err := vm.newImportTx(vm.ctx.XChainID, address, initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.issueTx(importTx, true /*=local*/); err != nil {
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

	newHead := <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk.ID()) {
		t.Fatalf("Expected new block to match")
	}

	txs := make([]*types.Transaction, 10)
	for i := 0; i < 5; i++ {
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(params.LaunchMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainID), key)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}
	for i := 5; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(params.ApricotPhase1MinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainID), key)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}
	errs := vm.chain.AddRemoteTxsSync(txs)
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

func TestLastAcceptedBlockNumberAllow(t *testing.T) {
	importAmount := uint64(1000000000)
	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.issueTx(importTx, true /*=local*/); err != nil {
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
	blkHash := blk.(*chain.BlockWrapper).Block.(*Block).ethBlock.Hash()

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

// Builds [blkA] with a virtuous import transaction and [blkB] with a separate import transaction
// that does not conflict. Accepts [blkB] and rejects [blkA], then asserts that the virtuous atomic
// transaction in [blkA] is correctly re-issued into the atomic transaction mempool.
func TestReissueAtomicTx(t *testing.T) {
	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase1, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: 10000000,
		testShortIDAddrs[1]: 10000000,
	})

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	genesisBlkID, err := vm.LastAccepted()
	if err != nil {
		t.Fatal(err)
	}

	importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.issueTx(importTx, true /*=local*/); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blkA, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if status := blkA.Status(); status != choices.Processing {
		t.Fatalf("Expected status of built block to be %s, but found %s", choices.Processing, status)
	}

	if err := vm.SetPreference(blkA.ID()); err != nil {
		t.Fatal(err)
	}

	// SetPreference to parent before rejecting (will rollback state to genesis
	// so that atomic transaction can be reissued, otherwise current block will
	// conflict with UTXO to be reissued)
	if err := vm.SetPreference(genesisBlkID); err != nil {
		t.Fatal(err)
	}

	// Rejecting [blkA] should cause [importTx] to be re-issued into the mempool.
	if err := blkA.Reject(); err != nil {
		t.Fatal(err)
	}

	// Sleep for a minimum of two seconds to ensure that [blkB] will have a different timestamp
	// than [blkA] so that the block will be unique. This is necessary since we have marked [blkA]
	// as Rejected.
	time.Sleep(2 * time.Second)
	<-issuer
	blkB, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if blkB.Height() != blkA.Height() {
		t.Fatalf("Expected blkB (%d) to have the same height as blkA (%d)", blkB.Height(), blkA.Height())
	}
	if status := blkA.Status(); status != choices.Rejected {
		t.Fatalf("Expected status of blkA to be %s, but found %s", choices.Rejected, status)
	}
	if status := blkB.Status(); status != choices.Processing {
		t.Fatalf("Expected status of blkB to be %s, but found %s", choices.Processing, status)
	}

	if err := blkB.Verify(); err != nil {
		t.Fatal(err)
	}

	if status := blkB.Status(); status != choices.Processing {
		t.Fatalf("Expected status of blkC to be %s, but found %s", choices.Processing, status)
	}

	if err := vm.SetPreference(blkB.ID()); err != nil {
		t.Fatal(err)
	}

	if err := blkB.Accept(); err != nil {
		t.Fatal(err)
	}

	if status := blkB.Status(); status != choices.Accepted {
		t.Fatalf("Expected status of accepted block to be %s, but found %s", choices.Accepted, status)
	}

	if lastAcceptedID, err := vm.LastAccepted(); err != nil {
		t.Fatal(err)
	} else if lastAcceptedID != blkB.ID() {
		t.Fatalf("Expected last accepted blockID to be the accepted block: %s, but found %s", blkB.ID(), lastAcceptedID)
	}

	// Check that [importTx] has been indexed correctly after [blkB] is accepted.
	_, height, err := vm.atomicTxRepository.GetByTxID(importTx.ID())
	if err != nil {
		t.Fatal(err)
	} else if height != blkB.Height() {
		t.Fatalf("Expected indexed height of import tx to be %d, but found %d", blkB.Height(), height)
	}
}

func TestAtomicTxFailsEVMStateTransferBuildBlock(t *testing.T) {
	issuer, vm, _, sharedMemory, _ := GenesisVM(t, true, genesisJSONApricotPhase1, "", "")

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	exportTxs := createExportTxOptions(t, vm, issuer, sharedMemory)
	exportTx1, exportTx2 := exportTxs[0], exportTxs[1]

	if err := vm.issueTx(exportTx1, true /*=local*/); err != nil {
		t.Fatal(err)
	}
	<-issuer
	exportBlk1, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := exportBlk1.Verify(); err != nil {
		t.Fatal(err)
	}

	if err := vm.SetPreference(exportBlk1.ID()); err != nil {
		t.Fatal(err)
	}

	if err := vm.issueTx(exportTx2, true /*=local*/); err == nil {
		t.Fatal("Should have failed to issue due to an invalid export tx")
	}

	if err := vm.mempool.AddTx(exportTx2); err == nil {
		t.Fatal("Should have failed to add because conflicting")
	}

	// Manually add transaction to mempool to bypass validation
	if err := vm.mempool.ForceAddTx(exportTx2); err != nil {
		t.Fatal(err)
	}
	<-issuer

	_, err = vm.BuildBlock()
	if err == nil {
		t.Fatal("BuildBlock should have returned an error due to invalid export transaction")
	}
}

func TestBuildInvalidBlockHead(t *testing.T) {
	issuer, vm, _, _, _ := GenesisVM(t, true, genesisJSONApricotPhase0, "", "")

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

	// Verify that the transaction fails verification when attempting to issue
	// it into the atomic mempool.
	if err := vm.issueTx(tx, true /*=local*/); err == nil {
		t.Fatal("Should have failed to issue invalid transaction")
	}
	// Force issue the transaction directly to the mempool
	if err := vm.mempool.AddTx(tx); err != nil {
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

func TestConfigureLogLevel(t *testing.T) {
	configTests := []struct {
		name                     string
		logConfig                string
		genesisJSON, upgradeJSON string
		expectedErr              string
	}{
		{
			name:        "Log level info",
			logConfig:   "{\"log-level\": \"info\"}",
			genesisJSON: genesisJSONApricotPhase2,
			upgradeJSON: "",
			expectedErr: "",
		},
		{
			name:        "Invalid log level",
			logConfig:   "{\"log-level\": \"cchain\"}",
			genesisJSON: genesisJSONApricotPhase3,
			upgradeJSON: "",
			expectedErr: "failed to initialize logger due to",
		},
	}
	for _, test := range configTests {
		t.Run(test.name, func(t *testing.T) {
			vm := &VM{}
			ctx, dbManager, genesisBytes, issuer, _ := setupGenesis(t, test.genesisJSON)
			appSender := &engCommon.SenderTest{T: t}
			appSender.CantSendAppGossip = true
			appSender.SendAppGossipF = func([]byte) error { return nil }
			err := vm.Initialize(
				ctx,
				dbManager,
				genesisBytes,
				[]byte(""),
				[]byte(test.logConfig),
				issuer,
				[]*engCommon.Fx{},
				appSender,
			)
			if len(test.expectedErr) == 0 && err != nil {
				t.Fatal(err)
			} else if len(test.expectedErr) > 0 {
				if err == nil {
					t.Fatalf("initialize should have failed due to %s", test.expectedErr)
				} else if !strings.Contains(err.Error(), test.expectedErr) {
					t.Fatalf("Expected initialize to fail due to %s, but failed with %s", test.expectedErr, err.Error())
				}
			}

			// If the VM was not initialized, do not attept to shut it down
			if err == nil {
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
		})
	}
}

// Regression test to ensure we can build blocks if we are starting with the
// Apricot Phase 4 ruleset in genesis.
func TestBuildApricotPhase4Block(t *testing.T) {
	issuer, vm, _, sharedMemory, _ := GenesisVM(t, true, genesisJSONApricotPhase4, "", "")

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan)

	key := testKeys[0].ToECDSA()
	address := testEthAddrs[0]

	importAmount := uint64(1000000000)
	utxoID := avax.UTXOID{TxID: ids.GenerateTestID()}

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
	if err := xChainSharedMemory.Apply(map[ids.ID]*atomic.Requests{vm.ctx.ChainID: {PutRequests: []*atomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			testKeys[0].PublicKey().Address().Bytes(),
		},
	}}}}); err != nil {
		t.Fatal(err)
	}

	importTx, err := vm.newImportTx(vm.ctx.XChainID, address, initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.issueTx(importTx, true /*=local*/); err != nil {
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

	ethBlk := blk.(*chain.BlockWrapper).Block.(*Block).ethBlock
	if eBlockGasCost := ethBlk.BlockGasCost(); eBlockGasCost == nil || eBlockGasCost.Cmp(common.Big0) != 0 {
		t.Fatalf("expected blockGasCost to be 0 but got %d", eBlockGasCost)
	}
	if eExtDataGasUsed := ethBlk.ExtDataGasUsed(); eExtDataGasUsed == nil || eExtDataGasUsed.Cmp(big.NewInt(1230)) != 0 {
		t.Fatalf("expected extDataGasUsed to be 1000 but got %d", eExtDataGasUsed)
	}
	minRequiredTip, err := dummy.MinRequiredTip(vm.chainConfig, ethBlk.Header())
	if err != nil {
		t.Fatal(err)
	}
	if minRequiredTip == nil || minRequiredTip.Cmp(common.Big0) != 0 {
		t.Fatalf("expected minRequiredTip to be 0 but got %d", minRequiredTip)
	}

	newHead := <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk.ID()) {
		t.Fatalf("Expected new block to match")
	}

	txs := make([]*types.Transaction, 10)
	for i := 0; i < 5; i++ {
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(params.LaunchMinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainID), key)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = signedTx
	}
	for i := 5; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(params.ApricotPhase1MinGasPrice), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainID), key)
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

	ethBlk = blk.(*chain.BlockWrapper).Block.(*Block).ethBlock
	if ethBlk.BlockGasCost() == nil || ethBlk.BlockGasCost().Cmp(big.NewInt(100)) < 0 {
		t.Fatalf("expected blockGasCost to be at least 100 but got %d", ethBlk.BlockGasCost())
	}
	if ethBlk.ExtDataGasUsed() == nil || ethBlk.ExtDataGasUsed().Cmp(common.Big0) != 0 {
		t.Fatalf("expected extDataGasUsed to be 0 but got %d", ethBlk.ExtDataGasUsed())
	}
	minRequiredTip, err = dummy.MinRequiredTip(vm.chainConfig, ethBlk.Header())
	if err != nil {
		t.Fatal(err)
	}
	if minRequiredTip == nil || minRequiredTip.Cmp(big.NewInt(0.05*params.GWei)) < 0 {
		t.Fatalf("expected minRequiredTip to be at least 0.05 gwei but got %d", minRequiredTip)
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

// Regression test to ensure we can build blocks if we are starting with the
// Apricot Phase 5 ruleset in genesis.
func TestBuildApricotPhase5Block(t *testing.T) {
	issuer, vm, _, sharedMemory, _ := GenesisVM(t, true, genesisJSONApricotPhase5, "", "")

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan)

	key := testKeys[0].ToECDSA()
	address := testEthAddrs[0]

	importAmount := uint64(1000000000)
	utxoID := avax.UTXOID{TxID: ids.GenerateTestID()}

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
	if err := xChainSharedMemory.Apply(map[ids.ID]*atomic.Requests{vm.ctx.ChainID: {PutRequests: []*atomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			testKeys[0].PublicKey().Address().Bytes(),
		},
	}}}}); err != nil {
		t.Fatal(err)
	}

	importTx, err := vm.newImportTx(vm.ctx.XChainID, address, initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.issueTx(importTx, true /*=local*/); err != nil {
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

	ethBlk := blk.(*chain.BlockWrapper).Block.(*Block).ethBlock
	if eBlockGasCost := ethBlk.BlockGasCost(); eBlockGasCost == nil || eBlockGasCost.Cmp(common.Big0) != 0 {
		t.Fatalf("expected blockGasCost to be 0 but got %d", eBlockGasCost)
	}
	if eExtDataGasUsed := ethBlk.ExtDataGasUsed(); eExtDataGasUsed == nil || eExtDataGasUsed.Cmp(big.NewInt(11230)) != 0 {
		t.Fatalf("expected extDataGasUsed to be 11230 but got %d", eExtDataGasUsed)
	}
	minRequiredTip, err := dummy.MinRequiredTip(vm.chainConfig, ethBlk.Header())
	if err != nil {
		t.Fatal(err)
	}
	if minRequiredTip == nil || minRequiredTip.Cmp(common.Big0) != 0 {
		t.Fatalf("expected minRequiredTip to be 0 but got %d", minRequiredTip)
	}

	newHead := <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk.ID()) {
		t.Fatalf("Expected new block to match")
	}

	txs := make([]*types.Transaction, 10)
	for i := 0; i < 10; i++ {
		tx := types.NewTransaction(uint64(i), address, big.NewInt(10), 21000, big.NewInt(params.LaunchMinGasPrice*3), nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainID), key)
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

	ethBlk = blk.(*chain.BlockWrapper).Block.(*Block).ethBlock
	if ethBlk.BlockGasCost() == nil || ethBlk.BlockGasCost().Cmp(big.NewInt(100)) < 0 {
		t.Fatalf("expected blockGasCost to be at least 100 but got %d", ethBlk.BlockGasCost())
	}
	if ethBlk.ExtDataGasUsed() == nil || ethBlk.ExtDataGasUsed().Cmp(common.Big0) != 0 {
		t.Fatalf("expected extDataGasUsed to be 0 but got %d", ethBlk.ExtDataGasUsed())
	}
	minRequiredTip, err = dummy.MinRequiredTip(vm.chainConfig, ethBlk.Header())
	if err != nil {
		t.Fatal(err)
	}
	if minRequiredTip == nil || minRequiredTip.Cmp(big.NewInt(0.05*params.GWei)) < 0 {
		t.Fatalf("expected minRequiredTip to be at least 0.05 gwei but got %d", minRequiredTip)
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

// This is a regression test to ensure that if two consecutive atomic transactions fail verification
// in onFinalizeAndAssemble it will not cause a panic due to calling RevertToSnapshot(revID) on the
// same revision ID twice.
func TestConsecutiveAtomicTransactionsRevertSnapshot(t *testing.T) {
	issuer, vm, _, sharedMemory, _ := GenesisVM(t, true, genesisJSONApricotPhase1, "", "")

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	newTxPoolHeadChan := make(chan core.NewTxPoolReorgEvent, 1)
	vm.chain.GetTxPool().SubscribeNewReorgEvent(newTxPoolHeadChan)

	// Create three conflicting import transactions
	importTxs := createImportTxOptions(t, vm, sharedMemory)

	// Issue the first import transaction, build, and accept the block.
	if err := vm.issueTx(importTxs[0], true); err != nil {
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

	newHead := <-newTxPoolHeadChan
	if newHead.Head.Hash() != common.Hash(blk.ID()) {
		t.Fatalf("Expected new block to match")
	}

	// Add the two conflicting transactions directly to the mempool, so that two consecutive transactions
	// will fail verification when build block is called.
	vm.mempool.AddTx(importTxs[1])
	vm.mempool.AddTx(importTxs[2])

	if _, err := vm.BuildBlock(); err == nil {
		t.Fatal("Expected build block to fail due to empty block")
	}
}

func TestAtomicTxBuildBlockDropsConflicts(t *testing.T) {
	importAmount := uint64(10000000)
	issuer, vm, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase5, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
		testShortIDAddrs[1]: importAmount,
		testShortIDAddrs[2]: importAmount,
	})
	conflictKey, err := accountKeystore.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	// Create a conflict set for each pair of transactions
	conflictSets := make([]ids.Set, len(testKeys))
	for index, key := range testKeys {
		importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[index], initialBaseFee, []*crypto.PrivateKeySECP256K1R{key})
		if err != nil {
			t.Fatal(err)
		}
		if err := vm.issueTx(importTx, true /*=local*/); err != nil {
			t.Fatal(err)
		}
		conflictSets[index].Add(importTx.ID())
		conflictTx, err := vm.newImportTx(vm.ctx.XChainID, conflictKey.Address, initialBaseFee, []*crypto.PrivateKeySECP256K1R{key})
		if err != nil {
			t.Fatal(err)
		}
		if err := vm.issueTx(conflictTx, true /*=local*/); err == nil {
			t.Fatal("should conflict with the utxoSet in the mempool")
		}
		// force add the tx
		vm.mempool.ForceAddTx(conflictTx)
		conflictSets[index].Add(conflictTx.ID())
	}
	<-issuer
	// Note: this only checks the path through OnFinalizeAndAssemble, we should make sure to add a test
	// that verifies blocks received from the network will also fail verification
	blk, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}
	atomicTxs := blk.(*chain.BlockWrapper).Block.(*Block).atomicTxs
	assert.True(t, len(atomicTxs) == len(testKeys), "Conflict transactions should be out of the batch")
	atomicTxIDs := ids.Set{}
	for _, tx := range atomicTxs {
		atomicTxIDs.Add(tx.ID())
	}

	// Check that removing the txIDs actually included in the block from each conflict set
	// leaves one item remaining for each conflict set ie. only one tx from each conflict set
	// has been included in the block.
	for _, conflictSet := range conflictSets {
		conflictSet.Difference(atomicTxIDs)
		assert.Equal(t, 1, conflictSet.Len())
	}

	if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := blk.Accept(); err != nil {
		t.Fatal(err)
	}
}

func TestBuildBlockDoesNotExceedAtomicGasLimit(t *testing.T) {
	importAmount := uint64(10000000)
	issuer, vm, _, sharedMemory, _ := GenesisVM(t, true, genesisJSONApricotPhase5, "", "")

	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	kc := secp256k1fx.NewKeychain()
	kc.Add(testKeys[0])
	txID, err := ids.ToID(hashing.ComputeHash256(testShortIDAddrs[0][:]))
	assert.NoError(t, err)

	mempoolTxs := 200
	for i := 0; i < mempoolTxs; i++ {
		utxo, err := addUTXO(sharedMemory, vm.ctx, txID, uint32(i), vm.ctx.AVAXAssetID, importAmount, testShortIDAddrs[0])
		assert.NoError(t, err)

		importTx, err := vm.newImportTxWithUTXOs(vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, kc, []*avax.UTXO{utxo})
		if err != nil {
			t.Fatal(err)
		}
		if err := vm.issueTx(importTx, true); err != nil {
			t.Fatal(err)
		}
	}

	<-issuer
	blk, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	atomicTxs := blk.(*chain.BlockWrapper).Block.(*Block).atomicTxs

	// Need to ensure that not all of the transactions in the mempool are included in the block.
	// This ensures that we hit the atomic gas limit while building the block before we hit the
	// upper limit on the size of the codec for marshalling the atomic transactions.
	if len(atomicTxs) >= mempoolTxs {
		t.Fatalf("Expected number of atomic transactions included in the block (%d) to be less than the number of transactions added to the mempool (%d)", len(atomicTxs), mempoolTxs)
	}
}

func TestExtraStateChangeAtomicGasLimitExceeded(t *testing.T) {
	importAmount := uint64(10000000)
	// We create two VMs one in ApriotPhase4 and one in ApricotPhase5, so that we can construct a block
	// containing a large enough atomic transaction that it will exceed the atomic gas limit in
	// ApricotPhase5.
	issuer, vm1, _, sharedMemory1, _ := GenesisVM(t, true, genesisJSONApricotPhase4, "", "")
	_, vm2, _, sharedMemory2, _ := GenesisVM(t, true, genesisJSONApricotPhase5, "", "")

	defer func() {
		if err := vm1.Shutdown(); err != nil {
			t.Fatal(err)
		}
		if err := vm2.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()

	kc := secp256k1fx.NewKeychain()
	kc.Add(testKeys[0])
	txID, err := ids.ToID(hashing.ComputeHash256(testShortIDAddrs[0][:]))
	assert.NoError(t, err)

	// Add enough UTXOs, such that the created import transaction will attempt to consume more gas than allowed
	// in ApricotPhase5.
	for i := 0; i < 100; i++ {
		_, err := addUTXO(sharedMemory1, vm1.ctx, txID, uint32(i), vm1.ctx.AVAXAssetID, importAmount, testShortIDAddrs[0])
		assert.NoError(t, err)

		_, err = addUTXO(sharedMemory2, vm2.ctx, txID, uint32(i), vm2.ctx.AVAXAssetID, importAmount, testShortIDAddrs[0])
		assert.NoError(t, err)
	}

	// Double the initial base fee used when estimating the cost of this transaction to ensure that when it is
	// used in ApricotPhase5 it still pays a sufficient fee with the fixed fee per atomic transaction.
	importTx, err := vm1.newImportTx(vm1.ctx.XChainID, testEthAddrs[0], new(big.Int).Mul(common.Big2, initialBaseFee), []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}
	if err := vm1.issueTx(importTx, true); err != nil {
		t.Fatal(err)
	}

	<-issuer
	blk1, err := vm1.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := blk1.Verify(); err != nil {
		t.Fatal(err)
	}

	validEthBlock := blk1.(*chain.BlockWrapper).Block.(*Block).ethBlock

	extraData, err := vm2.codec.Marshal(codecVersion, []*Tx{importTx})
	if err != nil {
		t.Fatal(err)
	}

	// Construct the new block with the extra data in the new format (slice of atomic transactions).
	ethBlk2 := types.NewBlock(
		types.CopyHeader(validEthBlock.Header()),
		nil,
		nil,
		nil,
		new(trie.Trie),
		extraData,
		true,
	)

	state, err := vm2.chain.CurrentState()
	if err != nil {
		t.Fatal(err)
	}

	// Hack: test [onExtraStateChange] directly to ensure it catches the atomic gas limit error correctly.
	if _, _, err := vm2.onExtraStateChange(ethBlk2, state); err == nil || !strings.Contains(err.Error(), "exceeds atomic gas limit") {
		t.Fatalf("Expected block to fail verification due to exceeded atomic gas limit, but found error: %v", err)
	}
}

func TestGetAtomicRepositoryRepairHeights(t *testing.T) {
	mainnetHeights := getAtomicRepositoryRepairHeights(params.AvalancheMainnetChainID)
	assert.Len(t, mainnetHeights, 76)
	sorted := sort.SliceIsSorted(mainnetHeights, func(i, j int) bool { return mainnetHeights[i] < mainnetHeights[j] })
	assert.True(t, sorted)
	testnetHeights := getAtomicRepositoryRepairHeights(params.AvalancheFujiChainID)
	assert.Empty(t, testnetHeights)
}
