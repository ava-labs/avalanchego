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
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/ava-labs/coreth/precompile/modules"
	"github.com/ava-labs/coreth/precompile/precompileconfig"
	"github.com/ava-labs/coreth/utils"
	"github.com/ethereum/go-ethereum/common"
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

var (
	TestChainConfig = &ChainConfig{
		AvalancheContext:    AvalancheContext{utils.TestSnowContext()},
		ChainID:             big.NewInt(1),
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
			FUpgradeTimestamp:               utils.NewUint64(0),
		},
	}

	TestLaunchConfig = &ChainConfig{
		AvalancheContext:    AvalancheContext{utils.TestSnowContext()},
		ChainID:             big.NewInt(1),
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
			FUpgradeTimestamp:               nil,
		},
	}

	TestApricotPhase1Config = &ChainConfig{
		AvalancheContext:    AvalancheContext{utils.TestSnowContext()},
		ChainID:             big.NewInt(1),
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
			FUpgradeTimestamp:               nil,
		},
	}

	TestApricotPhase2Config = &ChainConfig{
		AvalancheContext:    AvalancheContext{utils.TestSnowContext()},
		ChainID:             big.NewInt(1),
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
			FUpgradeTimestamp:               nil,
		},
	}

	TestApricotPhase3Config = &ChainConfig{
		AvalancheContext:    AvalancheContext{utils.TestSnowContext()},
		ChainID:             big.NewInt(1),
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
			FUpgradeTimestamp:               nil,
		},
	}

	TestApricotPhase4Config = &ChainConfig{
		AvalancheContext:    AvalancheContext{utils.TestSnowContext()},
		ChainID:             big.NewInt(1),
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
			FUpgradeTimestamp:               nil,
		},
	}

	TestApricotPhase5Config = &ChainConfig{
		AvalancheContext:    AvalancheContext{utils.TestSnowContext()},
		ChainID:             big.NewInt(1),
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
			FUpgradeTimestamp:               nil,
		},
	}

	TestApricotPhasePre6Config = &ChainConfig{
		AvalancheContext:    AvalancheContext{utils.TestSnowContext()},
		ChainID:             big.NewInt(1),
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
			FUpgradeTimestamp:               nil,
		},
	}

	TestApricotPhase6Config = &ChainConfig{
		AvalancheContext:    AvalancheContext{utils.TestSnowContext()},
		ChainID:             big.NewInt(1),
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
			FUpgradeTimestamp:               nil,
		},
	}

	TestApricotPhasePost6Config = &ChainConfig{
		AvalancheContext:    AvalancheContext{utils.TestSnowContext()},
		ChainID:             big.NewInt(1),
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
			FUpgradeTimestamp:               nil,
		},
	}

	TestBanffChainConfig = &ChainConfig{
		AvalancheContext:    AvalancheContext{utils.TestSnowContext()},
		ChainID:             big.NewInt(1),
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
			FUpgradeTimestamp:               nil,
		},
	}

	TestCortinaChainConfig = &ChainConfig{
		AvalancheContext:    AvalancheContext{utils.TestSnowContext()},
		ChainID:             big.NewInt(1),
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
			FUpgradeTimestamp:               nil,
		},
	}

	TestDurangoChainConfig = &ChainConfig{
		AvalancheContext:    AvalancheContext{utils.TestSnowContext()},
		ChainID:             big.NewInt(1),
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
		ShanghaiTime:        utils.NewUint64(0),
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
			FUpgradeTimestamp:               nil,
		},
	}

	TestEtnaChainConfig = &ChainConfig{
		AvalancheContext:    AvalancheContext{utils.TestSnowContext()},
		ChainID:             big.NewInt(1),
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
		ShanghaiTime:        utils.NewUint64(0),
		CancunTime:          utils.NewUint64(0),
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
			FUpgradeTimestamp:               nil,
		},
	}

	TestFUpgradeChainConfig = &ChainConfig{
		AvalancheContext:    AvalancheContext{utils.TestSnowContext()},
		ChainID:             big.NewInt(1),
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
		ShanghaiTime:        utils.NewUint64(0),
		CancunTime:          utils.NewUint64(0),
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
			FUpgradeTimestamp:               utils.NewUint64(0),
		},
	}

	TestRules = TestChainConfig.Rules(new(big.Int), 0)
)

