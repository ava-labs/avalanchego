// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"encoding/json"
	"math/big"
	"reflect"
	"testing"
	"time"

	"github.com/arr4n/shed/testerr"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/triedb"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	"github.com/ava-labs/avalanchego/graft/evm/firewood"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/saevm/cmputils"

	corethparams "github.com/ava-labs/avalanchego/graft/coreth/params"
	ethparams "github.com/ava-labs/libevm/params"
)

// TestParseGenesis locks in the C-Chain genesis of Mainnet, Fuji, and the Local
// network.
//
// It is intentionally a change detector, as any changes to the genesis
// block would break live networks. Only the scheduling of new upgrades should
// require changes to this test.
func TestParseGenesis(t *testing.T) {
	const nativeAssetContract = "0x7300000000000000000000000000000000000000003014608060405260043610603d5760003560e01c80631e010439146042578063b6510bb314606e575b600080fd5b605c60048036036020811015605657600080fd5b503560b1565b60408051918252519081900360200190f35b818015607957600080fd5b5060af60048036036080811015608e57600080fd5b506001600160a01b03813516906020810135906040810135906060013560b6565b005b30cd90565b836001600160a01b031681836108fc8690811502906040516000604051808303818888878c8acf9550505050505015801560f4573d6000803e3d6000fd5b505050505056fea26469706673582212201eebce970fe3f5cb96bf8ac6ba5f5c133fc2908ae3dcd51082cfee8f583429d064736f6c634300060a0033"
	var (
		mainnetCtx = &snow.Context{NetworkUpgrades: upgrade.Mainnet}
		fujiCtx    = &snow.Context{NetworkUpgrades: upgrade.Fuji}
		localCtx   = &snow.Context{NetworkUpgrades: upgrade.Default}

		initiallyActive = utils.PointerTo[uint64](uint64(upgrade.InitiallyActiveTime.Unix()))
		unscheduled     = utils.PointerTo[uint64](uint64(upgrade.UnscheduledActivationTime.Unix()))
	)
	tests := []struct {
		name    string
		ctx     *snow.Context
		genesis string
		want    *core.Genesis
		wantErr testerr.Want
	}{
		{
			name:    "mainnet",
			ctx:     mainnetCtx,
			genesis: genesis.MainnetConfig.CChainGenesis,
			want: &core.Genesis{
				Config: corethparams.WithExtra(
					&corethparams.ChainConfig{
						ChainID:             big.NewInt(43114),
						HomesteadBlock:      big.NewInt(0),
						DAOForkBlock:        big.NewInt(0),
						DAOForkSupport:      true,
						EIP150Block:         big.NewInt(0),
						EIP155Block:         big.NewInt(0),
						EIP158Block:         big.NewInt(0),
						ByzantiumBlock:      big.NewInt(0),
						ConstantinopleBlock: big.NewInt(0),
						PetersburgBlock:     big.NewInt(0),
						IstanbulBlock:       big.NewInt(0),
						MuirGlacierBlock:    big.NewInt(0),
						BerlinBlock:         big.NewInt(1640340),                 // AP2 activation block
						LondonBlock:         big.NewInt(3308552),                 // AP3 activation block
						ShanghaiTime:        utils.PointerTo[uint64](1709740800), // Durango
						CancunTime:          utils.PointerTo[uint64](1734368400), // Etna
					},
					&extras.ChainConfig{
						NetworkUpgrades: extras.NetworkUpgrades{
							ApricotPhase1BlockTimestamp:     utils.PointerTo[uint64](1617199200),
							ApricotPhase2BlockTimestamp:     utils.PointerTo[uint64](1620644400),
							ApricotPhase3BlockTimestamp:     utils.PointerTo[uint64](1629813600),
							ApricotPhase4BlockTimestamp:     utils.PointerTo[uint64](1632344400),
							ApricotPhase5BlockTimestamp:     utils.PointerTo[uint64](1638468000),
							ApricotPhasePre6BlockTimestamp:  utils.PointerTo[uint64](1662341400),
							ApricotPhase6BlockTimestamp:     utils.PointerTo[uint64](1662494400),
							ApricotPhasePost6BlockTimestamp: utils.PointerTo[uint64](1662519600),
							BanffBlockTimestamp:             utils.PointerTo[uint64](1666108800),
							CortinaBlockTimestamp:           utils.PointerTo[uint64](1682434800),
							DurangoBlockTimestamp:           utils.PointerTo[uint64](1709740800),
							EtnaTimestamp:                   utils.PointerTo[uint64](1734368400),
							FortunaTimestamp:                utils.PointerTo[uint64](1744124400),
							GraniteTimestamp:                utils.PointerTo[uint64](1763568000),
							HeliconTimestamp:                unscheduled,
						},
						AvalancheContext: extras.AvalancheContext{
							SnowCtx: mainnetCtx,
						},
						UpgradeConfig: extras.UpgradeConfig{
							PrecompileUpgrades: []extras.PrecompileUpgrade{
								{
									Config: warp.NewDefaultConfig(
										utils.PointerTo[uint64](1709740800), // Durango
									),
								},
							},
						},
					},
				),
				GasLimit:   100000000,
				Difficulty: big.NewInt(0),
				ExtraData:  []byte{0},
				Alloc: types.GenesisAlloc{
					common.HexToAddress("0x0100000000000000000000000000000000000000"): {
						Code:    common.FromHex(nativeAssetContract),
						Balance: big.NewInt(0),
					},
				},
			},
		},
		{
			name:    "fuji",
			ctx:     fujiCtx,
			genesis: genesis.FujiConfig.CChainGenesis,
			want: &core.Genesis{
				Config: corethparams.WithExtra(
					&corethparams.ChainConfig{
						ChainID:             big.NewInt(43113),
						HomesteadBlock:      big.NewInt(0),
						DAOForkBlock:        big.NewInt(0),
						DAOForkSupport:      true,
						EIP150Block:         big.NewInt(0),
						EIP155Block:         big.NewInt(0),
						EIP158Block:         big.NewInt(0),
						ByzantiumBlock:      big.NewInt(0),
						ConstantinopleBlock: big.NewInt(0),
						PetersburgBlock:     big.NewInt(0),
						IstanbulBlock:       big.NewInt(0),
						MuirGlacierBlock:    big.NewInt(0),
						BerlinBlock:         big.NewInt(184985),                  // AP2 activation block
						LondonBlock:         big.NewInt(805078),                  // AP3 activation block
						ShanghaiTime:        utils.PointerTo[uint64](1707840000), // Durango
						CancunTime:          utils.PointerTo[uint64](1732550400), // Etna
					},
					&extras.ChainConfig{
						NetworkUpgrades: extras.NetworkUpgrades{
							ApricotPhase1BlockTimestamp:     utils.PointerTo[uint64](1616767200),
							ApricotPhase2BlockTimestamp:     utils.PointerTo[uint64](1620223200),
							ApricotPhase3BlockTimestamp:     utils.PointerTo[uint64](1629140400),
							ApricotPhase4BlockTimestamp:     utils.PointerTo[uint64](1631826000),
							ApricotPhase5BlockTimestamp:     utils.PointerTo[uint64](1637766000),
							ApricotPhasePre6BlockTimestamp:  utils.PointerTo[uint64](1662494400),
							ApricotPhase6BlockTimestamp:     utils.PointerTo[uint64](1662494400),
							ApricotPhasePost6BlockTimestamp: utils.PointerTo[uint64](1662530400),
							BanffBlockTimestamp:             utils.PointerTo[uint64](1664805600),
							CortinaBlockTimestamp:           utils.PointerTo[uint64](1680793200),
							DurangoBlockTimestamp:           utils.PointerTo[uint64](1707840000),
							EtnaTimestamp:                   utils.PointerTo[uint64](1732550400),
							FortunaTimestamp:                utils.PointerTo[uint64](1741878000),
							GraniteTimestamp:                utils.PointerTo[uint64](1761750000),
							HeliconTimestamp:                unscheduled,
						},
						AvalancheContext: extras.AvalancheContext{
							SnowCtx: fujiCtx,
						},
						UpgradeConfig: extras.UpgradeConfig{
							PrecompileUpgrades: []extras.PrecompileUpgrade{
								{
									Config: warp.NewDefaultConfig(
										utils.PointerTo[uint64](1707840000), // Durango
									),
								},
							},
						},
					},
				),
				GasLimit:   100000000,
				Difficulty: big.NewInt(0),
				ExtraData:  []byte{0},
				Alloc: types.GenesisAlloc{
					common.HexToAddress("0x0100000000000000000000000000000000000000"): {
						Code:    common.FromHex(nativeAssetContract),
						Balance: big.NewInt(0),
					},
				},
			},
		},
		{
			name:    "local",
			ctx:     localCtx,
			genesis: genesis.LocalConfig.CChainGenesis,
			want: &core.Genesis{
				Config: corethparams.WithExtra(
					&corethparams.ChainConfig{
						ChainID:             big.NewInt(43112),
						HomesteadBlock:      big.NewInt(0),
						DAOForkBlock:        big.NewInt(0),
						DAOForkSupport:      true,
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
						ShanghaiTime:        initiallyActive, // Durango
						CancunTime:          initiallyActive, // Etna
					},
					&extras.ChainConfig{
						NetworkUpgrades: extras.NetworkUpgrades{
							ApricotPhase1BlockTimestamp:     initiallyActive,
							ApricotPhase2BlockTimestamp:     initiallyActive,
							ApricotPhase3BlockTimestamp:     initiallyActive,
							ApricotPhase4BlockTimestamp:     initiallyActive,
							ApricotPhase5BlockTimestamp:     initiallyActive,
							ApricotPhasePre6BlockTimestamp:  initiallyActive,
							ApricotPhase6BlockTimestamp:     initiallyActive,
							ApricotPhasePost6BlockTimestamp: initiallyActive,
							BanffBlockTimestamp:             initiallyActive,
							CortinaBlockTimestamp:           initiallyActive,
							DurangoBlockTimestamp:           initiallyActive,
							EtnaTimestamp:                   initiallyActive,
							FortunaTimestamp:                initiallyActive,
							GraniteTimestamp:                initiallyActive,
							HeliconTimestamp:                unscheduled,
						},
						AvalancheContext: extras.AvalancheContext{
							SnowCtx: localCtx,
						},
						UpgradeConfig: extras.UpgradeConfig{
							PrecompileUpgrades: []extras.PrecompileUpgrade{
								{
									Config: warp.NewDefaultConfig(
										initiallyActive, // Durango
									),
								},
							},
						},
					},
				),
				Timestamp:  *initiallyActive, // the local genesis time coincides with initial activation
				GasLimit:   100000000,
				Difficulty: big.NewInt(0),
				ExtraData:  []byte{0},
				Alloc: types.GenesisAlloc{
					common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"): {
						Balance: hexutil.MustDecodeBig("0x295BE96E64066972000000"),
					},
				},
			},
		},
		{
			name:    "invalid_json",
			ctx:     localCtx,
			genesis: "not json",
			wantErr: errIsType[*json.SyntaxError](),
		},
		{
			name:    "missing_required_fields",
			ctx:     localCtx,
			genesis: `{}`,
			wantErr: testerr.Contains("missing required field"),
		},
		{
			name:    "missing_gasLimit",
			ctx:     localCtx,
			genesis: `{"config":{"chainId":43112},"difficulty":"0x0","alloc":{}}`,
			wantErr: testerr.Contains(`missing required field 'gasLimit'`),
		},
		{
			name:    "missing_difficulty",
			ctx:     localCtx,
			genesis: `{"config":{"chainId":43112},"gasLimit":"0x0","alloc":{}}`,
			wantErr: testerr.Contains(`missing required field 'difficulty'`),
		},
		{
			name:    "null_difficulty_disallowed",
			ctx:     localCtx,
			genesis: `{"config":{"chainId":43112},"gasLimit":"0x0","difficulty":null,"alloc":{}}`,
			wantErr: testerr.Contains(`missing required field 'difficulty'`),
		},
		{
			name:    "missing_alloc",
			ctx:     localCtx,
			genesis: `{"config":{"chainId":43112},"gasLimit":"0x0","difficulty":"0x0"}`,
			wantErr: testerr.Contains(`missing required field 'alloc'`),
		},
		{
			name:    "no_config",
			ctx:     localCtx,
			genesis: `{"gasLimit":"0x0","difficulty":"0x0","alloc":{}}`,
			wantErr: testerr.Is(errNoGenesisChainConfig),
		},
		{
			name:    "no_chain_id",
			ctx:     localCtx,
			genesis: `{"config":{},"gasLimit":"0x0","difficulty":"0x0","alloc":{}}`,
			wantErr: testerr.Is(errNoGenesisChainID),
		},
		{
			name:    "non_zero_number",
			ctx:     localCtx,
			genesis: `{"config":{"chainId":43112},"gasLimit":"0x0","difficulty":"0x0","alloc":{},"number":"0x1"}`,
			wantErr: testerr.Is(errNonZeroGenesisNumber),
		},
		{
			name:    "non_zero_gas_used",
			ctx:     localCtx,
			genesis: `{"config":{"chainId":43112},"gasLimit":"0x0","difficulty":"0x0","alloc":{},"gasUsed":"0x1"}`,
			wantErr: testerr.Is(errNonZeroGenesisGasUsed),
		},
		{
			name:    "nonzero_parent_hash",
			ctx:     localCtx,
			genesis: `{"config":{"chainId":43112},"gasLimit":"0x0","difficulty":"0x0","alloc":{},"parentHash":"0x0100000000000000000000000000000000000000000000000000000000000000"}`,
			wantErr: testerr.Is(errNonZeroGenesisParentHash),
		},
		{
			name:    "non_nil_excess_blob_gas",
			ctx:     localCtx,
			genesis: `{"config":{"chainId":43112},"gasLimit":"0x0","difficulty":"0x0","alloc":{},"excessBlobGas":"0x0"}`,
			wantErr: testerr.Is(errNonNilGenesisExcessBlobGas),
		},
		{
			name:    "non_nil_blob_gas_used",
			ctx:     localCtx,
			genesis: `{"config":{"chainId":43112},"gasLimit":"0x0","difficulty":"0x0","alloc":{},"blobGasUsed":"0x0"}`,
			wantErr: testerr.Is(errNonNilGenesisBlobGasUsed),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g, err := parseGenesis(test.ctx, []byte(test.genesis))
			if diff := testerr.Diff(err, test.wantErr); diff != "" {
				t.Fatalf("parseGenesis(...) error (-want +got)\n%s", diff)
			}
			opts := cmp.Options{
				cmputils.BigInts(),
				cmp.Comparer(func(a, b *corethparams.ChainConfig) bool {
					return reflect.DeepEqual(a, b)
				}),
			}
			if diff := cmp.Diff(test.want, g, opts); diff != "" {
				t.Errorf("parseGenesis(%s) (-want +got)\n%s", test.genesis, diff)
			}
		})
	}
}

