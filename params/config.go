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
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/coreth/precompile/modules"
	"github.com/ava-labs/coreth/precompile/precompileconfig"
	"github.com/ava-labs/coreth/utils"
	"github.com/ethereum/go-ethereum/common"
)

const maxJSONLen = 64 * 1024 * 1024 // 64MB

// Avalanche ChainIDs
var (
	// AvalancheMainnetChainID ...
	AvalancheMainnetChainID = big.NewInt(43114)
	// AvalancheFujiChainID ...
	AvalancheFujiChainID = big.NewInt(43113)
	// AvalancheLocalChainID ...
	AvalancheLocalChainID = big.NewInt(43112)

	errNonGenesisForkByHeight = errors.New("coreth only supports forking by height at the genesis block")
)

var (
	// AvalancheMainnetChainConfig is the configuration for Avalanche Main Network
	AvalancheMainnetChainConfig = getChainConfig(constants.MainnetID, AvalancheMainnetChainID)

	// AvalancheFujiChainConfig is the configuration for the Fuji Test Network
	AvalancheFujiChainConfig = getChainConfig(constants.FujiID, AvalancheFujiChainID)

	// AvalancheLocalChainConfig is the configuration for the Avalanche Local Network
	AvalancheLocalChainConfig = getChainConfig(constants.LocalID, AvalancheLocalChainID)

	TestChainConfig = &ChainConfig{
		AvalancheContext:                AvalancheContext{utils.TestSnowContext()},
		ChainID:                         big.NewInt(1),
		HomesteadBlock:                  big.NewInt(0),
		DAOForkBlock:                    nil,
		DAOForkSupport:                  false,
		EIP150Block:                     big.NewInt(0),
		EIP155Block:                     big.NewInt(0),
		EIP158Block:                     big.NewInt(0),
		ByzantiumBlock:                  big.NewInt(0),
		ConstantinopleBlock:             big.NewInt(0),
		PetersburgBlock:                 big.NewInt(0),
		IstanbulBlock:                   big.NewInt(0),
		MuirGlacierBlock:                big.NewInt(0),
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
	}

	TestLaunchConfig = &ChainConfig{
		AvalancheContext:                AvalancheContext{utils.TestSnowContext()},
		ChainID:                         big.NewInt(1),
		HomesteadBlock:                  big.NewInt(0),
		DAOForkBlock:                    nil,
		DAOForkSupport:                  false,
		EIP150Block:                     big.NewInt(0),
		EIP155Block:                     big.NewInt(0),
		EIP158Block:                     big.NewInt(0),
		ByzantiumBlock:                  big.NewInt(0),
		ConstantinopleBlock:             big.NewInt(0),
		PetersburgBlock:                 big.NewInt(0),
		IstanbulBlock:                   big.NewInt(0),
		MuirGlacierBlock:                big.NewInt(0),
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
	}

	TestApricotPhase1Config = &ChainConfig{
		AvalancheContext:                AvalancheContext{utils.TestSnowContext()},
		ChainID:                         big.NewInt(1),
		HomesteadBlock:                  big.NewInt(0),
		DAOForkBlock:                    nil,
		DAOForkSupport:                  false,
		EIP150Block:                     big.NewInt(0),
		EIP155Block:                     big.NewInt(0),
		EIP158Block:                     big.NewInt(0),
		ByzantiumBlock:                  big.NewInt(0),
		ConstantinopleBlock:             big.NewInt(0),
		PetersburgBlock:                 big.NewInt(0),
		IstanbulBlock:                   big.NewInt(0),
		MuirGlacierBlock:                big.NewInt(0),
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
	}

	TestApricotPhase2Config = &ChainConfig{
		AvalancheContext:                AvalancheContext{utils.TestSnowContext()},
		ChainID:                         big.NewInt(1),
		HomesteadBlock:                  big.NewInt(0),
		DAOForkBlock:                    nil,
		DAOForkSupport:                  false,
		EIP150Block:                     big.NewInt(0),
		EIP155Block:                     big.NewInt(0),
		EIP158Block:                     big.NewInt(0),
		ByzantiumBlock:                  big.NewInt(0),
		ConstantinopleBlock:             big.NewInt(0),
		PetersburgBlock:                 big.NewInt(0),
		IstanbulBlock:                   big.NewInt(0),
		MuirGlacierBlock:                big.NewInt(0),
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
	}

	TestApricotPhase3Config = &ChainConfig{
		AvalancheContext:                AvalancheContext{utils.TestSnowContext()},
		ChainID:                         big.NewInt(1),
		HomesteadBlock:                  big.NewInt(0),
		DAOForkBlock:                    nil,
		DAOForkSupport:                  false,
		EIP150Block:                     big.NewInt(0),
		EIP155Block:                     big.NewInt(0),
		EIP158Block:                     big.NewInt(0),
		ByzantiumBlock:                  big.NewInt(0),
		ConstantinopleBlock:             big.NewInt(0),
		PetersburgBlock:                 big.NewInt(0),
		IstanbulBlock:                   big.NewInt(0),
		MuirGlacierBlock:                big.NewInt(0),
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
	}

	TestApricotPhase4Config = &ChainConfig{
		AvalancheContext:                AvalancheContext{utils.TestSnowContext()},
		ChainID:                         big.NewInt(1),
		HomesteadBlock:                  big.NewInt(0),
		DAOForkBlock:                    nil,
		DAOForkSupport:                  false,
		EIP150Block:                     big.NewInt(0),
		EIP155Block:                     big.NewInt(0),
		EIP158Block:                     big.NewInt(0),
		ByzantiumBlock:                  big.NewInt(0),
		ConstantinopleBlock:             big.NewInt(0),
		PetersburgBlock:                 big.NewInt(0),
		IstanbulBlock:                   big.NewInt(0),
		MuirGlacierBlock:                big.NewInt(0),
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
	}

	TestApricotPhase5Config = &ChainConfig{
		AvalancheContext:                AvalancheContext{utils.TestSnowContext()},
		ChainID:                         big.NewInt(1),
		HomesteadBlock:                  big.NewInt(0),
		DAOForkBlock:                    nil,
		DAOForkSupport:                  false,
		EIP150Block:                     big.NewInt(0),
		EIP155Block:                     big.NewInt(0),
		EIP158Block:                     big.NewInt(0),
		ByzantiumBlock:                  big.NewInt(0),
		ConstantinopleBlock:             big.NewInt(0),
		PetersburgBlock:                 big.NewInt(0),
		IstanbulBlock:                   big.NewInt(0),
		MuirGlacierBlock:                big.NewInt(0),
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
	}

	TestApricotPhasePre6Config = &ChainConfig{
		AvalancheContext:                AvalancheContext{utils.TestSnowContext()},
		ChainID:                         big.NewInt(1),
		HomesteadBlock:                  big.NewInt(0),
		DAOForkBlock:                    nil,
		DAOForkSupport:                  false,
		EIP150Block:                     big.NewInt(0),
		EIP155Block:                     big.NewInt(0),
		EIP158Block:                     big.NewInt(0),
		ByzantiumBlock:                  big.NewInt(0),
		ConstantinopleBlock:             big.NewInt(0),
		PetersburgBlock:                 big.NewInt(0),
		IstanbulBlock:                   big.NewInt(0),
		MuirGlacierBlock:                big.NewInt(0),
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
	}

	TestApricotPhase6Config = &ChainConfig{
		AvalancheContext:                AvalancheContext{utils.TestSnowContext()},
		ChainID:                         big.NewInt(1),
		HomesteadBlock:                  big.NewInt(0),
		DAOForkBlock:                    nil,
		DAOForkSupport:                  false,
		EIP150Block:                     big.NewInt(0),
		EIP155Block:                     big.NewInt(0),
		EIP158Block:                     big.NewInt(0),
		ByzantiumBlock:                  big.NewInt(0),
		ConstantinopleBlock:             big.NewInt(0),
		PetersburgBlock:                 big.NewInt(0),
		IstanbulBlock:                   big.NewInt(0),
		MuirGlacierBlock:                big.NewInt(0),
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
	}

	TestApricotPhasePost6Config = &ChainConfig{
		AvalancheContext:                AvalancheContext{utils.TestSnowContext()},
		ChainID:                         big.NewInt(1),
		HomesteadBlock:                  big.NewInt(0),
		DAOForkBlock:                    nil,
		DAOForkSupport:                  false,
		EIP150Block:                     big.NewInt(0),
		EIP155Block:                     big.NewInt(0),
		EIP158Block:                     big.NewInt(0),
		ByzantiumBlock:                  big.NewInt(0),
		ConstantinopleBlock:             big.NewInt(0),
		PetersburgBlock:                 big.NewInt(0),
		IstanbulBlock:                   big.NewInt(0),
		MuirGlacierBlock:                big.NewInt(0),
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
	}

	TestBanffChainConfig = &ChainConfig{
		AvalancheContext:                AvalancheContext{utils.TestSnowContext()},
		ChainID:                         big.NewInt(1),
		HomesteadBlock:                  big.NewInt(0),
		DAOForkBlock:                    nil,
		DAOForkSupport:                  false,
		EIP150Block:                     big.NewInt(0),
		EIP155Block:                     big.NewInt(0),
		EIP158Block:                     big.NewInt(0),
		ByzantiumBlock:                  big.NewInt(0),
		ConstantinopleBlock:             big.NewInt(0),
		PetersburgBlock:                 big.NewInt(0),
		IstanbulBlock:                   big.NewInt(0),
		MuirGlacierBlock:                big.NewInt(0),
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
	}

	TestCortinaChainConfig = &ChainConfig{
		AvalancheContext:                AvalancheContext{utils.TestSnowContext()},
		ChainID:                         big.NewInt(1),
		HomesteadBlock:                  big.NewInt(0),
		DAOForkBlock:                    nil,
		DAOForkSupport:                  false,
		EIP150Block:                     big.NewInt(0),
		EIP155Block:                     big.NewInt(0),
		EIP158Block:                     big.NewInt(0),
		ByzantiumBlock:                  big.NewInt(0),
		ConstantinopleBlock:             big.NewInt(0),
		PetersburgBlock:                 big.NewInt(0),
		IstanbulBlock:                   big.NewInt(0),
		MuirGlacierBlock:                big.NewInt(0),
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
	}

	TestDurangoChainConfig = &ChainConfig{
		AvalancheContext:                AvalancheContext{utils.TestSnowContext()},
		ChainID:                         big.NewInt(1),
		HomesteadBlock:                  big.NewInt(0),
		DAOForkBlock:                    nil,
		DAOForkSupport:                  false,
		EIP150Block:                     big.NewInt(0),
		EIP155Block:                     big.NewInt(0),
		EIP158Block:                     big.NewInt(0),
		ByzantiumBlock:                  big.NewInt(0),
		ConstantinopleBlock:             big.NewInt(0),
		PetersburgBlock:                 big.NewInt(0),
		IstanbulBlock:                   big.NewInt(0),
		MuirGlacierBlock:                big.NewInt(0),
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
	}

	TestRules = TestChainConfig.Rules(new(big.Int), 0)
)