// ChainConfig is the core config which determines the blockchain settings.
//
// ChainConfig is stored in the database on a per block basis. This means
// that any network, identified by its genesis block, can have its own
// set of configuration options.
type ChainConfig struct {
	ChainID *big.Int `json:"chainId"` // chainId identifies the current chain and is used for replay protection

	HomesteadBlock *big.Int `json:"homesteadBlock,omitempty"` // Homestead switch block (nil = no fork, 0 = already homestead)

	DAOForkBlock   *big.Int `json:"daoForkBlock,omitempty"`   // TheDAO hard-fork switch block (nil = no fork)
	DAOForkSupport bool     `json:"daoForkSupport,omitempty"` // Whether the nodes supports or opposes the DAO hard-fork

	// EIP150 implements the Gas price changes (https://github.com/ethereum/EIPs/issues/150)
	EIP150Block *big.Int `json:"eip150Block,omitempty"` // EIP150 HF block (nil = no fork)
	EIP155Block *big.Int `json:"eip155Block,omitempty"` // EIP155 HF block
	EIP158Block *big.Int `json:"eip158Block,omitempty"` // EIP158 HF block

	ByzantiumBlock      *big.Int `json:"byzantiumBlock,omitempty"`      // Byzantium switch block (nil = no fork, 0 = already on byzantium)
	ConstantinopleBlock *big.Int `json:"constantinopleBlock,omitempty"` // Constantinople switch block (nil = no fork, 0 = already activated)
	PetersburgBlock     *big.Int `json:"petersburgBlock,omitempty"`     // Petersburg switch block (nil = same as Constantinople)
	IstanbulBlock       *big.Int `json:"istanbulBlock,omitempty"`       // Istanbul switch block (nil = no fork, 0 = already on istanbul)
	MuirGlacierBlock    *big.Int `json:"muirGlacierBlock,omitempty"`    // Eip-2384 (bomb delay) switch block (nil = no fork, 0 = already activated)
	BerlinBlock         *big.Int `json:"berlinBlock,omitempty"`         // Berlin switch block (nil = no fork, 0 = already on berlin)
	LondonBlock         *big.Int `json:"londonBlock,omitempty"`         // London switch block (nil = no fork, 0 = already on london)

	// Fork scheduling was switched from blocks to timestamps here

	ShanghaiTime *uint64 `json:"shanghaiTime,omitempty"` // Shanghai switch time (nil = no fork, 0 = already on shanghai)
	CancunTime   *uint64 `json:"cancunTime,omitempty"`   // Cancun switch time (nil = no fork, 0 = already activated)
	VerkleTime   *uint64 `json:"verkleTime,omitempty"`   // Verkle switch time (nil = no fork, 0 = already on verkle)

	NetworkUpgrades // Config for timestamps that enable network upgrades. Skip encoding/decoding directly into ChainConfig.

	AvalancheContext `json:"-"` // Avalanche specific context set during VM initialization. Not serialized.

	UpgradeConfig `json:"-"` // Config specified in upgradeBytes (avalanche network upgrades or enable/disabling precompiles). Skip encoding/decoding directly into ChainConfig.
}

