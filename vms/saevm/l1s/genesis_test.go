// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/arr4n/shed/testerr"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/triedb"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
	"github.com/ava-labs/avalanchego/vms/saevm/cmputils"

	l1params "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
)

func TestMain(m *testing.M) {
	evm.RegisterAllLibEVMExtras()
	m.Run()
}

func errIsType[T error]() testerr.Want {
	return testerr.As(func(T) string { return "" })
}

const testChainID = 43111

var (
	testGenesisTime = uint64(upgrade.InitiallyActiveTime.Unix())

	testAllocAddr   = common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC")
	testAirdropAddr = common.HexToAddress("0x0100000000000000000000000000000000000001")
	testAllocFund   = big.NewInt(1_000_000_000_000_000_000)
	testAirdropFund = big.NewInt(1_000)

	testAirdropData = mustMarshal([]*core.Airdrop{
		{Address: testAirdropAddr},
		{Address: testAllocAddr}, // overridden by the alloc
	})
	testAirdropHash = common.BytesToHash(crypto.Keccak256(testAirdropData))
)

// mutateFeeConfig returns a copy of the default fee config with `mutate`
// applied. The default is fully populated, so a mutation isolates the single
// field under test.
func mutateFeeConfig(mutate func(*commontype.FeeConfig)) commontype.FeeConfig {
	c := l1params.DefaultFeeConfig
	mutate(&c)
	return c
}

func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// testEthConfig returns the Ethereum portion of a chain config as an L1
// genesis typically declares it: all block-based forks active, with the
// time-based forks left to be derived from the Avalanche upgrades.
func testEthConfig() *params.ChainConfig {
	return &params.ChainConfig{
		ChainID:             big.NewInt(testChainID),
		HomesteadBlock:      big.NewInt(0),
		EIP150Block:         big.NewInt(0),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		MuirGlacierBlock:    big.NewInt(0),
		BerlinBlock:         big.NewInt(0),
		LondonBlock:         big.NewInt(0),
	}
}

type genesisOption = options.Option[core.Genesis]

// testGenesis returns a minimal L1 genesis. Upgrade timestamps other than
// SubnetEVM are left unset so that they default to the network upgrades of
// the context used to parse the genesis.
func testGenesis(opts ...genesisOption) *core.Genesis {
	return options.ApplyTo(&core.Genesis{
		Config: l1params.WithExtra(testEthConfig(), &extras.ChainConfig{
			NetworkUpgrades: extras.NetworkUpgrades{
				SubnetEVMTimestamp: new(uint64(0)),
			},
			FeeConfig: l1params.DefaultFeeConfig,
		}),
		Difficulty: big.NewInt(0),
		GasLimit:   l1params.DefaultFeeConfig.GasLimit.Uint64(),
		Timestamp:  testGenesisTime,
		Alloc: types.GenesisAlloc{
			testAllocAddr: {Balance: testAllocFund},
		},
	}, opts...)
}

func testGenesisJSON(opts ...genesisOption) string {
	return string(mustMarshal(testGenesis(opts...)))
}

func withValidAirdrop() genesisOption {
	return options.Func[core.Genesis](func(g *core.Genesis) {
		g.AirdropHash = testAirdropHash
		g.AirdropAmount = testAirdropFund
	})
}

func withInitialMinDelay(delayMS uint64) genesisOption {
	return options.Func[core.Genesis](func(g *core.Genesis) {
		l1params.GetExtra(g.Config).InitialMinDelayMS = delayMS
	})
}

func withFeeConfig(c commontype.FeeConfig) genesisOption {
	return options.Func[core.Genesis](func(g *core.Genesis) {
		l1params.GetExtra(g.Config).FeeConfig = c
	})
}

func newContext(t *testing.T, fork upgradetest.Fork) *snow.Context {
	ctx := snowtest.Context(t, ids.GenerateTestID())
	ctx.NetworkUpgrades = upgradetest.GetConfig(fork)
	ctx.Log = loggingtest.New(t, logging.Debug)
	return ctx
}