func getChainConfig(networkID uint32, chainID *big.Int) *ChainConfig {
	return &ChainConfig{
		ChainID:                         chainID,
		HomesteadBlock:                  big.NewInt(0),
		DAOForkBlock:                    big.NewInt(0),
		DAOForkSupport:                  true,
		EIP150Block:                     big.NewInt(0),
		EIP155Block:                     big.NewInt(0),
		EIP158Block:                     big.NewInt(0),
		ByzantiumBlock:                  big.NewInt(0),
		ConstantinopleBlock:             big.NewInt(0),
		PetersburgBlock:                 big.NewInt(0),
		IstanbulBlock:                   big.NewInt(0),
		MuirGlacierBlock:                big.NewInt(0),
		ApricotPhase1BlockTimestamp:     getUpgradeTime(networkID, version.ApricotPhase1Times),
		ApricotPhase2BlockTimestamp:     getUpgradeTime(networkID, version.ApricotPhase2Times),
		ApricotPhase3BlockTimestamp:     getUpgradeTime(networkID, version.ApricotPhase3Times),
		ApricotPhase4BlockTimestamp:     getUpgradeTime(networkID, version.ApricotPhase4Times),
		ApricotPhase5BlockTimestamp:     getUpgradeTime(networkID, version.ApricotPhase5Times),
		ApricotPhasePre6BlockTimestamp:  getUpgradeTime(networkID, version.ApricotPhasePre6Times),
		ApricotPhase6BlockTimestamp:     getUpgradeTime(networkID, version.ApricotPhase6Times),
		ApricotPhasePost6BlockTimestamp: getUpgradeTime(networkID, version.ApricotPhasePost6Times),
		BanffBlockTimestamp:             getUpgradeTime(networkID, version.BanffTimes),
		CortinaBlockTimestamp:           getUpgradeTime(networkID, version.CortinaTimes),
		DurangoBlockTimestamp:           getUpgradeTime(networkID, version.DurangoTimes),
	}
}