// Description returns a human-readable description of ChainConfig.
func (c *ChainConfig) Description() string {
	var banner string

	banner += fmt.Sprintf("Chain ID:  %v\n", c.ChainID)
	banner += "Consensus: Dummy Consensus Engine\n\n"

	// Create a list of forks with a short description of them. Forks that only
	// makes sense for mainnet should be optional at printing to avoid bloating
	// the output for testnets and private networks.
	banner += "Hard Forks (block based):\n"
	banner += fmt.Sprintf(" - Homestead:                   #%-8v (https://github.com/ethereum/execution-specs/blob/master/network-upgrades/mainnet-upgrades/homestead.md)\n", c.HomesteadBlock)
	if c.DAOForkBlock != nil {
		banner += fmt.Sprintf(" - DAO Fork:                    #%-8v (https://github.com/ethereum/execution-specs/blob/master/network-upgrades/mainnet-upgrades/dao-fork.md)\n", c.DAOForkBlock)
	}
	banner += fmt.Sprintf(" - Tangerine Whistle (EIP 150): #%-8v (https://github.com/ethereum/execution-specs/blob/master/network-upgrades/mainnet-upgrades/tangerine-whistle.md)\n", c.EIP150Block)
	banner += fmt.Sprintf(" - Spurious Dragon/1 (EIP 155): #%-8v (https://github.com/ethereum/execution-specs/blob/master/network-upgrades/mainnet-upgrades/spurious-dragon.md)\n", c.EIP155Block)
	banner += fmt.Sprintf(" - Spurious Dragon/2 (EIP 158): #%-8v (https://github.com/ethereum/execution-specs/blob/master/network-upgrades/mainnet-upgrades/spurious-dragon.md)\n", c.EIP155Block)
	banner += fmt.Sprintf(" - Byzantium:                   #%-8v (https://github.com/ethereum/execution-specs/blob/master/network-upgrades/mainnet-upgrades/byzantium.md)\n", c.ByzantiumBlock)
	banner += fmt.Sprintf(" - Constantinople:              #%-8v (https://github.com/ethereum/execution-specs/blob/master/network-upgrades/mainnet-upgrades/constantinople.md)\n", c.ConstantinopleBlock)
	banner += fmt.Sprintf(" - Petersburg:                  #%-8v (https://github.com/ethereum/execution-specs/blob/master/network-upgrades/mainnet-upgrades/petersburg.md)\n", c.PetersburgBlock)
	banner += fmt.Sprintf(" - Istanbul:                    #%-8v (https://github.com/ethereum/execution-specs/blob/master/network-upgrades/mainnet-upgrades/istanbul.md)\n", c.IstanbulBlock)
	if c.MuirGlacierBlock != nil {
		banner += fmt.Sprintf(" - Muir Glacier:                #%-8v (https://github.com/ethereum/execution-specs/blob/master/network-upgrades/mainnet-upgrades/muir-glacier.md)\n", c.MuirGlacierBlock)
	}

	banner += "Hard forks (timestamp based):\n"
	banner += fmt.Sprintf(" - Cancun Timestamp:              @%-10v (https://github.com/ethereum/execution-specs/blob/master/network-upgrades/mainnet-upgrades/cancun.md)\n", ptrToString(c.CancunTime))
	banner += fmt.Sprintf(" - Verkle Timestamp:              @%-10v", ptrToString(c.VerkleTime))
	banner += "\n"

	banner += "Avalanche Upgrades (timestamp based):\n"
	banner += c.NetworkUpgrades.Description()
	banner += "\n"

	upgradeConfigBytes, err := json.Marshal(c.UpgradeConfig)
	if err != nil {
		upgradeConfigBytes = []byte("cannot marshal UpgradeConfig")
	}
	banner += fmt.Sprintf("Upgrade Config: %s", string(upgradeConfigBytes))
	banner += "\n"
	return banner
}

// IsHomestead returns whether num is either equal to the homestead block or greater.
func (c *ChainConfig) IsHomestead(num *big.Int) bool {
	return isBlockForked(c.HomesteadBlock, num)
}

// IsDAOFork returns whether num is either equal to the DAO fork block or greater.
func (c *ChainConfig) IsDAOFork(num *big.Int) bool {
	return isBlockForked(c.DAOForkBlock, num)
}

// IsEIP150 returns whether num is either equal to the EIP150 fork block or greater.
func (c *ChainConfig) IsEIP150(num *big.Int) bool {
	return isBlockForked(c.EIP150Block, num)
}