func TestParseGenesis(t *testing.T) {
	var (
		latest    = newContext(t, upgradetest.Latest)
		durango   = newContext(t, upgradetest.Durango)
		delayedTS = new(testGenesisTime + 100)
	)
	tests := []struct {
		name         string
		ctx          *snow.Context
		genesis      string
		upgradeBytes string
		airdropData  []byte
		wantErr      testerr.Want
	}{
		{
			name:    "defaults",
			ctx:     latest,
			genesis: testGenesisJSON(),
		},
		{
			name:    "fee_config_defaulted",
			ctx:     latest,
			genesis: testGenesisJSON(withFeeConfig(commontype.EmptyFeeConfig)),
		},
		{
			name:    "upgrade_bytes",
			ctx:     durango,
			genesis: testGenesisJSON(),
			upgradeBytes: string(mustMarshal(extras.UpgradeConfig{
				NetworkUpgradeOverrides: &extras.NetworkUpgrades{
					DurangoTimestamp: delayedTS,
				},
			})),
		},
		{
			name:        "airdrop",
			ctx:         latest,
			genesis:     testGenesisJSON(withValidAirdrop()),
			airdropData: testAirdropData,
		},
		{
			name:    "invalid_json",
			ctx:     latest,
			genesis: "not json",
			wantErr: errIsType[*json.SyntaxError](),
		},
		{
			name:    "missing_required_fields",
			ctx:     latest,
			genesis: `{"not-airdrop":true}`,
			wantErr: testerr.Contains("missing required field"),
		},
		{
			name:    "no_config",
			ctx:     latest,
			genesis: `{"gasLimit":"0x0","difficulty":"0x0","alloc":{}}`,
			wantErr: testerr.Is(errNoGenesisChainConfig),
		},
		{
			name:    "no_chain_id",
			ctx:     latest,
			genesis: `{"config":{},"gasLimit":"0x0","difficulty":"0x0","alloc":{}}`,
			wantErr: testerr.Is(errNoGenesisChainID),
		},
		{
			name:    "non_zero_number",
			ctx:     latest,
			genesis: `{"config":{"chainId":43111},"gasLimit":"0x0","difficulty":"0x0","alloc":{},"number":"0x1"}`,
			wantErr: testerr.Is(errNonZeroGenesisNumber),
		},
		{
			name:    "non_zero_gas_used",
			ctx:     latest,
			genesis: `{"config":{"chainId":43111},"gasLimit":"0x0","difficulty":"0x0","alloc":{},"gasUsed":"0x1"}`,
			wantErr: testerr.Is(errNonZeroGenesisGasUsed),
		},
		{
			name:    "nonzero_parent_hash",
			ctx:     latest,
			genesis: `{"config":{"chainId":43111},"gasLimit":"0x0","difficulty":"0x0","alloc":{},"parentHash":"0x0100000000000000000000000000000000000000000000000000000000000000"}`,
			wantErr: testerr.Is(errNonZeroGenesisParentHash),
		},
		{
			name:    "non_nil_excess_blob_gas",
			ctx:     latest,
			genesis: `{"config":{"chainId":43111},"gasLimit":"0x0","difficulty":"0x0","alloc":{},"excessBlobGas":"0x0"}`,
			wantErr: testerr.Is(errNonNilGenesisExcessBlobGas),
		},
		{
			name:    "non_nil_blob_gas_used",
			ctx:     latest,
			genesis: `{"config":{"chainId":43111},"gasLimit":"0x0","difficulty":"0x0","alloc":{},"blobGasUsed":"0x0"}`,
			wantErr: testerr.Is(errNonNilGenesisBlobGasUsed),
		},
		{
			name: "gas_limit_mismatch",
			ctx:  latest,
			genesis: testGenesisJSON(options.Func[core.Genesis](func(g *core.Genesis) {
				g.GasLimit++
			})),
			wantErr: testerr.Is(errGasLimitMismatch),
		},
		{
			name: "invalid_fee_config",
			ctx:  latest,
			genesis: testGenesisJSON(withFeeConfig(mutateFeeConfig(func(c *commontype.FeeConfig) {
				c.GasLimit = nil
			}))),
			wantErr: testerr.Is(commontype.ErrGasLimitNil),
		},
		{
			name:    "initial_min_delay_too_large",
			ctx:     latest,
			genesis: testGenesisJSON(withInitialMinDelay(acp226.InitialDelayExcess.Delay() + 1)),
			wantErr: testerr.Contains("initialMinDelayMS too large"),
		},
		{
			// An L1 MUST NOT be able to activate a fork before the network it
			// runs on supports it.
			name:    "upgrade_override_before_network_upgrade",
			ctx:     latest,
			genesis: testGenesisJSON(),
			upgradeBytes: string(mustMarshal(extras.UpgradeConfig{
				NetworkUpgradeOverrides: &extras.NetworkUpgrades{
					GraniteTimestamp: new(uint64(1)),
				},
			})),
			wantErr: testerr.Contains("granite fork block timestamp is invalid"),
		},
		{
			name:         "invalid_upgrade_bytes",
			ctx:          latest,
			genesis:      testGenesisJSON(),
			upgradeBytes: "not json",
			wantErr:      errIsType[*json.SyntaxError](),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g, err := parseGenesis(test.ctx, []byte(test.genesis), []byte(test.upgradeBytes), test.airdropData)
			if diff := testerr.Diff(err, test.wantErr); diff != "" {
				t.Fatalf("parseGenesis(...) error (-want +got)\n%s", diff)
			}
			if err != nil {
				return
			}

			require.Equal(t, test.airdropData, g.AirdropData, "airdrop data mismatch")

			// Everything other than the config and the airdrop data must be
			// passed through unmodified.
			var want core.Genesis
			require.NoError(t, json.Unmarshal([]byte(test.genesis), &want))
			opts := cmp.Options{
				cmputils.BigInts(),
				cmpopts.EquateEmpty(),
				cmpopts.IgnoreFields(core.Genesis{}, "Config", "AirdropData"),
			}
			if diff := cmp.Diff(&want, (*core.Genesis)(g), opts); diff != "" {
				t.Errorf("parseGenesis(...) (-want +got)\n%s", diff)
			}
		})
	}
}