// ChainConfig is the core config which determines the blockchain settings.
//
// ChainConfig is stored in the database on a per block basis. This means
// that any network, identified by its genesis block, can have its own
// set of configuration options.
type ChainConfig struct {
	AvalancheContext `json:"-"` // Avalanche specific context set during VM initialization. Not serialized.

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

	// Avalanche Network Upgrades
	ApricotPhase1BlockTimestamp *uint64 `json:"apricotPhase1BlockTimestamp,omitempty"` // Apricot Phase 1 Block Timestamp (nil = no fork, 0 = already activated)
	// Apricot Phase 2 Block Timestamp (nil = no fork, 0 = already activated)
	// Apricot Phase 2 includes a modified version of the Berlin Hard Fork from Ethereum
	ApricotPhase2BlockTimestamp *uint64 `json:"apricotPhase2BlockTimestamp,omitempty"`
	// Apricot Phase 3 introduces dynamic fees and a modified version of the London Hard Fork from Ethereum (nil = no fork, 0 = already activated)
	ApricotPhase3BlockTimestamp *uint64 `json:"apricotPhase3BlockTimestamp,omitempty"`
	// Apricot Phase 4 introduces the notion of a block fee to the dynamic fee algorithm (nil = no fork, 0 = already activated)
	ApricotPhase4BlockTimestamp *uint64 `json:"apricotPhase4BlockTimestamp,omitempty"`
	// Apricot Phase 5 introduces a batch of atomic transactions with a maximum atomic gas limit per block. (nil = no fork, 0 = already activated)
	ApricotPhase5BlockTimestamp *uint64 `json:"apricotPhase5BlockTimestamp,omitempty"`
	// Apricot Phase Pre-6 deprecates the NativeAssetCall precompile (soft). (nil = no fork, 0 = already activated)
	ApricotPhasePre6BlockTimestamp *uint64 `json:"apricotPhasePre6BlockTimestamp,omitempty"`
	// Apricot Phase 6 deprecates the NativeAssetBalance and NativeAssetCall precompiles. (nil = no fork, 0 = already activated)
	ApricotPhase6BlockTimestamp *uint64 `json:"apricotPhase6BlockTimestamp,omitempty"`
	// Apricot Phase Post-6 deprecates the NativeAssetCall precompile (soft). (nil = no fork, 0 = already activated)
	ApricotPhasePost6BlockTimestamp *uint64 `json:"apricotPhasePost6BlockTimestamp,omitempty"`
	// Banff restricts import/export transactions to AVAX. (nil = no fork, 0 = already activated)
	BanffBlockTimestamp *uint64 `json:"banffBlockTimestamp,omitempty"`
	// Cortina increases the block gas limit to 15M. (nil = no fork, 0 = already activated)
	CortinaBlockTimestamp *uint64 `json:"cortinaBlockTimestamp,omitempty"`
	// Durango activates the Shanghai Execution Spec Upgrade from Ethereum (https://github.com/ethereum/execution-specs/blob/master/network-upgrades/mainnet-upgrades/shanghai.md#included-eips)
	// and Avalanche Warp Messaging. (nil = no fork, 0 = already activated)
	// Note: EIP-4895 is excluded since withdrawals are not relevant to the Avalanche C-Chain or Subnets running the EVM.
	DurangoBlockTimestamp *uint64 `json:"durangoBlockTimestamp,omitempty"`
	// Cancun activates the Cancun upgrade from Ethereum. (nil = no fork, 0 = already activated)
	CancunTime *uint64 `json:"cancunTime,omitempty"`
	// Verkle activates the Verkle upgrade from Ethereum. (nil = no fork, 0 = already activated)
	VerkleTime *uint64 `json:"verkleTime,omitempty"` // Verkle switch time (nil = no fork, 0 = already on verkle)

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
	banner += fmt.Sprintf(" - Cancun Timestamp:                 @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.12.0)\n", ptrToString(c.CancunTime))
	banner += "\n"

	banner += "Mandatory Avalanche Upgrades (timestamp based):\n"
	banner += fmt.Sprintf(" - Apricot Phase 1 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.3.0)\n", ptrToString(c.ApricotPhase1BlockTimestamp))
	banner += fmt.Sprintf(" - Apricot Phase 2 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.4.0)\n", ptrToString(c.ApricotPhase2BlockTimestamp))
	banner += fmt.Sprintf(" - Apricot Phase 3 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.5.0)\n", ptrToString(c.ApricotPhase3BlockTimestamp))
	banner += fmt.Sprintf(" - Apricot Phase 4 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.6.0)\n", ptrToString(c.ApricotPhase4BlockTimestamp))
	banner += fmt.Sprintf(" - Apricot Phase 5 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0)\n", ptrToString(c.ApricotPhase5BlockTimestamp))
	banner += fmt.Sprintf(" - Apricot Phase P6 Timestamp        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.8.0)\n", ptrToString(c.ApricotPhasePre6BlockTimestamp))
	banner += fmt.Sprintf(" - Apricot Phase 6 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.8.0)\n", ptrToString(c.ApricotPhase6BlockTimestamp))
	banner += fmt.Sprintf(" - Apricot Phase Post-6 Timestamp:   @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.8.0\n", ptrToString(c.ApricotPhasePost6BlockTimestamp))
	banner += fmt.Sprintf(" - Banff Timestamp:                  @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0)\n", ptrToString(c.BanffBlockTimestamp))
	banner += fmt.Sprintf(" - Cortina Timestamp:                @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.10.0)\n", ptrToString(c.CortinaBlockTimestamp))
	banner += fmt.Sprintf(" - Durango Timestamp:                @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.11.0)\n", ptrToString(c.DurangoBlockTimestamp))
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
	return utils.IsBlockForked(c.HomesteadBlock, num)
}