// IsEIP155 returns whether num is either equal to the EIP155 fork block or greater.
func (c *ChainConfig) IsEIP155(num *big.Int) bool {
	return isBlockForked(c.EIP155Block, num)
}

// IsEIP158 returns whether num is either equal to the EIP158 fork block or greater.
func (c *ChainConfig) IsEIP158(num *big.Int) bool {
	return isBlockForked(c.EIP158Block, num)
}

// IsByzantium returns whether num is either equal to the Byzantium fork block or greater.
func (c *ChainConfig) IsByzantium(num *big.Int) bool {
	return isBlockForked(c.ByzantiumBlock, num)
}

// IsConstantinople returns whether num is either equal to the Constantinople fork block or greater.
func (c *ChainConfig) IsConstantinople(num *big.Int) bool {
	return isBlockForked(c.ConstantinopleBlock, num)
}

// IsMuirGlacier returns whether num is either equal to the Muir Glacier (EIP-2384) fork block or greater.
func (c *ChainConfig) IsMuirGlacier(num *big.Int) bool {
	return isBlockForked(c.MuirGlacierBlock, num)
}

// IsPetersburg returns whether num is either
// - equal to or greater than the PetersburgBlock fork block,
// - OR is nil, and Constantinople is active
func (c *ChainConfig) IsPetersburg(num *big.Int) bool {
	return isBlockForked(c.PetersburgBlock, num) || c.PetersburgBlock == nil && isBlockForked(c.ConstantinopleBlock, num)
}

// IsIstanbul returns whether num is either equal to the Istanbul fork block or greater.
func (c *ChainConfig) IsIstanbul(num *big.Int) bool {
	return isBlockForked(c.IstanbulBlock, num)
}

// IsBerlin returns whether num is either equal to the Berlin fork block or greater.
func (c *ChainConfig) IsBerlin(num *big.Int) bool {
	return isBlockForked(c.BerlinBlock, num)
}

// IsLondon returns whether num is either equal to the London fork block or greater.
func (c *ChainConfig) IsLondon(num *big.Int) bool {
	return isBlockForked(c.LondonBlock, num)
}

// IsShanghai returns whether time is either equal to the Shanghai fork time or greater.
func (c *ChainConfig) IsShanghai(num *big.Int, time uint64) bool {
	return c.IsLondon(num) && isTimestampForked(c.ShanghaiTime, time)
}

// IsCancun returns whether num is either equal to the Cancun fork time or greater.
func (c *ChainConfig) IsCancun(num *big.Int, time uint64) bool {
	return c.IsLondon(num) && isTimestampForked(c.CancunTime, time)
}

// IsVerkle returns whether num is either equal to the Verkle fork time or greater.
func (c *ChainConfig) IsVerkle(num *big.Int, time uint64) bool {
	return c.IsLondon(num) && isTimestampForked(c.VerkleTime, time)
}

// CheckCompatible checks whether scheduled fork transitions have been imported
// with a mismatching chain configuration.
func (c *ChainConfig) CheckCompatible(newcfg *ChainConfig, height uint64, time uint64) *ConfigCompatError {
	var (
		bhead = new(big.Int).SetUint64(height)
		btime = time
	)
	// Iterate checkCompatible to find the lowest conflict.
	var lasterr *ConfigCompatError
	for {
		err := c.checkCompatible(newcfg, bhead, btime)
		if err == nil || (lasterr != nil && err.RewindToBlock == lasterr.RewindToBlock && err.RewindToTime == lasterr.RewindToTime) {
			break
		}
		lasterr = err

		if err.RewindToTime > 0 {
			btime = err.RewindToTime
		} else {
			bhead.SetUint64(err.RewindToBlock)
		}
	}
	return lasterr
}

type fork struct {
	name      string
	block     *big.Int // some go-ethereum forks use block numbers
	timestamp *uint64  // Avalanche forks use timestamps
	optional  bool     // if true, the fork may be nil and next fork is still allowed
}