// This test is intentionally a change detector: the genesis hash is part of
// consensus, so any change would break live networks.
//
// Only the local network genesis may change.
func TestGenesisHash(t *testing.T) {
	tests := []struct {
		name      string
		networkID uint32
		want      string
	}{
		{
			name:      "mainnet",
			networkID: constants.MainnetID,
			want:      "0x31ced5b9beb7f8782b014660da0cb18cc409f121f408186886e1ca3e8eeca96b",
		},
		{
			name:      "fuji",
			networkID: constants.FujiID,
			want:      "0x31ced5b9beb7f8782b014660da0cb18cc409f121f408186886e1ca3e8eeca96b",
		},
		{
			name:      "local",
			networkID: constants.LocalID,
			want:      "0x608ddbd611241719b64642d8e152537e2a5bdf46b6ddb9e8f15340c5e007b8b1",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := &snow.Context{
				NetworkUpgrades: upgrade.GetConfig(test.networkID),
			}
			genesis := genesis.GetConfig(test.networkID).CChainGenesis

			g, err := parseGenesis(ctx, []byte(genesis))
			require.NoError(t, err, "parseGenesis")

			block, err := genesisToBlock(g)
			require.NoError(t, err, "genesisToBlock")

			require.Equal(t, common.HexToHash(test.want), block.Hash())
		})
	}
}