// TestGenesisBlockMatchesSubnetEVM ensures that the genesis block, and thus
// the genesis hash, is identical to the one subnet-evm produces for the same
// genesis. Existing L1s MUST retain their genesis hash.
//
// TODO: Delete this test when graft/subnet-evm is being deleted.
func TestGenesisBlockMatchesSubnetEVM(t *testing.T) {
	type spec struct {
		name        string
		upgrades    upgradetest.Fork
		genesis     string
		airdropData []byte
	}
	var specs []spec
	for fork := upgradetest.NoUpgrades; fork <= upgradetest.Latest; fork++ {
		specs = append(specs, spec{
			name:     fork.String(),
			upgrades: fork,
			genesis:  testGenesisJSON(),
		})
	}
	specs = append(specs,
		spec{
			name:        "airdrop",
			upgrades:    upgradetest.Latest,
			genesis:     testGenesisJSON(withValidAirdrop()),
			airdropData: testAirdropData,
		},
		spec{
			name:     "initial_min_delay",
			upgrades: upgradetest.Latest,
			genesis:  testGenesisJSON(withInitialMinDelay(1_500)),
		},
		spec{
			name:     "explicit_base_fee",
			upgrades: upgradetest.Latest,
			genesis: testGenesisJSON(options.Func[core.Genesis](func(g *core.Genesis) {
				g.BaseFee = big.NewInt(1_000)
			})),
		},
		spec{
			// subnet-evm commits the genesis state with deleteEmptyObjects=false.
			name:     "empty_account",
			upgrades: upgradetest.Latest,
			genesis: testGenesisJSON(options.Func[core.Genesis](func(g *core.Genesis) {
				g.Alloc[common.Address{0xe0}] = types.Account{Balance: new(big.Int)}
			})),
		},
		spec{
			name:     "header_fields",
			upgrades: upgradetest.Latest,
			genesis: testGenesisJSON(options.Func[core.Genesis](func(g *core.Genesis) {
				g.Nonce = 42
				g.ExtraData = []byte("extra")
				g.Mixhash = common.Hash{1}
				g.Coinbase = common.Address{2}
				g.Difficulty = big.NewInt(3)
			})),
		},
	)

	for _, s := range specs {
		t.Run(s.name, func(t *testing.T) {
			g, err := parseGenesis(newContext(t, s.upgrades), []byte(s.genesis), nil, s.airdropData)
			require.NoErrorf(t, err, "parseGenesis(%s)", s.genesis)

			got, err := g.block()
			require.NoErrorf(t, err, "%T.block()", g)
			want := (*core.Genesis)(g).ToBlock()

			require.Equalf(t, want.Root(), got.Root(), "%T.block().Root()", g)
			require.Equalf(t, want.Hash(), got.Hash(), "%T.block().Hash()", g)
		})
	}
}