// CheckConfigForkOrder checks that we don't "skip" any forks, geth isn't pluggable enough
// to guarantee that forks can be implemented in a different order than on official networks
func (c *ChainConfig) CheckConfigForkOrder() error {
	ethForks := []fork{
		{name: "homesteadBlock", block: c.HomesteadBlock},
		{name: "daoForkBlock", block: c.DAOForkBlock, optional: true},
		{name: "eip150Block", block: c.EIP150Block},
		{name: "eip155Block", block: c.EIP155Block},
		{name: "eip158Block", block: c.EIP158Block},
		{name: "byzantiumBlock", block: c.ByzantiumBlock},
		{name: "constantinopleBlock", block: c.ConstantinopleBlock},
		{name: "petersburgBlock", block: c.PetersburgBlock},
		{name: "istanbulBlock", block: c.IstanbulBlock},
		{name: "muirGlacierBlock", block: c.MuirGlacierBlock, optional: true},
		{name: "berlinBlock", block: c.BerlinBlock},
		{name: "londonBlock", block: c.LondonBlock},
		{name: "shanghaiTime", timestamp: c.ShanghaiTime},
		{name: "cancunTime", timestamp: c.CancunTime, optional: true},
		{name: "verkleTime", timestamp: c.VerkleTime, optional: true},
	}

	// Check that forks are enabled in order
	if err := checkForks(ethForks, true); err != nil {
		return err
	}

	// Note: In Avalanche, hard forks must take place via block timestamps instead
	// of block numbers since blocks are produced asynchronously. Therefore, we do not
	// check that the block timestamps in the same way as for
	// the block number forks since it would not be a meaningful comparison.
	// Instead, we check only that Phases are enabled in order.
	// Note: we do not add the optional stateful precompile configs in here because they are optional
	// and independent, such that the ordering they are enabled does not impact the correctness of the
	// chain config.
	if err := checkForks(c.forkOrder(), false); err != nil {
		return err
	}

	return nil
}

// checkForks checks that forks are enabled in order and returns an error if not
// [blockFork] is true if the fork is a block number fork, false if it is a timestamp fork
func checkForks(forks []fork, blockFork bool) error {
	lastFork := fork{}
	for _, cur := range forks {
		if lastFork.name != "" {
			switch {
			// Non-optional forks must all be present in the chain config up to the last defined fork
			case lastFork.block == nil && lastFork.timestamp == nil && (cur.block != nil || cur.timestamp != nil):
				if cur.block != nil {
					return fmt.Errorf("unsupported fork ordering: %v not enabled, but %v enabled at block %v",
						lastFork.name, cur.name, cur.block)
				} else {
					return fmt.Errorf("unsupported fork ordering: %v not enabled, but %v enabled at timestamp %v",
						lastFork.name, cur.name, cur.timestamp)
				}

			// Fork (whether defined by block or timestamp) must follow the fork definition sequence
			case (lastFork.block != nil && cur.block != nil) || (lastFork.timestamp != nil && cur.timestamp != nil):
				if lastFork.block != nil && lastFork.block.Cmp(cur.block) > 0 {
					return fmt.Errorf("unsupported fork ordering: %v enabled at block %v, but %v enabled at block %v",
						lastFork.name, lastFork.block, cur.name, cur.block)
				} else if lastFork.timestamp != nil && *lastFork.timestamp > *cur.timestamp {
					return fmt.Errorf("unsupported fork ordering: %v enabled at timestamp %v, but %v enabled at timestamp %v",
						lastFork.name, lastFork.timestamp, cur.name, cur.timestamp)
				}

				// Timestamp based forks can follow block based ones, but not the other way around
				if lastFork.timestamp != nil && cur.block != nil {
					return fmt.Errorf("unsupported fork ordering: %v used timestamp ordering, but %v reverted to block ordering",
						lastFork.name, cur.name)
				}
			}
		}
		// If it was optional and not set, then ignore it
		if !cur.optional || (cur.block != nil || cur.timestamp != nil) {
			lastFork = cur
		}
	}
	return nil
}