// IsDAOFork returns whether num is either equal to the DAO fork block or greater.
func (c *ChainConfig) IsDAOFork(num *big.Int) bool {
	return utils.IsBlockForked(c.DAOForkBlock, num)
}

// IsEIP150 returns whether num is either equal to the EIP150 fork block or greater.
func (c *ChainConfig) IsEIP150(num *big.Int) bool {
	return utils.IsBlockForked(c.EIP150Block, num)
}

// IsEIP155 returns whether num is either equal to the EIP155 fork block or greater.
func (c *ChainConfig) IsEIP155(num *big.Int) bool {
	return utils.IsBlockForked(c.EIP155Block, num)
}

// IsEIP158 returns whether num is either equal to the EIP158 fork block or greater.
func (c *ChainConfig) IsEIP158(num *big.Int) bool {
	return utils.IsBlockForked(c.EIP158Block, num)
}

// IsByzantium returns whether num is either equal to the Byzantium fork block or greater.
func (c *ChainConfig) IsByzantium(num *big.Int) bool {
	return utils.IsBlockForked(c.ByzantiumBlock, num)
}

// IsConstantinople returns whether num is either equal to the Constantinople fork block or greater.
func (c *ChainConfig) IsConstantinople(num *big.Int) bool {
	return utils.IsBlockForked(c.ConstantinopleBlock, num)
}