func TestGenesisAirdrop(t *testing.T) {
	withAirdropHashOf := func(data []byte) genesisOption {
		return options.Func[core.Genesis](func(g *core.Genesis) {
			g.AirdropHash = common.BytesToHash(crypto.Keccak256(data))
		})
	}

	invalidData := []byte("not json")

	tests := []struct {
		name        string
		opts        []genesisOption
		airdropData []byte
		wantErr     testerr.Want
	}{
		{
			name:        "valid",
			opts:        []genesisOption{withValidAirdrop()},
			airdropData: testAirdropData,
		},
		{
			name:    "missing_data",
			opts:    []genesisOption{withValidAirdrop()},
			wantErr: testerr.Is(errAirdropHashMismatch),
		},
		{
			name:        "mismatched_data",
			opts:        []genesisOption{withValidAirdrop()},
			airdropData: mustMarshal([]*core.Airdrop{{Address: common.Address{0xff}}}),
			wantErr:     testerr.Is(errAirdropHashMismatch),
		},
		{
			name:        "data_without_hash",
			airdropData: testAirdropData,
			wantErr:     testerr.Is(errAirdropHashMismatch),
		},
		{
			name:        "invalid_json",
			opts:        []genesisOption{withAirdropHashOf(invalidData)},
			airdropData: invalidData,
			wantErr:     errIsType[*json.SyntaxError](),
		},
		{
			name:        "no_amount",
			opts:        []genesisOption{withAirdropHashOf(testAirdropData)},
			airdropData: testAirdropData,
			wantErr:     testerr.Is(errNoAirdropAmount),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			genesis := testGenesisJSON(tt.opts...)
			g, err := parseGenesis(newContext(t, upgradetest.Latest), []byte(genesis), nil, tt.airdropData)
			require.NoErrorf(t, err, "parseGenesis(%s)", genesis)

			_, err = g.block()
			if diff := testerr.Diff(err, tt.wantErr); diff != "" {
				t.Errorf("%T.block() error (-want +got)\n%s", g, diff)
			}
		})
	}
}