// checkCompatible confirms that [newcfg] is backwards compatible with [c] to upgrade with the given head block height and timestamp.
// This confirms that all Ethereum and Avalanche upgrades are backwards compatible as well as that the precompile config is backwards
// compatible.
func (c *ChainConfig) checkCompatible(newcfg *ChainConfig, headNumber *big.Int, headTimestamp uint64) *ConfigCompatError {
	if isForkBlockIncompatible(c.HomesteadBlock, newcfg.HomesteadBlock, headNumber) {
		return newBlockCompatError("Homestead fork block", c.HomesteadBlock, newcfg.HomesteadBlock)
	}
	if isForkBlockIncompatible(c.DAOForkBlock, newcfg.DAOForkBlock, headNumber) {
		return newBlockCompatError("DAO fork block", c.DAOForkBlock, newcfg.DAOForkBlock)
	}
	if c.IsDAOFork(headNumber) && c.DAOForkSupport != newcfg.DAOForkSupport {
		return newBlockCompatError("DAO fork support flag", c.DAOForkBlock, newcfg.DAOForkBlock)
	}
	if isForkBlockIncompatible(c.EIP150Block, newcfg.EIP150Block, headNumber) {
		return newBlockCompatError("EIP150 fork block", c.EIP150Block, newcfg.EIP150Block)
	}
	if isForkBlockIncompatible(c.EIP155Block, newcfg.EIP155Block, headNumber) {
		return newBlockCompatError("EIP155 fork block", c.EIP155Block, newcfg.EIP155Block)
	}
	if isForkBlockIncompatible(c.EIP158Block, newcfg.EIP158Block, headNumber) {
		return newBlockCompatError("EIP158 fork block", c.EIP158Block, newcfg.EIP158Block)
	}
	if c.IsEIP158(headNumber) && !configBlockEqual(c.ChainID, newcfg.ChainID) {
		return newBlockCompatError("EIP158 chain ID", c.EIP158Block, newcfg.EIP158Block)
	}
	if isForkBlockIncompatible(c.ByzantiumBlock, newcfg.ByzantiumBlock, headNumber) {
		return newBlockCompatError("Byzantium fork block", c.ByzantiumBlock, newcfg.ByzantiumBlock)
	}
	if isForkBlockIncompatible(c.ConstantinopleBlock, newcfg.ConstantinopleBlock, headNumber) {
		return newBlockCompatError("Constantinople fork block", c.ConstantinopleBlock, newcfg.ConstantinopleBlock)
	}
	if isForkBlockIncompatible(c.PetersburgBlock, newcfg.PetersburgBlock, headNumber) {
		// the only case where we allow Petersburg to be set in the past is if it is equal to Constantinople
		// mainly to satisfy fork ordering requirements which state that Petersburg fork be set if Constantinople fork is set
		if isForkBlockIncompatible(c.ConstantinopleBlock, newcfg.PetersburgBlock, headNumber) {
			return newBlockCompatError("Petersburg fork block", c.PetersburgBlock, newcfg.PetersburgBlock)
		}
	}
	if isForkBlockIncompatible(c.IstanbulBlock, newcfg.IstanbulBlock, headNumber) {
		return newBlockCompatError("Istanbul fork block", c.IstanbulBlock, newcfg.IstanbulBlock)
	}
	if isForkBlockIncompatible(c.MuirGlacierBlock, newcfg.MuirGlacierBlock, headNumber) {
		return newBlockCompatError("Muir Glacier fork block", c.MuirGlacierBlock, newcfg.MuirGlacierBlock)
	}
	if isForkBlockIncompatible(c.BerlinBlock, newcfg.BerlinBlock, headNumber) {
		return newBlockCompatError("Berlin fork block", c.BerlinBlock, newcfg.BerlinBlock)
	}
	if isForkBlockIncompatible(c.LondonBlock, newcfg.LondonBlock, headNumber) {
		return newBlockCompatError("London fork block", c.LondonBlock, newcfg.LondonBlock)
	}
	if isForkTimestampIncompatible(c.ShanghaiTime, newcfg.ShanghaiTime, headTimestamp) {
		return newTimestampCompatError("Shanghai fork timestamp", c.ShanghaiTime, newcfg.ShanghaiTime)
	}
	if isForkTimestampIncompatible(c.CancunTime, newcfg.CancunTime, headTimestamp) {
		return newTimestampCompatError("Cancun fork timestamp", c.CancunTime, newcfg.CancunTime)
	}
	if isForkTimestampIncompatible(c.VerkleTime, newcfg.VerkleTime, headTimestamp) {
		return newTimestampCompatError("Verkle fork timestamp", c.VerkleTime, newcfg.VerkleTime)
	}

	// Check avalanche network upgrades
	if err := c.CheckNetworkUpgradesCompatible(&newcfg.NetworkUpgrades, headTimestamp); err != nil {
		return err
	}
	return nil
}