// IsMuirGlacier returns whether num is either equal to the Muir Glacier (EIP-2384) fork block or greater.
func (c *ChainConfig) IsMuirGlacier(num *big.Int) bool {
	return utils.IsBlockForked(c.MuirGlacierBlock, num)
}

// IsPetersburg returns whether num is either
// - equal to or greater than the PetersburgBlock fork block,
// - OR is nil, and Constantinople is active
func (c *ChainConfig) IsPetersburg(num *big.Int) bool {
	return utils.IsBlockForked(c.PetersburgBlock, num) || c.PetersburgBlock == nil && utils.IsBlockForked(c.ConstantinopleBlock, num)
}

// IsIstanbul returns whether num is either equal to the Istanbul fork block or greater.
func (c *ChainConfig) IsIstanbul(num *big.Int) bool {
	return utils.IsBlockForked(c.IstanbulBlock, num)
}

// Avalanche Upgrades:

// IsApricotPhase1 returns whether [time] represents a block
// with a timestamp after the Apricot Phase 1 upgrade time.
func (c *ChainConfig) IsApricotPhase1(time uint64) bool {
	return utils.IsTimestampForked(c.ApricotPhase1BlockTimestamp, time)
}

// IsApricotPhase2 returns whether [time] represents a block
// with a timestamp after the Apricot Phase 2 upgrade time.
func (c *ChainConfig) IsApricotPhase2(time uint64) bool {
	return utils.IsTimestampForked(c.ApricotPhase2BlockTimestamp, time)
}

// IsApricotPhase3 returns whether [time] represents a block
// with a timestamp after the Apricot Phase 3 upgrade time.
func (c *ChainConfig) IsApricotPhase3(time uint64) bool {
	return utils.IsTimestampForked(c.ApricotPhase3BlockTimestamp, time)
}