func upgradeAt(fork upgradetest.Fork, t time.Time) upgrade.Config {
	c := upgradetest.GetConfigWithUpgradeTime(fork, t)
	upgradetest.SetTimesTo(&c, fork-1, upgrade.InitiallyActiveTime)
	return c
}

func TestSetupGenesis(t *testing.T) {
	var (
		latest       = upgradetest.GetConfig(upgradetest.Latest)
		localGenesis = genesis.LocalConfig.CChainGenesis
		fujiGenesis  = genesis.FujiConfig.CChainGenesis
	)
	tests := []struct {
		name            string
		initialUpgrades upgrade.Config
		restartUpgrades upgrade.Config
		restartGenesis  string
		wantErr         testerr.Want
	}{
		{
			// The first write commits a fresh database; re-running against the
			// already-initialized database, as on a restart, is idempotent.
			name:            "restart",
			initialUpgrades: latest,
			restartUpgrades: latest,
			restartGenesis:  localGenesis,
		},
		{
			// A different network's genesis has a different state root, and
			// therefore a different hash, than the one already stored.
			name:            "mismatch",
			initialUpgrades: latest,
			restartUpgrades: latest,
			restartGenesis:  fujiGenesis,
			wantErr:         errIsType[*core.GenesisMismatchError](),
		},
		{
			name:            "schedule_upgrade",
			initialUpgrades: upgradetest.GetConfig(upgradetest.Cortina),
			restartUpgrades: upgradeAt(
				upgradetest.Durango,
				upgrade.InitiallyActiveTime.Add(time.Second),
			),
			restartGenesis: localGenesis,
		},
		{
			name: "delay_future_upgrade",
			initialUpgrades: upgradeAt(
				upgradetest.Durango,
				upgrade.InitiallyActiveTime.Add(time.Second),
			),
			restartUpgrades: upgradeAt(
				upgradetest.Durango,
				upgrade.InitiallyActiveTime.Add(2*time.Second),
			),
			restartGenesis: localGenesis,
		},
		{
			name: "advance_future_upgrade",
			initialUpgrades: upgradeAt(
				upgradetest.Durango,
				upgrade.InitiallyActiveTime.Add(time.Second),
			),
			restartUpgrades: upgradeAt(
				upgradetest.Durango,
				upgrade.InitiallyActiveTime.Add(2*time.Second),
			),
			restartGenesis: localGenesis,
		},
		{
			// Rescheduling an upgrade that the head block has already activated
			// is rejected, as it could change the execution of accepted blocks.
			// Unscheduling Helicon leaves the genesis block unchanged (it is not
			// active at genesis) so the conflict surfaces as a compatibility
			// error rather than a genesis hash mismatch.
			name:            "incompatible_upgrade",
			initialUpgrades: latest,
			restartUpgrades: upgradetest.GetConfig(upgradetest.Granite),
			restartGenesis:  localGenesis,
			wantErr:         errIsType[*ethparams.ConfigCompatError](),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backends := []struct {
				name   string
				config *triedb.Config
			}{
				{
					name:   "hashdb",
					config: triedb.HashDefaults,
				},
				{
					name: "firewood",
					config: &triedb.Config{
						DBOverride: firewood.DefaultConfig(t.TempDir()).BackendConstructor,
					},
				},
			}
			for _, backend := range backends {
				t.Run(backend.name, func(t *testing.T) {
					db := rawdb.NewMemoryDatabase()
					trieConfig := backend.config

					g, err := parseGenesis(
						&snow.Context{NetworkUpgrades: test.initialUpgrades},
						[]byte(localGenesis),
					)
					require.NoError(t, err, "parseGenesis(initial)")

					block, err := setupGenesis(db, trieConfig, g)
					require.NoError(t, err, "writeGenesis(initial)")

					genesisHash := block.Hash()
					require.Equal(t, genesisHash, rawdb.ReadCanonicalHash(db, 0), "rawdb.ReadCanonicalHash(initial)")

					gotConfig := rawdb.ReadChainConfig(db, genesisHash)
					cmpBaseConfig := cmp.Options{
						cmpopts.IgnoreUnexported(corethparams.ChainConfig{}),
						cmputils.BigInts(),
					}
					if diff := cmp.Diff(g.Config, gotConfig, cmpBaseConfig); diff != "" {
						t.Errorf("initial stored base config (-want +got)\n%s", diff)
					}

					cmpNetworkUpgrades := cmp.Transformer("networkUpgrades", func(c *corethparams.ChainConfig) extras.NetworkUpgrades {
						return corethparams.GetExtra(c).NetworkUpgrades
					})
					if diff := cmp.Diff(g.Config, gotConfig, cmpNetworkUpgrades); diff != "" {
						t.Errorf("initial stored network upgrades (-want +got)\n%s", diff)
					}

					// The restart runs on the initialized database. It must agree on
					// the canonical block and store its own chain config.
					g, err = parseGenesis(
						&snow.Context{NetworkUpgrades: test.restartUpgrades},
						[]byte(test.restartGenesis),
					)
					require.NoError(t, err, "parseGenesis(restart)")

					block, err = setupGenesis(db, trieConfig, g)
					if diff := testerr.Diff(err, test.wantErr); diff != "" {
						t.Fatalf("writeGenesis(restart) error (-want +got)\n%s", diff)
					}
					require.Equal(t, genesisHash, rawdb.ReadCanonicalHash(db, 0), "rawdb.ReadCanonicalHash(restart)")
					if test.wantErr != nil {
						return
					}

					require.Equal(t, genesisHash, block.Hash(), "writeGenesis(restart) block hash")
					gotConfig = rawdb.ReadChainConfig(db, genesisHash)
					if diff := cmp.Diff(g.Config, gotConfig, cmpBaseConfig); diff != "" {
						t.Errorf("stored base config after restart (-want +got)\n%s", diff)
					}
					if diff := cmp.Diff(g.Config, gotConfig, cmpNetworkUpgrades); diff != "" {
						t.Errorf("stored network upgrades after restart (-want +got)\n%s", diff)
					}
				})
			}
		})
	}
}
