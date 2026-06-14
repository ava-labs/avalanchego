// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/triedb"

	"github.com/ava-labs/avalanchego/graft/coreth/core"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/snow"
)

var errNoChainID = errors.New("genesis must specify a chain ID")

// parseGenesis decodes the genesis bytes and populates the upgrade schedule.
func parseGenesis(ctx *snow.Context, b []byte) (*core.Genesis, error) {
	var g core.Genesis
	if err := json.Unmarshal(b, &g); err != nil {
		return nil, fmt.Errorf("unmarshalling genesis: %w", err)
	}

	// The JSON only specifies the chain-specific configuration; the upgrade
	// schedule is configured by ctx.
	if g.Config == nil || g.Config.ChainID == nil {
		return nil, errNoChainID
	}
	chainID := g.Config.ChainID
	u := &ctx.NetworkUpgrades
	g.Config = params.WithExtra(
		&params.ChainConfig{
			ChainID:             chainID,
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
			BerlinBlock:         big.NewInt(berlinBlock(chainID)),
			LondonBlock:         big.NewInt(londonBlock(chainID)),
			ShanghaiTime:        utils.TimeToNewUint64(u.DurangoTime),
			CancunTime:          utils.TimeToNewUint64(u.EtnaTime),
		},
		&extras.ChainConfig{
			NetworkUpgrades: extras.NetworkUpgrades{
				ApricotPhase1BlockTimestamp:     utils.TimeToNewUint64(u.ApricotPhase1Time),
				ApricotPhase2BlockTimestamp:     utils.TimeToNewUint64(u.ApricotPhase2Time),
				ApricotPhase3BlockTimestamp:     utils.TimeToNewUint64(u.ApricotPhase3Time),
				ApricotPhase4BlockTimestamp:     utils.TimeToNewUint64(u.ApricotPhase4Time),
				ApricotPhase5BlockTimestamp:     utils.TimeToNewUint64(u.ApricotPhase5Time),
				ApricotPhasePre6BlockTimestamp:  utils.TimeToNewUint64(u.ApricotPhasePre6Time),
				ApricotPhase6BlockTimestamp:     utils.TimeToNewUint64(u.ApricotPhase6Time),
				ApricotPhasePost6BlockTimestamp: utils.TimeToNewUint64(u.ApricotPhasePost6Time),
				BanffBlockTimestamp:             utils.TimeToNewUint64(u.BanffTime),
				CortinaBlockTimestamp:           utils.TimeToNewUint64(u.CortinaTime),
				DurangoBlockTimestamp:           utils.TimeToNewUint64(u.DurangoTime),
				EtnaTimestamp:                   utils.TimeToNewUint64(u.EtnaTime),
				FortunaTimestamp:                utils.TimeToNewUint64(u.FortunaTime),
				GraniteTimestamp:                utils.TimeToNewUint64(u.GraniteTime),
				HeliconTimestamp:                utils.TimeToNewUint64(u.HeliconTime),
			},
			AvalancheContext: extras.AvalancheContext{
				SnowCtx: ctx,
			},
			UpgradeConfig: extras.UpgradeConfig{
				PrecompileUpgrades: []extras.PrecompileUpgrade{
					{
						Config: warp.NewDefaultConfig(
							utils.TimeToNewUint64(u.DurangoTime),
						),
					},
				},
			},
		},
	)
	return &g, nil
}

var (
	mainnetChainID = big.NewInt(43114)
	fujiChainID    = big.NewInt(43113)
)

func berlinBlock(chainID *big.Int) int64 {
	switch {
	case utils.BigEqual(chainID, mainnetChainID):
		return 1640340 // https://snowtrace.io/block/1640340?chainid=43114, AP2 activation block
	case utils.BigEqual(chainID, fujiChainID):
		return 184985 // https://testnet.snowtrace.io/block/184985?chainid=43113, AP2 activation block
	default:
		return 0
	}
}

func londonBlock(chainID *big.Int) int64 {
	switch {
	case utils.BigEqual(chainID, mainnetChainID):
		return 3308552 // https://snowtrace.io/block/3308552?chainid=43114, AP3 activation block
	case utils.BigEqual(chainID, fujiChainID):
		return 805078 // https://testnet.snowtrace.io/block/805078?chainid=43113, AP3 activation block
	default:
		return 0
	}
}

// writeGenesis commits the genesis block and chain config to db if the chain is
// not already initialized. Otherwise it verifies that the stored genesis hash
// matches and overwrites the stored chain config, which may schedule upgrades
// that were not previously scheduled. It returns the genesis block.
func writeGenesis(
	db ethdb.Database,
	tdb *triedb.Database,
	genesis *core.Genesis,
) (*types.Block, error) {
	// TODO(StephenButtolph): While we are guaranteed for the trie write to have
	// finished before writing the canonical hash to disk with hashdb or pathdb,
	// firewood has separate synchronization guarantees. It could be possible
	// for the genesis hash to be canonical without the state being on disk.
	stored := rawdb.ReadCanonicalHash(db, 0)
	if (stored == common.Hash{}) {
		return genesis.Commit(db, tdb)
	}

	block := genesis.ToBlock()
	if hash := block.Hash(); hash != stored {
		return nil, &core.GenesisMismatchError{
			Stored: stored,
			New:    hash,
		}
	}

	// TODO(StephenButtolph): Consider checking compatibility of the chain
	// config against the last accepted block.
	rawdb.WriteChainConfig(db, stored, genesis.Config)
	return block, nil
}