// This test is intentionally a change detector: the genesis hash is part of
// consensus, so any change would break deployed networks.
func TestHistoricalGenesisHashes(t *testing.T) {
	hashes := [...]string{
		// SubnetEVM is always active at genesis, which adds the BaseFee.
		upgradetest.NoUpgrades:        "0x66e75620d5123ead246047e3807b2a3c80f237bdb5105d0d6bee5708373eb0c9",
		upgradetest.ApricotPhase1:     "0x66e75620d5123ead246047e3807b2a3c80f237bdb5105d0d6bee5708373eb0c9",
		upgradetest.ApricotPhase2:     "0x66e75620d5123ead246047e3807b2a3c80f237bdb5105d0d6bee5708373eb0c9",
		upgradetest.ApricotPhase3:     "0x66e75620d5123ead246047e3807b2a3c80f237bdb5105d0d6bee5708373eb0c9",
		upgradetest.ApricotPhase4:     "0x66e75620d5123ead246047e3807b2a3c80f237bdb5105d0d6bee5708373eb0c9",
		upgradetest.ApricotPhase5:     "0x66e75620d5123ead246047e3807b2a3c80f237bdb5105d0d6bee5708373eb0c9",
		upgradetest.ApricotPhasePre6:  "0x66e75620d5123ead246047e3807b2a3c80f237bdb5105d0d6bee5708373eb0c9",
		upgradetest.ApricotPhase6:     "0x66e75620d5123ead246047e3807b2a3c80f237bdb5105d0d6bee5708373eb0c9",
		upgradetest.ApricotPhasePost6: "0x66e75620d5123ead246047e3807b2a3c80f237bdb5105d0d6bee5708373eb0c9",
		upgradetest.Banff:             "0x66e75620d5123ead246047e3807b2a3c80f237bdb5105d0d6bee5708373eb0c9",
		upgradetest.Cortina:           "0x66e75620d5123ead246047e3807b2a3c80f237bdb5105d0d6bee5708373eb0c9",
		upgradetest.Durango:           "0x66e75620d5123ead246047e3807b2a3c80f237bdb5105d0d6bee5708373eb0c9",

		// Added the block gas cost and the Cancun fields
		upgradetest.Etna:    "0xec21bbd365270e4807627fa4f78cf4637ce0540ab1e822eca34bba7661a79708",
		upgradetest.Fortuna: "0xec21bbd365270e4807627fa4f78cf4637ce0540ab1e822eca34bba7661a79708",

		// Added millisecond timestamps and the min delay excess
		upgradetest.Granite: "0xd57b1b98e59e33327a109e0e5c2d14e0c66f7521ea53939a35a09b1f4dfcbeae",
		upgradetest.Helicon: "0xd57b1b98e59e33327a109e0e5c2d14e0c66f7521ea53939a35a09b1f4dfcbeae",

		// TODO: Add SAE required fields
	}
	_ = hashes[upgradetest.Latest] // Enforce completeness at compile time.

	for _fork, hash := range hashes {
		fork := upgradetest.Fork(_fork)
		t.Run(fork.String(), func(t *testing.T) {
			genesis := testGenesisJSON()
			g, err := parseGenesis(newContext(t, fork), []byte(genesis), nil, nil)
			require.NoErrorf(t, err, "parseGenesis(%s)", genesis)

			block, err := g.block()
			require.NoErrorf(t, err, "%T.block()", g)

			// Debugging this test is basically impossible without the header.
			{
				header := block.Header()
				headerJSON, err := json.MarshalIndent(header, "", "\t")
				require.NoErrorf(t, err, "json.MarshalIndent(%T)", header)
				t.Logf("header:\n%s", headerJSON)
			}
			require.Equalf(t, hash, block.Hash().String(), "%T.block().Hash()", g)
		})
	}
}

