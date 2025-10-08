// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2015 The go-ethereum Authors
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

package tests

import (
	"fmt"
	"math/big"
	"os"
	"sort"
	"strings"

	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/utils"
)

// Forks table defines supported forks and their chain config.
var Forks map[string]*params.ChainConfig

func init() {
	params.WithTempRegisteredExtras(initializeForks)
}

// initializeForks MUST be called inside [params.WithTempRegisteredExtras] to
// allow [params.WithExtra] to work without global registration of libevm
// extras.
func initializeForks() {
	Forks = map[string]*params.ChainConfig{
		"Frontier": {
			ChainID: big.NewInt(1),
		},
		"Homestead": {
			ChainID:        big.NewInt(1),
			HomesteadBlock: big.NewInt(0),
		},
		"EIP150": {
			ChainID:        big.NewInt(1),
			HomesteadBlock: big.NewInt(0),
			EIP150Block:    big.NewInt(0),
		},
		"EIP158": {
			ChainID:        big.NewInt(1),
			HomesteadBlock: big.NewInt(0),
			EIP150Block:    big.NewInt(0),
			EIP155Block:    big.NewInt(0),
			EIP158Block:    big.NewInt(0),
		},
		"Byzantium": {
			ChainID:        big.NewInt(1),
			HomesteadBlock: big.NewInt(0),
			EIP150Block:    big.NewInt(0),
			EIP155Block:    big.NewInt(0),
			EIP158Block:    big.NewInt(0),
			DAOForkBlock:   big.NewInt(0),
			ByzantiumBlock: big.NewInt(0),
		},
		"Constantinople": {
			ChainID:             big.NewInt(1),
			HomesteadBlock:      big.NewInt(0),
			EIP150Block:         big.NewInt(0),
			EIP155Block:         big.NewInt(0),
			EIP158Block:         big.NewInt(0),
			DAOForkBlock:        big.NewInt(0),
			ByzantiumBlock:      big.NewInt(0),
			ConstantinopleBlock: big.NewInt(0),
			PetersburgBlock:     big.NewInt(10000000),
		},
		"ConstantinopleFix": {
			ChainID:             big.NewInt(1),
			HomesteadBlock:      big.NewInt(0),
			EIP150Block:         big.NewInt(0),
			EIP155Block:         big.NewInt(0),
			EIP158Block:         big.NewInt(0),
			DAOForkBlock:        big.NewInt(0),
			ByzantiumBlock:      big.NewInt(0),
			ConstantinopleBlock: big.NewInt(0),
			PetersburgBlock:     big.NewInt(0),
		},
		"Istanbul": {
			ChainID:             big.NewInt(1),
			HomesteadBlock:      big.NewInt(0),
			EIP150Block:         big.NewInt(0),
			EIP155Block:         big.NewInt(0),
			EIP158Block:         big.NewInt(0),
			DAOForkBlock:        big.NewInt(0),
			ByzantiumBlock:      big.NewInt(0),
			ConstantinopleBlock: big.NewInt(0),
			PetersburgBlock:     big.NewInt(0),
			IstanbulBlock:       big.NewInt(0),
		},
		"MuirGlacier": {
			ChainID:             big.NewInt(1),
			HomesteadBlock:      big.NewInt(0),
			EIP150Block:         big.NewInt(0),
			EIP155Block:         big.NewInt(0),
			EIP158Block:         big.NewInt(0),
			DAOForkBlock:        big.NewInt(0),
			ByzantiumBlock:      big.NewInt(0),
			ConstantinopleBlock: big.NewInt(0),
			PetersburgBlock:     big.NewInt(0),
			IstanbulBlock:       big.NewInt(0),
			MuirGlacierBlock:    big.NewInt(0),
		},
		"FrontierToHomesteadAt5": {
			ChainID:        big.NewInt(1),
			HomesteadBlock: big.NewInt(5),
		},
		"HomesteadToEIP150At5": {
			ChainID:        big.NewInt(1),
			HomesteadBlock: big.NewInt(0),
			EIP150Block:    big.NewInt(5),
		},
		"HomesteadToDaoAt5": {
			ChainID:        big.NewInt(1),
			HomesteadBlock: big.NewInt(0),
			DAOForkBlock:   big.NewInt(5),
			DAOForkSupport: true,
		},
		"EIP158ToByzantiumAt5": {
			ChainID:        big.NewInt(1),
			HomesteadBlock: big.NewInt(0),
			EIP150Block:    big.NewInt(0),
			EIP155Block:    big.NewInt(0),
			EIP158Block:    big.NewInt(0),
			ByzantiumBlock: big.NewInt(5),
		},
		"ByzantiumToConstantinopleAt5": {
			ChainID:             big.NewInt(1),
			HomesteadBlock:      big.NewInt(0),
			EIP150Block:         big.NewInt(0),
			EIP155Block:         big.NewInt(0),
			EIP158Block:         big.NewInt(0),
			ByzantiumBlock:      big.NewInt(0),
			ConstantinopleBlock: big.NewInt(5),
		},
		"ByzantiumToConstantinopleFixAt5": {
			ChainID:             big.NewInt(1),
			HomesteadBlock:      big.NewInt(0),
			EIP150Block:         big.NewInt(0),
			EIP155Block:         big.NewInt(0),
			EIP158Block:         big.NewInt(0),
			ByzantiumBlock:      big.NewInt(0),
			ConstantinopleBlock: big.NewInt(5),
			PetersburgBlock:     big.NewInt(5),
		},
		"ConstantinopleFixToIstanbulAt5": {
			ChainID:             big.NewInt(1),
			HomesteadBlock:      big.NewInt(0),
			EIP150Block:         big.NewInt(0),
			EIP155Block:         big.NewInt(0),
			EIP158Block:         big.NewInt(0),
			ByzantiumBlock:      big.NewInt(0),
			ConstantinopleBlock: big.NewInt(0),
			PetersburgBlock:     big.NewInt(0),
			IstanbulBlock:       big.NewInt(5),
		},
		"ApricotPhase1": params.WithExtra(
			&params.ChainConfig{
				ChainID:             big.NewInt(1),
				HomesteadBlock:      big.NewInt(0),
				EIP150Block:         big.NewInt(0),
				EIP155Block:         big.NewInt(0),
				EIP158Block:         big.NewInt(0),
				ByzantiumBlock:      big.NewInt(0),
				ConstantinopleBlock: big.NewInt(0),
				PetersburgBlock:     big.NewInt(0),
				IstanbulBlock:       big.NewInt(0),
			},
			&extras.ChainConfig{
				NetworkUpgrades: extras.NetworkUpgrades{
					ApricotPhase1BlockTimestamp: utils.NewUint64(0),
				},
			},
		),
		"ApricotPhase2": params.WithExtra(
			&params.ChainConfig{
				ChainID:             big.NewInt(1),
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
			},
			&extras.ChainConfig{
				NetworkUpgrades: extras.NetworkUpgrades{
					ApricotPhase1BlockTimestamp: utils.NewUint64(0),
					ApricotPhase2BlockTimestamp: utils.NewUint64(0),
				},
			},
		),
		"ApricotPhase3": params.WithExtra(
			&params.ChainConfig{
				ChainID:             big.NewInt(1),
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
			},
			&extras.ChainConfig{
				NetworkUpgrades: extras.NetworkUpgrades{
					ApricotPhase1BlockTimestamp: utils.NewUint64(0),
					ApricotPhase2BlockTimestamp: utils.NewUint64(0),
					ApricotPhase3BlockTimestamp: utils.NewUint64(0),
				},
			},
		),
		"ApricotPhase4": params.WithExtra(
			&params.ChainConfig{
				ChainID:             big.NewInt(1),
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
			},
			&extras.ChainConfig{
				NetworkUpgrades: extras.NetworkUpgrades{
					ApricotPhase1BlockTimestamp: utils.NewUint64(0),
					ApricotPhase2BlockTimestamp: utils.NewUint64(0),
					ApricotPhase3BlockTimestamp: utils.NewUint64(0),
					ApricotPhase4BlockTimestamp: utils.NewUint64(0),
				},
			},
		),
		"ApricotPhase5": params.WithExtra(
			&params.ChainConfig{
				ChainID:             big.NewInt(1),
				HomesteadBlock:      big.NewInt(0),
				EIP150Block:         big.NewInt(0),
				EIP155Block:         big.NewInt(0),
				EIP158Block:         big.NewInt(0),
				ByzantiumBlock:      big.NewInt(0),
				ConstantinopleBlock: big.NewInt(0),
				PetersburgBlock:     big.NewInt(0),
				IstanbulBlock:       big.NewInt(0),
				BerlinBlock:         big.NewInt(0),
				LondonBlock:         big.NewInt(0),
			},
			&extras.ChainConfig{
				NetworkUpgrades: extras.NetworkUpgrades{
					ApricotPhase1BlockTimestamp: utils.NewUint64(0),
					ApricotPhase2BlockTimestamp: utils.NewUint64(0),
					ApricotPhase3BlockTimestamp: utils.NewUint64(0),
					ApricotPhase4BlockTimestamp: utils.NewUint64(0),
					ApricotPhase5BlockTimestamp: utils.NewUint64(0),
				},
			},
		),
		"Banff": params.WithExtra(
			&params.ChainConfig{
				ChainID:             big.NewInt(1),
				HomesteadBlock:      big.NewInt(0),
				EIP150Block:         big.NewInt(0),
				EIP155Block:         big.NewInt(0),
				EIP158Block:         big.NewInt(0),
				ByzantiumBlock:      big.NewInt(0),
				ConstantinopleBlock: big.NewInt(0),
				PetersburgBlock:     big.NewInt(0),
				IstanbulBlock:       big.NewInt(0),
				BerlinBlock:         big.NewInt(0),
				LondonBlock:         big.NewInt(0),
			},
			&extras.ChainConfig{
				NetworkUpgrades: extras.NetworkUpgrades{
					ApricotPhase1BlockTimestamp: utils.NewUint64(0),
					ApricotPhase2BlockTimestamp: utils.NewUint64(0),
					ApricotPhase3BlockTimestamp: utils.NewUint64(0),
					ApricotPhase4BlockTimestamp: utils.NewUint64(0),
					ApricotPhase5BlockTimestamp: utils.NewUint64(0),
					BanffBlockTimestamp:         utils.NewUint64(0),
				},
			},
		),
		"Cortina": params.WithExtra(
			&params.ChainConfig{
				ChainID:             big.NewInt(1),
				HomesteadBlock:      big.NewInt(0),
				EIP150Block:         big.NewInt(0),
				EIP155Block:         big.NewInt(0),
				EIP158Block:         big.NewInt(0),
				ByzantiumBlock:      big.NewInt(0),
				ConstantinopleBlock: big.NewInt(0),
				PetersburgBlock:     big.NewInt(0),
				IstanbulBlock:       big.NewInt(0),
				BerlinBlock:         big.NewInt(0),
				LondonBlock:         big.NewInt(0),
			},
			&extras.ChainConfig{
				NetworkUpgrades: extras.NetworkUpgrades{
					ApricotPhase1BlockTimestamp: utils.NewUint64(0),
					ApricotPhase2BlockTimestamp: utils.NewUint64(0),
					ApricotPhase3BlockTimestamp: utils.NewUint64(0),
					ApricotPhase4BlockTimestamp: utils.NewUint64(0),
					ApricotPhase5BlockTimestamp: utils.NewUint64(0),
					BanffBlockTimestamp:         utils.NewUint64(0),
					CortinaBlockTimestamp:       utils.NewUint64(0),
				},
			},
		),
		"Durango": params.WithExtra(
			&params.ChainConfig{
				ChainID:             big.NewInt(1),
				HomesteadBlock:      big.NewInt(0),
				EIP150Block:         big.NewInt(0),
				EIP155Block:         big.NewInt(0),
				EIP158Block:         big.NewInt(0),
				ByzantiumBlock:      big.NewInt(0),
				ConstantinopleBlock: big.NewInt(0),
				PetersburgBlock:     big.NewInt(0),
				IstanbulBlock:       big.NewInt(0),
				BerlinBlock:         big.NewInt(0),
				LondonBlock:         big.NewInt(0),
			},
			&extras.ChainConfig{
				NetworkUpgrades: extras.NetworkUpgrades{
					ApricotPhase1BlockTimestamp: utils.NewUint64(0),
					ApricotPhase2BlockTimestamp: utils.NewUint64(0),
					ApricotPhase3BlockTimestamp: utils.NewUint64(0),
					ApricotPhase4BlockTimestamp: utils.NewUint64(0),
					ApricotPhase5BlockTimestamp: utils.NewUint64(0),
					BanffBlockTimestamp:         utils.NewUint64(0),
					CortinaBlockTimestamp:       utils.NewUint64(0),
					DurangoBlockTimestamp:       utils.NewUint64(0),
				},
			},
		),
		"Cancun": params.WithExtra(
			&params.ChainConfig{
				ChainID:             big.NewInt(1),
				HomesteadBlock:      big.NewInt(0),
				EIP150Block:         big.NewInt(0),
				EIP155Block:         big.NewInt(0),
				EIP158Block:         big.NewInt(0),
				ByzantiumBlock:      big.NewInt(0),
				ConstantinopleBlock: big.NewInt(0),
				PetersburgBlock:     big.NewInt(0),
				IstanbulBlock:       big.NewInt(0),
				BerlinBlock:         big.NewInt(0),
				LondonBlock:         big.NewInt(0),
				ShanghaiTime:        utils.NewUint64(0),
				CancunTime:          utils.NewUint64(0),
			},
			&extras.ChainConfig{
				NetworkUpgrades: extras.NetworkUpgrades{
					ApricotPhase1BlockTimestamp: utils.NewUint64(0),
					ApricotPhase2BlockTimestamp: utils.NewUint64(0),
					ApricotPhase3BlockTimestamp: utils.NewUint64(0),
					ApricotPhase4BlockTimestamp: utils.NewUint64(0),
					ApricotPhase5BlockTimestamp: utils.NewUint64(0),
					BanffBlockTimestamp:         utils.NewUint64(0),
					CortinaBlockTimestamp:       utils.NewUint64(0),
					DurangoBlockTimestamp:       utils.NewUint64(0),
				},
			},
		),
	}
}

// AvailableForks returns the set of defined fork names
func AvailableForks() []string {
	var availableForks []string
	for k := range Forks {
		availableForks = append(availableForks, k)
	}
	sort.Strings(availableForks)
	return availableForks
}

// UnsupportedForkError is returned when a test requests a fork that isn't implemented.
type UnsupportedForkError struct {
	Name string
}

func (e UnsupportedForkError) Error() string {
	return fmt.Sprintf("unsupported fork %q", e.Name)
}

func GetRepoRootPath(suffix string) string {
	// - When executed via a test binary, the working directory will be wherever
	// the binary is executed from, but scripts should require execution from
	// the repo root.
	//
	// - When executed via ginkgo (nicer for development + supports
	// parallel execution) the working directory will always be the
	// target path (e.g. [repo root]./tests/warp) and getting the repo
	// root will require stripping the target path suffix.
	//
	// TODO(marun) Avoid relying on the current working directory to find test
	// dependencies by embedding data where possible (e.g. for genesis) and
	// explicitly configuring paths for execution.
	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return strings.TrimSuffix(cwd, suffix)
}
