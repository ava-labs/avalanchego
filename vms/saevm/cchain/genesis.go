// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/ava-labs/avalanchego/graft/coreth/core"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/snow"
)

// parseGenesis decodes the genesis bytes and populates the upgrade schedule.
func parseGenesis(ctx *snow.Context, b []byte) (*core.Genesis, error) {
	var g core.Genesis
	if err := json.Unmarshal(b, &g); err != nil {
		return nil, fmt.Errorf("unmarshalling genesis: %w", err)
	}

	// The JSON only specifies the chain-specific configuration; the upgrade
	// schedule is configured by ctx.
	var chainID *big.Int
	if c := g.Config; c != nil {
		chainID = c.ChainID
	}
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