func TestWriteGenesis(t *testing.T) {
	type spec struct {
		fork              upgradetest.Fork
		latestUpgradeTime time.Time
		genesis           string
		upgradeBytes      string
	}
	newSpecContext := func(t *testing.T, s spec) *snow.Context {
		ctx := newContext(t, s.fork)
		if !s.latestUpgradeTime.IsZero() {
			ctx.NetworkUpgrades.HeliconTime = s.latestUpgradeTime
		}
		return ctx
	}

	tests := []struct {
		name    string
		initial spec
		// If non-zero, a head header with this timestamp is written after
		// the initial genesis to simulate the chain having progressed.
		headTime uint64
		restart  spec
		wantErr  testerr.Want
	}{
		{
			name: "same_genesis",
			initial: spec{
				fork:    upgradetest.Latest,
				genesis: testGenesisJSON(),
			},
			restart: spec{
				fork:    upgradetest.Latest,
				genesis: testGenesisJSON(),
			},
		},
		{
			name: "same_genesis_with_progressed_head",
			initial: spec{
				fork:    upgradetest.Latest,
				genesis: testGenesisJSON(),
			},
			headTime: testGenesisTime + 4_000,
			restart: spec{
				fork:    upgradetest.Latest,
				genesis: testGenesisJSON(),
			},
		},
		{
			name: "genesis_hash_mismatch",
			initial: spec{
				fork:    upgradetest.Latest,
				genesis: testGenesisJSON(),
			},
			restart: spec{
				fork: upgradetest.Latest,
				genesis: testGenesisJSON(options.Func[core.Genesis](func(g *core.Genesis) {
					g.Alloc[testAllocAddr] = types.Account{Balance: big.NewInt(1)}
				})),
			},
			wantErr: errIsType[*core.GenesisMismatchError](),
		},
		{
			name: "schedule_future_upgrade",
			initial: spec{
				fork:    upgradetest.Latest - 1,
				genesis: testGenesisJSON(),
			},
			restart: spec{
				fork:              upgradetest.Latest,
				latestUpgradeTime: upgrade.InitiallyActiveTime.Add(1_000 * time.Second),
				genesis:           testGenesisJSON(),
			},
		},
		{
			name: "delay_future_upgrade",
			initial: spec{
				fork:              upgradetest.Latest,
				latestUpgradeTime: upgrade.InitiallyActiveTime.Add(1_000 * time.Second),
				genesis:           testGenesisJSON(),
			},
			restart: spec{
				fork:              upgradetest.Latest,
				latestUpgradeTime: upgrade.InitiallyActiveTime.Add(2_000 * time.Second),
				genesis:           testGenesisJSON(),
			},
		},
		{
			name: "advance_future_upgrade",
			initial: spec{
				fork:              upgradetest.Latest,
				latestUpgradeTime: upgrade.InitiallyActiveTime.Add(2_000 * time.Second),
				genesis:           testGenesisJSON(),
			},
			restart: spec{
				fork:              upgradetest.Latest,
				latestUpgradeTime: upgrade.InitiallyActiveTime.Add(1_000 * time.Second),
				genesis:           testGenesisJSON(),
			},
		},
		{
			name: "add_future_network_override",
			initial: spec{
				fork:              upgradetest.Latest,
				latestUpgradeTime: upgrade.InitiallyActiveTime.Add(1_000 * time.Second),
				genesis:           testGenesisJSON(),
			},
			restart: spec{
				fork:              upgradetest.Latest,
				latestUpgradeTime: upgrade.InitiallyActiveTime.Add(1_000 * time.Second),
				genesis:           testGenesisJSON(),
				upgradeBytes: string(mustMarshal(extras.UpgradeConfig{
					NetworkUpgradeOverrides: &extras.NetworkUpgrades{
						HeliconTimestamp: new(testGenesisTime + 2_000),
					},
				})),
			},
		},
		{
			// L1 missed upgrade, allows override to recover
			name: "delay_activated_network_override",
			initial: spec{
				fork:              upgradetest.Latest,
				genesis:           testGenesisJSON(),
				latestUpgradeTime: upgrade.UnscheduledActivationTime,
			},
			headTime: testGenesisTime + 4_000,
			restart: spec{
				fork:              upgradetest.Latest,
				genesis:           testGenesisJSON(),
				latestUpgradeTime: upgrade.InitiallyActiveTime.Add(1_000 * time.Second),
				upgradeBytes: string(mustMarshal(extras.UpgradeConfig{
					NetworkUpgradeOverrides: &extras.NetworkUpgrades{
						HeliconTimestamp: new(testGenesisTime + 3_000),
					},
				})),
			},
			wantErr: errIsType[*params.ConfigCompatError](),
		},
		{
			// L1 missed upgrade, allows override to recover
			name: "delay_missed_network_upgrade",
			initial: spec{
				fork:              upgradetest.Latest,
				genesis:           testGenesisJSON(),
				latestUpgradeTime: upgrade.UnscheduledActivationTime,
			},
			headTime: testGenesisTime + 2_000,
			restart: spec{
				fork:              upgradetest.Latest,
				genesis:           testGenesisJSON(),
				latestUpgradeTime: upgrade.InitiallyActiveTime.Add(1_000 * time.Second),
				upgradeBytes: string(mustMarshal(extras.UpgradeConfig{
					NetworkUpgradeOverrides: &extras.NetworkUpgrades{
						HeliconTimestamp: new(testGenesisTime + 3_000),
					},
				})),
			},
		},
		{
			// The head was executed under rules that included the upgrade, so
			// pushing it into the future would change how that block should
			// have been executed.
			name: "delay_activated_upgrade",
			initial: spec{
				fork:              upgradetest.Latest,
				latestUpgradeTime: upgrade.InitiallyActiveTime.Add(1_000 * time.Second),
				genesis:           testGenesisJSON(),
			},
			headTime: testGenesisTime + 4_000,
			restart: spec{
				fork:              upgradetest.Latest,
				latestUpgradeTime: upgrade.InitiallyActiveTime.Add(3_000 * time.Second),
				genesis:           testGenesisJSON(),
			},
			wantErr: errIsType[*params.ConfigCompatError](),
		},
		{
			name: "advance_activated_upgrade",
			initial: spec{
				fork:              upgradetest.Latest,
				latestUpgradeTime: upgrade.InitiallyActiveTime.Add(3_000 * time.Second),
				genesis:           testGenesisJSON(),
			},
			headTime: testGenesisTime + 4_000,
			restart: spec{
				fork:              upgradetest.Latest,
				latestUpgradeTime: upgrade.InitiallyActiveTime.Add(1_000 * time.Second),
				genesis:           testGenesisJSON(),
			},
			wantErr: errIsType[*params.ConfigCompatError](),
		},
		{
			// Durango does not change the genesis block, so un-scheduling it
			// isolates the fork-incompatibility error without tripping a
			// genesis-hash mismatch.
			name: "incompatible_upgrade",
			initial: spec{
				fork:    upgradetest.Durango,
				genesis: testGenesisJSON(),
			},
			restart: spec{
				fork:    upgradetest.Cortina,
				genesis: testGenesisJSON(),
			},
			wantErr: errIsType[*params.ConfigCompatError](),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()
			g, err := parseGenesis(
				newSpecContext(t, tt.initial),
				[]byte(tt.initial.genesis),
				[]byte(tt.initial.upgradeBytes),
				nil,
			)
			require.NoError(t, err, "parseGenesis(initial)")
			require.NoErrorf(t, g.verifyAndWriteBlock(db), "%T.verifyAndWriteBlock(initial)", g)

			block, err := g.block()
			require.NoErrorf(t, err, "%T.block()", g)
			genesisHash := block.Hash()
			requireStoredConfig(t, db, genesisHash, g.Config)
			initialConfig := g.Config

			if tt.headTime != 0 {
				head := &types.Header{
					ParentHash: genesisHash,
					Number:     big.NewInt(1),
					Time:       tt.headTime,
				}
				rawdb.WriteHeader(db, head)
				rawdb.WriteHeadHeaderHash(db, head.Hash())
			}

			// The restart runs on the initialized database. It must
			// agree on the block hash and store its own chain config.
			g, err = parseGenesis(
				newSpecContext(t, tt.restart),
				[]byte(tt.restart.genesis),
				[]byte(tt.restart.upgradeBytes),
				nil,
			)
			require.NoError(t, err, "parseGenesis(restart)")

			err = g.verifyAndWriteBlock(db)
			if diff := testerr.Diff(err, tt.wantErr); diff != "" {
				t.Fatalf("%T.verifyAndWriteBlock(restart) error (-want +got)\n%s", g, diff)
			}
			require.Equal(t, genesisHash, rawdb.ReadCanonicalHash(db, 0), "rawdb.ReadCanonicalHash(restart)")

			wantConfig := g.Config
			if tt.wantErr != nil {
				// A rejected restart MUST NOT leave the incompatible config
				// behind for the next one to read.
				wantConfig = initialConfig
			}
			requireStoredConfig(t, db, genesisHash, wantConfig)
		})
	}
}

