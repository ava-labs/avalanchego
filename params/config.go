// (c) 2019-2020, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2016 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package params

import (
	"math/big"

	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/coreth/utils"
	gethparams "github.com/ava-labs/libevm/params"
)

// Avalanche ChainIDs
var (
	// AvalancheMainnetChainID ...
	AvalancheMainnetChainID = big.NewInt(43114)
	// AvalancheFujiChainID ...
	AvalancheFujiChainID = big.NewInt(43113)
	// AvalancheLocalChainID ...
	AvalancheLocalChainID = big.NewInt(43112)
)

// Guarantees extras initialisation before a call to [ChainConfig.Rules].
var _ = libevmInit()

var (
	TestChainConfig = WithExtra(
		&ChainConfig{
			ChainID:             big.NewInt(1),
			HomesteadBlock:      big.NewInt(0),
			DAOForkBlock:        nil,
			DAOForkSupport:      false,
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
			ShanghaiTime:        utils.TimeToNewUint64(upgrade.GetConfig(constants.UnitTestID).DurangoTime),
			CancunTime:          utils.TimeToNewUint64(upgrade.GetConfig(constants.UnitTestID).EtnaTime),
		},
		&ChainConfigExtra{
			AvalancheContext: AvalancheContext{utils.TestSnowContext()},
			NetworkUpgrades: NetworkUpgrades{
				ApricotPhase1BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase2BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase3BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase4BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase5BlockTimestamp:     utils.NewUint64(0),
				ApricotPhasePre6BlockTimestamp:  utils.NewUint64(0),
				ApricotPhase6BlockTimestamp:     utils.NewUint64(0),
				ApricotPhasePost6BlockTimestamp: utils.NewUint64(0),
				BanffBlockTimestamp:             utils.NewUint64(0),
				CortinaBlockTimestamp:           utils.NewUint64(0),
				DurangoBlockTimestamp:           utils.NewUint64(0),
				EtnaTimestamp:                   utils.NewUint64(0),
			},
		})

	TestLaunchConfig = WithExtra(
		&ChainConfig{
			ChainID:             big.NewInt(1),
			HomesteadBlock:      big.NewInt(0),
			DAOForkBlock:        nil,
			DAOForkSupport:      false,
			EIP150Block:         big.NewInt(0),
			EIP155Block:         big.NewInt(0),
			EIP158Block:         big.NewInt(0),
			ByzantiumBlock:      big.NewInt(0),
			ConstantinopleBlock: big.NewInt(0),
			PetersburgBlock:     big.NewInt(0),
			IstanbulBlock:       big.NewInt(0),
			MuirGlacierBlock:    big.NewInt(0),
		},
		&ChainConfigExtra{
			AvalancheContext: AvalancheContext{utils.TestSnowContext()},
			NetworkUpgrades: NetworkUpgrades{
				ApricotPhase1BlockTimestamp:     nil,
				ApricotPhase2BlockTimestamp:     nil,
				ApricotPhase3BlockTimestamp:     nil,
				ApricotPhase4BlockTimestamp:     nil,
				ApricotPhase5BlockTimestamp:     nil,
				ApricotPhasePre6BlockTimestamp:  nil,
				ApricotPhase6BlockTimestamp:     nil,
				ApricotPhasePost6BlockTimestamp: nil,
				BanffBlockTimestamp:             nil,
				CortinaBlockTimestamp:           nil,
				DurangoBlockTimestamp:           nil,
				EtnaTimestamp:                   nil,
			},
		})

	TestApricotPhase1Config = WithExtra(
		&ChainConfig{
			ChainID:             big.NewInt(1),
			HomesteadBlock:      big.NewInt(0),
			DAOForkBlock:        nil,
			DAOForkSupport:      false,
			EIP150Block:         big.NewInt(0),
			EIP155Block:         big.NewInt(0),
			EIP158Block:         big.NewInt(0),
			ByzantiumBlock:      big.NewInt(0),
			ConstantinopleBlock: big.NewInt(0),
			PetersburgBlock:     big.NewInt(0),
			IstanbulBlock:       big.NewInt(0),
			MuirGlacierBlock:    big.NewInt(0),
		},
		&ChainConfigExtra{
			AvalancheContext: AvalancheContext{utils.TestSnowContext()},
			NetworkUpgrades: NetworkUpgrades{
				ApricotPhase1BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase2BlockTimestamp:     nil,
				ApricotPhase3BlockTimestamp:     nil,
				ApricotPhase4BlockTimestamp:     nil,
				ApricotPhase5BlockTimestamp:     nil,
				ApricotPhasePre6BlockTimestamp:  nil,
				ApricotPhase6BlockTimestamp:     nil,
				ApricotPhasePost6BlockTimestamp: nil,
				BanffBlockTimestamp:             nil,
				CortinaBlockTimestamp:           nil,
				DurangoBlockTimestamp:           nil,
				EtnaTimestamp:                   nil,
			},
		})

	TestApricotPhase2Config = WithExtra(
		&ChainConfig{
			ChainID:             big.NewInt(1),
			HomesteadBlock:      big.NewInt(0),
			DAOForkBlock:        nil,
			DAOForkSupport:      false,
			EIP150Block:         big.NewInt(0),
			EIP155Block:         big.NewInt(0),
			EIP158Block:         big.NewInt(0),
			ByzantiumBlock:      big.NewInt(0),
			ConstantinopleBlock: big.NewInt(0),
			PetersburgBlock:     big.NewInt(0),
			IstanbulBlock:       big.NewInt(0),
			MuirGlacierBlock:    big.NewInt(0),
			BerlinBlock:         big.NewInt(0),
		},
		&ChainConfigExtra{
			AvalancheContext: AvalancheContext{utils.TestSnowContext()},
			NetworkUpgrades: NetworkUpgrades{
				ApricotPhase1BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase2BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase3BlockTimestamp:     nil,
				ApricotPhase4BlockTimestamp:     nil,
				ApricotPhase5BlockTimestamp:     nil,
				ApricotPhasePre6BlockTimestamp:  nil,
				ApricotPhase6BlockTimestamp:     nil,
				ApricotPhasePost6BlockTimestamp: nil,
				BanffBlockTimestamp:             nil,
				CortinaBlockTimestamp:           nil,
				DurangoBlockTimestamp:           nil,
				EtnaTimestamp:                   nil,
			},
		})

	TestApricotPhase3Config = WithExtra(
		&ChainConfig{
			ChainID:             big.NewInt(1),
			HomesteadBlock:      big.NewInt(0),
			DAOForkBlock:        nil,
			DAOForkSupport:      false,
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
		},
		&ChainConfigExtra{
			AvalancheContext: AvalancheContext{utils.TestSnowContext()},
			NetworkUpgrades: NetworkUpgrades{
				ApricotPhase1BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase2BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase3BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase4BlockTimestamp:     nil,
				ApricotPhase5BlockTimestamp:     nil,
				ApricotPhasePre6BlockTimestamp:  nil,
				ApricotPhase6BlockTimestamp:     nil,
				ApricotPhasePost6BlockTimestamp: nil,
				BanffBlockTimestamp:             nil,
				CortinaBlockTimestamp:           nil,
				DurangoBlockTimestamp:           nil,
				EtnaTimestamp:                   nil,
			},
		})

	TestApricotPhase4Config = WithExtra(
		&ChainConfig{
			ChainID:             big.NewInt(1),
			HomesteadBlock:      big.NewInt(0),
			DAOForkBlock:        nil,
			DAOForkSupport:      false,
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
		},
		&ChainConfigExtra{
			AvalancheContext: AvalancheContext{utils.TestSnowContext()},
			NetworkUpgrades: NetworkUpgrades{
				ApricotPhase1BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase2BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase3BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase4BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase5BlockTimestamp:     nil,
				ApricotPhasePre6BlockTimestamp:  nil,
				ApricotPhase6BlockTimestamp:     nil,
				ApricotPhasePost6BlockTimestamp: nil,
				BanffBlockTimestamp:             nil,
				CortinaBlockTimestamp:           nil,
				DurangoBlockTimestamp:           nil,
				EtnaTimestamp:                   nil,
			},
		})

	TestApricotPhase5Config = WithExtra(
		&ChainConfig{
			ChainID:             big.NewInt(1),
			HomesteadBlock:      big.NewInt(0),
			DAOForkBlock:        nil,
			DAOForkSupport:      false,
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
		},
		&ChainConfigExtra{
			AvalancheContext: AvalancheContext{utils.TestSnowContext()},
			NetworkUpgrades: NetworkUpgrades{
				ApricotPhase1BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase2BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase3BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase4BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase5BlockTimestamp:     utils.NewUint64(0),
				ApricotPhasePre6BlockTimestamp:  nil,
				ApricotPhase6BlockTimestamp:     nil,
				ApricotPhasePost6BlockTimestamp: nil,
				BanffBlockTimestamp:             nil,
				CortinaBlockTimestamp:           nil,
				DurangoBlockTimestamp:           nil,
				EtnaTimestamp:                   nil,
			},
		})

	TestApricotPhasePre6Config = WithExtra(
		&ChainConfig{
			ChainID:             big.NewInt(1),
			HomesteadBlock:      big.NewInt(0),
			DAOForkBlock:        nil,
			DAOForkSupport:      false,
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
		},
		&ChainConfigExtra{
			AvalancheContext: AvalancheContext{utils.TestSnowContext()},
			NetworkUpgrades: NetworkUpgrades{
				ApricotPhase1BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase2BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase3BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase4BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase5BlockTimestamp:     utils.NewUint64(0),
				ApricotPhasePre6BlockTimestamp:  utils.NewUint64(0),
				ApricotPhase6BlockTimestamp:     nil,
				ApricotPhasePost6BlockTimestamp: nil,
				BanffBlockTimestamp:             nil,
				CortinaBlockTimestamp:           nil,
				DurangoBlockTimestamp:           nil,
				EtnaTimestamp:                   nil,
			},
		})

	TestApricotPhase6Config = WithExtra(
		&ChainConfig{
			ChainID:             big.NewInt(1),
			HomesteadBlock:      big.NewInt(0),
			DAOForkBlock:        nil,
			DAOForkSupport:      false,
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
		},
		&ChainConfigExtra{
			AvalancheContext: AvalancheContext{utils.TestSnowContext()},
			NetworkUpgrades: NetworkUpgrades{
				ApricotPhase1BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase2BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase3BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase4BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase5BlockTimestamp:     utils.NewUint64(0),
				ApricotPhasePre6BlockTimestamp:  utils.NewUint64(0),
				ApricotPhase6BlockTimestamp:     utils.NewUint64(0),
				ApricotPhasePost6BlockTimestamp: nil,
				BanffBlockTimestamp:             nil,
				CortinaBlockTimestamp:           nil,
				DurangoBlockTimestamp:           nil,
				EtnaTimestamp:                   nil,
			},
		})

	TestApricotPhasePost6Config = WithExtra(
		&ChainConfig{
			ChainID:             big.NewInt(1),
			HomesteadBlock:      big.NewInt(0),
			DAOForkBlock:        nil,
			DAOForkSupport:      false,
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
		},
		&ChainConfigExtra{
			AvalancheContext: AvalancheContext{utils.TestSnowContext()},
			NetworkUpgrades: NetworkUpgrades{
				ApricotPhase1BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase2BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase3BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase4BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase5BlockTimestamp:     utils.NewUint64(0),
				ApricotPhasePre6BlockTimestamp:  utils.NewUint64(0),
				ApricotPhase6BlockTimestamp:     utils.NewUint64(0),
				ApricotPhasePost6BlockTimestamp: utils.NewUint64(0),
				BanffBlockTimestamp:             nil,
				CortinaBlockTimestamp:           nil,
				DurangoBlockTimestamp:           nil,
				EtnaTimestamp:                   nil,
			},
		})

	TestBanffChainConfig = WithExtra(
		&ChainConfig{
			ChainID:             big.NewInt(1),
			HomesteadBlock:      big.NewInt(0),
			DAOForkBlock:        nil,
			DAOForkSupport:      false,
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
		},
		&ChainConfigExtra{
			AvalancheContext: AvalancheContext{utils.TestSnowContext()},
			NetworkUpgrades: NetworkUpgrades{
				ApricotPhase1BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase2BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase3BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase4BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase5BlockTimestamp:     utils.NewUint64(0),
				ApricotPhasePre6BlockTimestamp:  utils.NewUint64(0),
				ApricotPhase6BlockTimestamp:     utils.NewUint64(0),
				ApricotPhasePost6BlockTimestamp: utils.NewUint64(0),
				BanffBlockTimestamp:             utils.NewUint64(0),
				CortinaBlockTimestamp:           nil,
				DurangoBlockTimestamp:           nil,
				EtnaTimestamp:                   nil,
			},
		})

	TestCortinaChainConfig = WithExtra(
		&ChainConfig{
			ChainID:             big.NewInt(1),
			HomesteadBlock:      big.NewInt(0),
			DAOForkBlock:        nil,
			DAOForkSupport:      false,
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
		},
		&ChainConfigExtra{
			AvalancheContext: AvalancheContext{utils.TestSnowContext()},
			NetworkUpgrades: NetworkUpgrades{
				ApricotPhase1BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase2BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase3BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase4BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase5BlockTimestamp:     utils.NewUint64(0),
				ApricotPhasePre6BlockTimestamp:  utils.NewUint64(0),
				ApricotPhase6BlockTimestamp:     utils.NewUint64(0),
				ApricotPhasePost6BlockTimestamp: utils.NewUint64(0),
				BanffBlockTimestamp:             utils.NewUint64(0),
				CortinaBlockTimestamp:           utils.NewUint64(0),
				DurangoBlockTimestamp:           nil,
				EtnaTimestamp:                   nil,
			},
		})

	TestDurangoChainConfig = WithExtra(
		&ChainConfig{
			ChainID:             big.NewInt(1),
			HomesteadBlock:      big.NewInt(0),
			DAOForkBlock:        nil,
			DAOForkSupport:      false,
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
			ShanghaiTime:        utils.NewUint64(0),
		},
		&ChainConfigExtra{
			AvalancheContext: AvalancheContext{utils.TestSnowContext()},
			NetworkUpgrades: NetworkUpgrades{
				ApricotPhase1BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase2BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase3BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase4BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase5BlockTimestamp:     utils.NewUint64(0),
				ApricotPhasePre6BlockTimestamp:  utils.NewUint64(0),
				ApricotPhase6BlockTimestamp:     utils.NewUint64(0),
				ApricotPhasePost6BlockTimestamp: utils.NewUint64(0),
				BanffBlockTimestamp:             utils.NewUint64(0),
				CortinaBlockTimestamp:           utils.NewUint64(0),
				DurangoBlockTimestamp:           utils.NewUint64(0),
				EtnaTimestamp:                   nil,
			},
		})

	TestEtnaChainConfig = WithExtra(
		&ChainConfig{
			ChainID:             big.NewInt(1),
			HomesteadBlock:      big.NewInt(0),
			DAOForkBlock:        nil,
			DAOForkSupport:      false,
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
			ShanghaiTime:        utils.NewUint64(0),
			CancunTime:          utils.NewUint64(0),
		},
		&ChainConfigExtra{
			AvalancheContext: AvalancheContext{utils.TestSnowContext()},
			NetworkUpgrades: NetworkUpgrades{
				ApricotPhase1BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase2BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase3BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase4BlockTimestamp:     utils.NewUint64(0),
				ApricotPhase5BlockTimestamp:     utils.NewUint64(0),
				ApricotPhasePre6BlockTimestamp:  utils.NewUint64(0),
				ApricotPhase6BlockTimestamp:     utils.NewUint64(0),
				ApricotPhasePost6BlockTimestamp: utils.NewUint64(0),
				BanffBlockTimestamp:             utils.NewUint64(0),
				CortinaBlockTimestamp:           utils.NewUint64(0),
				DurangoBlockTimestamp:           utils.NewUint64(0),
				EtnaTimestamp:                   utils.NewUint64(0),
			},
		})

	TestRules = TestChainConfig.Rules(new(big.Int), IsMergeTODO, 0)
)

// ChainConfig is the core config which determines the blockchain settings.
//
// ChainConfig is stored in the database on a per block basis. This means
// that any network, identified by its genesis block, can have its own
// set of configuration options.
type ChainConfig = gethparams.ChainConfig

// Rules wraps ChainConfig and is merely syntactic sugar or can be used for functions
// that do not have or require information about the block.
//
// Rules is a one time interface meaning that it shouldn't be used in between transition
// phases.
type Rules = gethparams.Rules