// IsApricotPhase4 returns whether [time] represents a block
// with a timestamp after the Apricot Phase 4 upgrade time.
func (c *ChainConfig) IsApricotPhase4(time uint64) bool {
	return utils.IsTimestampForked(c.ApricotPhase4BlockTimestamp, time)
}

// IsApricotPhase5 returns whether [time] represents a block
// with a timestamp after the Apricot Phase 5 upgrade time.
func (c *ChainConfig) IsApricotPhase5(time uint64) bool {
	return utils.IsTimestampForked(c.ApricotPhase5BlockTimestamp, time)
}

// IsApricotPhasePre6 returns whether [time] represents a block
// with a timestamp after the Apricot Phase Pre 6 upgrade time.
func (c *ChainConfig) IsApricotPhasePre6(time uint64) bool {
	return utils.IsTimestampForked(c.ApricotPhasePre6BlockTimestamp, time)
}

// IsApricotPhase6 returns whether [time] represents a block
// with a timestamp after the Apricot Phase 6 upgrade time.
func (c *ChainConfig) IsApricotPhase6(time uint64) bool {
	return utils.IsTimestampForked(c.ApricotPhase6BlockTimestamp, time)
}

// IsApricotPhasePost6 returns whether [time] represents a block
// with a timestamp after the Apricot Phase 6 Post upgrade time.
func (c *ChainConfig) IsApricotPhasePost6(time uint64) bool {
	return utils.IsTimestampForked(c.ApricotPhasePost6BlockTimestamp, time)
}

// IsBanff returns whether [time] represents a block
// with a timestamp after the Banff upgrade time.
func (c *ChainConfig) IsBanff(time uint64) bool {
	return utils.IsTimestampForked(c.BanffBlockTimestamp, time)
}

// IsCortina returns whether [time] represents a block
// with a timestamp after the Cortina upgrade time.
func (c *ChainConfig) IsCortina(time uint64) bool {
	return utils.IsTimestampForked(c.CortinaBlockTimestamp, time)
}

// IsDurango returns whether [time] represents a block
// with a timestamp after the Durango upgrade time.
func (c *ChainConfig) IsDurango(time uint64) bool {
	return utils.IsTimestampForked(c.DurangoBlockTimestamp, time)
}

// IsCancun returns whether [time] represents a block
// with a timestamp after the Cancun upgrade time.
func (c *ChainConfig) IsCancun(num *big.Int, time uint64) bool {
	return utils.IsTimestampForked(c.CancunTime, time)
}

// IsVerkle returns whether [time] represents a block
// with a timestamp after the Verkle upgrade time.
func (c *ChainConfig) IsVerkle(num *big.Int, time uint64) bool {
	return utils.IsTimestampForked(c.VerkleTime, time)
}

func (r *Rules) PredicatersExist() bool {
	return len(r.Predicaters) > 0
}

func (r *Rules) PredicaterExists(addr common.Address) bool {
	_, PredicaterExists := r.Predicaters[addr]
	return PredicaterExists
}