// requireStoredConfig asserts that the chain config, including its upgrade
// config, stored for the genesis hash matches want.
func requireStoredConfig(t *testing.T, db ethdb.Database, genesisHash common.Hash, want *l1params.ChainConfig) {
	t.Helper()

	var gotUpgrades extras.UpgradeConfig
	got, err := customrawdb.ReadChainConfig(db, genesisHash, &gotUpgrades)
	require.NoError(t, err, "customrawdb.ReadChainConfig()")
	l1params.GetExtra(got).UpgradeConfig = gotUpgrades

	ethOpts := cmp.Options{
		cmputils.BigInts(),
		cmpopts.EquateEmpty(),
		cmpopts.IgnoreFields(params.ChainConfig{}, "extra"), // compared below
	}
	if diff := cmp.Diff(want, got, ethOpts); diff != "" {
		t.Errorf("stored Ethereum chain config (-want +got)\n%s", diff)
	}

	extraOpts := cmp.Options{
		cmputils.BigInts(),
		cmpopts.EquateEmpty(),
		// [extras.PrecompileUpgrade] embeds the [precompileconfig.Config]
		// interface so it has a promoted Equal method that cmp would call
		cmp.Comparer(func(a, b extras.PrecompileUpgrade) bool {
			if a.Config == nil || b.Config == nil {
				return a.Config == b.Config
			}
			return a.Config.Equal(b.Config)
		}),
		cmpopts.IgnoreFields(extras.ChainConfig{}, "AvalancheContext"), // not serialized
	}
	if diff := cmp.Diff(l1params.GetExtra(want), l1params.GetExtra(got), extraOpts); diff != "" {
		t.Errorf("stored extra chain config (-want +got)\n%s", diff)
	}
}

