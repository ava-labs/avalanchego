// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package vmtest

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/graft/coreth/core"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/params/paramstest"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap3"

	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
)

var (
	TestKeys         = secp256k1.TestKeys()[:3]
	TestEthAddrs     []common.Address // testEthAddrs[i] corresponds to testKeys[i]
	TestShortIDAddrs []ids.ShortID
	InitialBaseFee   = big.NewInt(ap3.InitialBaseFee)
	InitialFund      = new(big.Int).Mul(big.NewInt(params.Ether), big.NewInt(10))
)

func init() {
	for _, pk := range TestKeys {
		TestEthAddrs = append(TestEthAddrs, pk.EthAddress())
		TestShortIDAddrs = append(TestShortIDAddrs, pk.Address())
	}
}

// GenesisJSON returns the JSON representation of the genesis block
// for the given chain configuration, with pre-funded accounts.
func GenesisJSON(cfg *params.ChainConfig) string {
	g := NewTestGenesis(cfg)
	b, err := json.Marshal(g)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func NewTestGenesis(cfg *params.ChainConfig) *core.Genesis {
	g := new(core.Genesis)
	g.Difficulty = big.NewInt(0)
	g.GasLimit = 0x5f5e100
	g.Timestamp = uint64(upgrade.InitiallyActiveTime.Unix())

	// Use chainId: 43111, so that it does not overlap with any Avalanche ChainIDs, which may have their
	// config overridden in vm.Initialize.
	cpy := *cfg
	cpy.ChainID = big.NewInt(43111)
	g.Config = &cpy

	allocStr := `{"0100000000000000000000000000000000000000":{"code":"0x7300000000000000000000000000000000000000003014608060405260043610603d5760003560e01c80631e010439146042578063b6510bb314606e575b600080fd5b605c60048036036020811015605657600080fd5b503560b1565b60408051918252519081900360200190f35b818015607957600080fd5b5060af60048036036080811015608e57600080fd5b506001600160a01b03813516906020810135906040810135906060013560b6565b005b30cd90565b836001600160a01b031681836108fc8690811502906040516000604051808303818888878c8acf9550505050505015801560f4573d6000803e3d6000fd5b505050505056fea26469706673582212201eebce970fe3f5cb96bf8ac6ba5f5c133fc2908ae3dcd51082cfee8f583429d064736f6c634300060a0033","balance":"0x0"}}`
	json.Unmarshal([]byte(allocStr), &g.Alloc)
	// After Durango, an additional account is funded in tests to use
	// with warp messages.
	if params.GetExtra(cfg).IsDurango(0) {
		addr := common.HexToAddress("0x99b9DEA54C48Dfea6aA9A4Ca4623633EE04ddbB5")
		g.Alloc[addr] = types.Account{Balance: InitialFund}
	}

	// Fund the test keys
	for _, ethAddr := range TestEthAddrs {
		g.Alloc[ethAddr] = types.Account{Balance: InitialFund}
	}

	return g
}

func NewPrefundedGenesis(
	balance int,
	addresses ...common.Address,
) *core.Genesis {
	alloc := types.GenesisAlloc{}
	for _, address := range addresses {
		alloc[address] = types.Account{
			Balance: big.NewInt(int64(balance)),
		}
	}

	return &core.Genesis{
		Config:     params.TestChainConfig,
		Difficulty: big.NewInt(0),
		Alloc:      alloc,
	}
}

// SetupGenesis sets up the genesis
func SetupGenesis(
	t *testing.T,
	fork upgradetest.Fork,
) (*snow.Context,
	*prefixdb.Database,
	[]byte,
	*avalancheatomic.Memory,
) {
	ctx := snowtest.Context(t, snowtest.CChainID)
	ctx.NetworkUpgrades = upgradetest.GetConfig(fork)

	baseDB := memdb.New()

	// initialize the atomic memory
	atomicMemory := avalancheatomic.NewMemory(prefixdb.New([]byte{0}, baseDB))
	ctx.SharedMemory = atomicMemory.NewSharedMemory(ctx.ChainID)

	// NB: this lock is intentionally left locked when this function returns.
	// The caller of this function is responsible for unlocking.
	ctx.Lock.Lock()

	prefixedDB := prefixdb.New([]byte{1}, baseDB)
	genesisJSON := GenesisJSON(paramstest.ForkToChainConfig[fork])
	return ctx, prefixedDB, []byte(genesisJSON), atomicMemory
}