// IsPrecompileEnabled returns whether precompile with [address] is enabled at [timestamp].
func (c *ChainConfig) IsPrecompileEnabled(address common.Address, timestamp uint64) bool {
	config := c.getActivePrecompileConfig(address, timestamp)
	return config != nil && !config.IsDisabled()
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

// Verify verifies chain config and returns error
func (c *ChainConfig) Verify() error {
	// Verify the precompile upgrades are internally consistent given the existing chainConfig.
	if err := c.verifyPrecompileUpgrades(); err != nil {
		return fmt.Errorf("invalid precompile upgrades: %w", err)
	}

	return nil
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
		{name: "cancunTime", timestamp: c.CancunTime},
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
		if blockFork && cur.block != nil && common.Big0.Cmp(cur.block) != 0 {
			return errNonGenesisForkByHeight
		}
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

	// TODO(aaronbuchwald) check that avalanche block timestamps are at least possible with the other rule set changes
	// additional change: require that block number hard forks are either 0 or nil since they should not
	// be enabled at a specific block number.

	return nil
}

func (c *ChainConfig) checkCompatible(newcfg *ChainConfig, height *big.Int, time uint64) *ConfigCompatError {
	if isForkBlockIncompatible(c.HomesteadBlock, newcfg.HomesteadBlock, height) {
		return newBlockCompatError("Homestead fork block", c.HomesteadBlock, newcfg.HomesteadBlock)
	}
	if isForkBlockIncompatible(c.DAOForkBlock, newcfg.DAOForkBlock, height) {
		return newBlockCompatError("DAO fork block", c.DAOForkBlock, newcfg.DAOForkBlock)
	}
	if c.IsDAOFork(height) && c.DAOForkSupport != newcfg.DAOForkSupport {
		return newBlockCompatError("DAO fork support flag", c.DAOForkBlock, newcfg.DAOForkBlock)
	}
	if isForkBlockIncompatible(c.EIP150Block, newcfg.EIP150Block, height) {
		return newBlockCompatError("EIP150 fork block", c.EIP150Block, newcfg.EIP150Block)
	}
	if isForkBlockIncompatible(c.EIP155Block, newcfg.EIP155Block, height) {
		return newBlockCompatError("EIP155 fork block", c.EIP155Block, newcfg.EIP155Block)
	}
	if isForkBlockIncompatible(c.EIP158Block, newcfg.EIP158Block, height) {
		return newBlockCompatError("EIP158 fork block", c.EIP158Block, newcfg.EIP158Block)
	}
	if c.IsEIP158(height) && !configBlockEqual(c.ChainID, newcfg.ChainID) {
		return newBlockCompatError("EIP158 chain ID", c.EIP158Block, newcfg.EIP158Block)
	}
	if isForkBlockIncompatible(c.ByzantiumBlock, newcfg.ByzantiumBlock, height) {
		return newBlockCompatError("Byzantium fork block", c.ByzantiumBlock, newcfg.ByzantiumBlock)
	}
	if isForkBlockIncompatible(c.ConstantinopleBlock, newcfg.ConstantinopleBlock, height) {
		return newBlockCompatError("Constantinople fork block", c.ConstantinopleBlock, newcfg.ConstantinopleBlock)
	}
	if isForkBlockIncompatible(c.PetersburgBlock, newcfg.PetersburgBlock, height) {
		// the only case where we allow Petersburg to be set in the past is if it is equal to Constantinople
		// mainly to satisfy fork ordering requirements which state that Petersburg fork be set if Constantinople fork is set
		if isForkBlockIncompatible(c.ConstantinopleBlock, newcfg.PetersburgBlock, height) {
			return newBlockCompatError("Petersburg fork block", c.PetersburgBlock, newcfg.PetersburgBlock)
		}
	}
	if isForkBlockIncompatible(c.IstanbulBlock, newcfg.IstanbulBlock, height) {
		return newBlockCompatError("Istanbul fork block", c.IstanbulBlock, newcfg.IstanbulBlock)
	}
	if isForkBlockIncompatible(c.MuirGlacierBlock, newcfg.MuirGlacierBlock, height) {
		return newBlockCompatError("Muir Glacier fork block", c.MuirGlacierBlock, newcfg.MuirGlacierBlock)
	}
	if isForkTimestampIncompatible(c.ApricotPhase1BlockTimestamp, newcfg.ApricotPhase1BlockTimestamp, time) {
		return newTimestampCompatError("ApricotPhase1 fork block timestamp", c.ApricotPhase1BlockTimestamp, newcfg.ApricotPhase1BlockTimestamp)
	}
	if isForkTimestampIncompatible(c.ApricotPhase2BlockTimestamp, newcfg.ApricotPhase2BlockTimestamp, time) {
		return newTimestampCompatError("ApricotPhase2 fork block timestamp", c.ApricotPhase2BlockTimestamp, newcfg.ApricotPhase2BlockTimestamp)
	}
	if isForkTimestampIncompatible(c.ApricotPhase3BlockTimestamp, newcfg.ApricotPhase3BlockTimestamp, time) {
		return newTimestampCompatError("ApricotPhase3 fork block timestamp", c.ApricotPhase3BlockTimestamp, newcfg.ApricotPhase3BlockTimestamp)
	}
	if isForkTimestampIncompatible(c.ApricotPhase4BlockTimestamp, newcfg.ApricotPhase4BlockTimestamp, time) {
		return newTimestampCompatError("ApricotPhase4 fork block timestamp", c.ApricotPhase4BlockTimestamp, newcfg.ApricotPhase4BlockTimestamp)
	}
	if isForkTimestampIncompatible(c.ApricotPhase5BlockTimestamp, newcfg.ApricotPhase5BlockTimestamp, time) {
		return newTimestampCompatError("ApricotPhase5 fork block timestamp", c.ApricotPhase5BlockTimestamp, newcfg.ApricotPhase5BlockTimestamp)
	}
	if isForkTimestampIncompatible(c.ApricotPhasePre6BlockTimestamp, newcfg.ApricotPhasePre6BlockTimestamp, time) {
		return newTimestampCompatError("ApricotPhasePre6 fork block timestamp", c.ApricotPhasePre6BlockTimestamp, newcfg.ApricotPhasePre6BlockTimestamp)
	}
	if isForkTimestampIncompatible(c.ApricotPhase6BlockTimestamp, newcfg.ApricotPhase6BlockTimestamp, time) {
		return newTimestampCompatError("ApricotPhase6 fork block timestamp", c.ApricotPhase6BlockTimestamp, newcfg.ApricotPhase6BlockTimestamp)
	}
	if isForkTimestampIncompatible(c.ApricotPhasePost6BlockTimestamp, newcfg.ApricotPhasePost6BlockTimestamp, time) {
		return newTimestampCompatError("ApricotPhasePost6 fork block timestamp", c.ApricotPhasePost6BlockTimestamp, newcfg.ApricotPhasePost6BlockTimestamp)
	}
	if isForkTimestampIncompatible(c.BanffBlockTimestamp, newcfg.BanffBlockTimestamp, time) {
		return newTimestampCompatError("Banff fork block timestamp", c.BanffBlockTimestamp, newcfg.BanffBlockTimestamp)
	}
	if isForkTimestampIncompatible(c.CortinaBlockTimestamp, newcfg.CortinaBlockTimestamp, time) {
		return newTimestampCompatError("Cortina fork block timestamp", c.CortinaBlockTimestamp, newcfg.CortinaBlockTimestamp)
	}
	if isForkTimestampIncompatible(c.DurangoBlockTimestamp, newcfg.DurangoBlockTimestamp, time) {
		return newTimestampCompatError("Durango fork block timestamp", c.DurangoBlockTimestamp, newcfg.DurangoBlockTimestamp)
	}
	if isForkTimestampIncompatible(c.CancunTime, newcfg.CancunTime, time) {
		return newTimestampCompatError("Cancun fork block timestamp", c.CancunTime, newcfg.CancunTime)
	}

	return nil
}

// isForkBlockIncompatible returns true if a fork scheduled at s1 cannot be rescheduled to
// block s2 because head is already past the fork.
func isForkBlockIncompatible(s1, s2, head *big.Int) bool {
	return (utils.IsBlockForked(s1, head) || utils.IsBlockForked(s2, head)) && !configBlockEqual(s1, s2)
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
	return (utils.IsTimestampForked(s1, head) || utils.IsTimestampForked(s2, head)) && !configTimestampEqual(s1, s2)
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

func ptrToString(val *uint64) string {
	if val == nil {
		return "nil"
	}
	return fmt.Sprintf("%d", *val)
}

// Rules wraps ChainConfig and is merely syntactic sugar or can be used for functions
// that do not have or require information about the block.
//
// Rules is a one time interface meaning that it shouldn't be used in between transition
// phases.
type Rules struct {
	ChainID                                                 *big.Int
	IsHomestead, IsEIP150, IsEIP155, IsEIP158               bool
	IsByzantium, IsConstantinople, IsPetersburg, IsIstanbul bool
	IsCancun                                                bool

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

// IsPrecompileEnabled returns true if the precompile at [addr] is enabled for this rule set.
func (r *Rules) IsPrecompileEnabled(addr common.Address) bool {
	_, ok := r.ActivePrecompiles[addr]
	return ok
}

// Rules ensures c's ChainID is not nil.
func (c *ChainConfig) rules(num *big.Int, timestamp uint64) Rules {
	chainID := c.ChainID
	if chainID == nil {
		chainID = new(big.Int)
	}
	return Rules{
		ChainID:          new(big.Int).Set(chainID),
		IsHomestead:      c.IsHomestead(num),
		IsEIP150:         c.IsEIP150(num),
		IsEIP155:         c.IsEIP155(num),
		IsEIP158:         c.IsEIP158(num),
		IsByzantium:      c.IsByzantium(num),
		IsConstantinople: c.IsConstantinople(num),
		IsPetersburg:     c.IsPetersburg(num),
		IsIstanbul:       c.IsIstanbul(num),
		IsCancun:         c.IsCancun(num, timestamp),
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