// isForkBlockIncompatible returns true if a fork scheduled at block s1 cannot be
// rescheduled to block s2 because head is already past the fork.
func isForkBlockIncompatible(s1, s2, head *big.Int) bool {
	return (isBlockForked(s1, head) || isBlockForked(s2, head)) && !configBlockEqual(s1, s2)
}

// isBlockForked returns whether a fork scheduled at block s is active at the
// given head block. Whilst this method is the same as isTimestampForked, they
// are explicitly separate for clearer reading.
func isBlockForked(s, head *big.Int) bool {
	if s == nil || head == nil {
		return false
	}
	return s.Cmp(head) <= 0
}

func configBlockEqual(x, y *big.Int) bool {
	if x == nil {
		return y == nil
	}
	if y == nil {
		return x == nil
	}
	return x.Cmp(y) == 0
}

// isForkTimestampIncompatible returns true if a fork scheduled at timestamp s1
// cannot be rescheduled to timestamp s2 because head is already past the fork.
func isForkTimestampIncompatible(s1, s2 *uint64, head uint64) bool {
	return (isTimestampForked(s1, head) || isTimestampForked(s2, head)) && !configTimestampEqual(s1, s2)
}

// isTimestampForked returns whether a fork scheduled at timestamp s is active
// at the given head timestamp. Whilst this method is the same as isBlockForked,
// they are explicitly separate for clearer reading.
func isTimestampForked(s *uint64, head uint64) bool {
	if s == nil {
		return false
	}
	return *s <= head
}

func configTimestampEqual(x, y *uint64) bool {
	if x == nil {
		return y == nil
	}
	if y == nil {
		return x == nil
	}
	return *x == *y
}

// ConfigCompatError is raised if the locally-stored blockchain is initialised with a
// ChainConfig that would alter the past.
type ConfigCompatError struct {
	What string

	// block numbers of the stored and new configurations if block based forking
	StoredBlock, NewBlock *big.Int

	// timestamps of the stored and new configurations if time based forking
	StoredTime, NewTime *uint64

	// the block number to which the local chain must be rewound to correct the error
	RewindToBlock uint64

	// the timestamp to which the local chain must be rewound to correct the error
	RewindToTime uint64
}

func newBlockCompatError(what string, storedblock, newblock *big.Int) *ConfigCompatError {
	var rew *big.Int
	switch {
	case storedblock == nil:
		rew = newblock
	case newblock == nil || storedblock.Cmp(newblock) < 0:
		rew = storedblock
	default:
		rew = newblock
	}
	err := &ConfigCompatError{
		What:          what,
		StoredBlock:   storedblock,
		NewBlock:      newblock,
		RewindToBlock: 0,
	}
	if rew != nil && rew.Sign() > 0 {
		err.RewindToBlock = rew.Uint64() - 1
	}
	return err
}

func newTimestampCompatError(what string, storedtime, newtime *uint64) *ConfigCompatError {
	var rew *uint64
	switch {
	case storedtime == nil:
		rew = newtime
	case newtime == nil || *storedtime < *newtime:
		rew = storedtime
	default:
		rew = newtime
	}
	err := &ConfigCompatError{
		What:         what,
		StoredTime:   storedtime,
		NewTime:      newtime,
		RewindToTime: 0,
	}
	if rew != nil && *rew > 0 {
		err.RewindToTime = *rew - 1
	}
	return err
}