// TestWriteGenesisState ensures that the genesis state, including the airdrop,
// is persisted to the database and matches the root committed to by the
// written genesis header.
func TestWriteGenesisState(t *testing.T) {
	genesis := testGenesisJSON(withValidAirdrop())
	g, err := parseGenesis(newContext(t, upgradetest.Latest), []byte(genesis), nil, testAirdropData)
	require.NoErrorf(t, err, "parseGenesis(%s)", genesis)

	block, err := g.block()
	require.NoErrorf(t, err, "%T.block()", g)
	root := block.Root()

	db := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(db, triedb.HashDefaults)
	defer func() {
		require.NoErrorf(t, tdb.Close(), "%T.Close()", tdb)
	}()

	require.Falsef(t, tdb.Initialized(root), "%T.Initialized(%s)", tdb, root)
	require.NoErrorf(t, g.setupTrieDB(db, triedb.HashDefaults), "%T.setupTrieDB()", g)
	require.Truef(t, tdb.Initialized(root), "%T.Initialized(%s)", tdb, root)

	// Restarting a node calls setupTrieDB on an already populated database.
	require.NoErrorf(t, g.setupTrieDB(db, triedb.HashDefaults), "%T.setupTrieDB() second call", g)
	require.Truef(t, tdb.Initialized(root), "%T.Initialized(%s) after second call", tdb, root)

	// The state is only usable if it is the one the written header commits to.
	require.NoErrorf(t, g.verifyAndWriteBlock(db), "%T.verifyAndWriteBlock()", g)
	stored := rawdb.ReadHeader(db, block.Hash(), genesisNumber)
	require.NotNil(t, stored, "rawdb.ReadHeader()")
	require.Equal(t, root, stored.Root, "stored genesis header state root")

	statedb, err := state.New(root, state.NewDatabaseWithNodeDB(db, tdb), nil)
	require.NoError(t, err, "state.New()")

	// The airdrop is applied, but the explicit alloc takes precedence.
	require.Equal(t, testAirdropFund, statedb.GetBalance(testAirdropAddr).ToBig(), "airdrop balance")
	require.Equal(t, testAllocFund, statedb.GetBalance(testAllocAddr).ToBig(), "alloc balance")
}