func (err *ConfigCompatError) Error() string {
	if err.StoredBlock != nil {
		return fmt.Sprintf("mismatching %s in database (have block %d, want block %d, rewindto block %d)", err.What, err.StoredBlock, err.NewBlock, err.RewindToBlock)
	}
	return fmt.Sprintf("mismatching %s in database (have timestamp %s, want timestamp %s, rewindto timestamp %d)", err.What, ptrToString(err.StoredTime), ptrToString(err.NewTime), err.RewindToTime)
}

type EthRules struct {
	IsHomestead, IsEIP150, IsEIP155, IsEIP158               bool
	IsByzantium, IsConstantinople, IsPetersburg, IsIstanbul bool
	IsCancun                                                bool
	IsVerkle                                                bool
}

// Rules wraps ChainConfig and is merely syntactic sugar or can be used for functions
// that do not have or require information about the block.
//
// Rules is a one time interface meaning that it shouldn't be used in between transition
// phases.
type Rules struct {
	ChainID *big.Int

	// Rules for Ethereum releases
	EthRules

	// Rules for Avalanche releases
	AvalancheRules

	// ActivePrecompiles maps addresses to stateful precompiled contracts that are enabled
	// for this rule set.
	// Note: none of these addresses should conflict with the address space used by
	// any existing precompiles.
	ActivePrecompiles map[common.Address]precompileconfig.Config
	// Predicaters maps addresses to stateful precompile Predicaters
	// that are enabled for this rule set.
	Predicaters map[common.Address]precompileconfig.Predicater
	// AccepterPrecompiles map addresses to stateful precompile accepter functions
	// that are enabled for this rule set.
	AccepterPrecompiles map[common.Address]precompileconfig.Accepter
}

// Rules ensures c's ChainID is not nil.
func (c *ChainConfig) rules(num *big.Int, timestamp uint64) Rules {
	chainID := c.ChainID
	if chainID == nil {
		chainID = new(big.Int)
	}
	return Rules{
		ChainID: new(big.Int).Set(chainID),
		EthRules: EthRules{
			IsHomestead:      c.IsHomestead(num),
			IsEIP150:         c.IsEIP150(num),
			IsEIP155:         c.IsEIP155(num),
			IsEIP158:         c.IsEIP158(num),
			IsByzantium:      c.IsByzantium(num),
			IsConstantinople: c.IsConstantinople(num),
			IsPetersburg:     c.IsPetersburg(num),
			IsIstanbul:       c.IsIstanbul(num),
			IsCancun:         c.IsCancun(num, timestamp),
		},
	}
}

// Rules returns the Avalanche modified rules to support Avalanche
// network upgrades
func (c *ChainConfig) Rules(blockNum *big.Int, timestamp uint64) Rules {
	rules := c.rules(blockNum, timestamp)

	rules.AvalancheRules = c.GetAvalancheRules(timestamp)

	// Initialize the stateful precompiles that should be enabled at [blockTimestamp].
	rules.ActivePrecompiles = make(map[common.Address]precompileconfig.Config)
	rules.Predicaters = make(map[common.Address]precompileconfig.Predicater)
	rules.AccepterPrecompiles = make(map[common.Address]precompileconfig.Accepter)
	for _, module := range modules.RegisteredModules() {
		if config := c.getActivePrecompileConfig(module.Address, timestamp); config != nil && !config.IsDisabled() {
			rules.ActivePrecompiles[module.Address] = config
			if predicater, ok := config.(precompileconfig.Predicater); ok {
				rules.Predicaters[module.Address] = predicater
			}
			if precompileAccepter, ok := config.(precompileconfig.Accepter); ok {
				rules.AccepterPrecompiles[module.Address] = precompileAccepter
			}
		}
	}

	return rules
}
