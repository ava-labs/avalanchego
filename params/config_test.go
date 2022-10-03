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
// Copyright 2017 The go-ethereum Authors
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
	"reflect"
	"testing"
)

func TestCheckCompatible(t *testing.T) {
	type test struct {
		stored, new                 *ChainConfig
		blockHeight, blockTimestamp uint64
		wantErr                     *ConfigCompatError
	}
	tests := []test{
		{stored: TestChainConfig, new: TestChainConfig, blockHeight: 0, blockTimestamp: 0, wantErr: nil},
		{stored: TestChainConfig, new: TestChainConfig, blockHeight: 100, blockTimestamp: 1000, wantErr: nil},
		{
			stored:         &ChainConfig{EIP150Block: big.NewInt(10)},
			new:            &ChainConfig{EIP150Block: big.NewInt(20)},
			blockHeight:    9,
			blockTimestamp: 90,
			wantErr:        nil,
		},
		{
			stored:         TestChainConfig,
			new:            &ChainConfig{HomesteadBlock: nil},
			blockHeight:    3,
			blockTimestamp: 30,
			wantErr: &ConfigCompatError{
				What:         "Homestead fork block",
				StoredConfig: big.NewInt(0),
				NewConfig:    nil,
				RewindTo:     0,
			},
		},
		{
			stored:         TestChainConfig,
			new:            &ChainConfig{HomesteadBlock: big.NewInt(1)},
			blockHeight:    3,
			blockTimestamp: 30,
			wantErr: &ConfigCompatError{
				What:         "Homestead fork block",
				StoredConfig: big.NewInt(0),
				NewConfig:    big.NewInt(1),
				RewindTo:     0,
			},
		},
		{
			stored:         &ChainConfig{HomesteadBlock: big.NewInt(30), EIP150Block: big.NewInt(10)},
			new:            &ChainConfig{HomesteadBlock: big.NewInt(25), EIP150Block: big.NewInt(20)},
			blockHeight:    25,
			blockTimestamp: 250,
			wantErr: &ConfigCompatError{
				What:         "EIP150 fork block",
				StoredConfig: big.NewInt(10),
				NewConfig:    big.NewInt(20),
				RewindTo:     9,
			},
		},
		{
			stored:         &ChainConfig{ConstantinopleBlock: big.NewInt(30)},
			new:            &ChainConfig{ConstantinopleBlock: big.NewInt(30), PetersburgBlock: big.NewInt(30)},
			blockHeight:    40,
			blockTimestamp: 400,
			wantErr:        nil,
		},
		{
			stored:         &ChainConfig{ConstantinopleBlock: big.NewInt(30)},
			new:            &ChainConfig{ConstantinopleBlock: big.NewInt(30), PetersburgBlock: big.NewInt(31)},
			blockHeight:    40,
			blockTimestamp: 400,
			wantErr: &ConfigCompatError{
				What:         "Petersburg fork block",
				StoredConfig: nil,
				NewConfig:    big.NewInt(31),
				RewindTo:     30,
			},
		},
		{
			stored:         TestChainConfig,
			new:            TestApricotPhase4Config,
			blockHeight:    0,
			blockTimestamp: 0,
			wantErr: &ConfigCompatError{
				What:         "ApricotPhase5 fork block timestamp",
				StoredConfig: big.NewInt(0),
				NewConfig:    nil,
				RewindTo:     0,
			},
		},
		{
			stored:         TestChainConfig,
			new:            TestApricotPhase4Config,
			blockHeight:    10,
			blockTimestamp: 100,
			wantErr: &ConfigCompatError{
				What:         "ApricotPhase5 fork block timestamp",
				StoredConfig: big.NewInt(0),
				NewConfig:    nil,
				RewindTo:     0,
			},
		},
	}

	for _, test := range tests {
		err := test.stored.CheckCompatible(test.new, test.blockHeight, test.blockTimestamp)
		if !reflect.DeepEqual(err, test.wantErr) {
			t.Errorf("error mismatch:\nstored: %v\nnew: %v\nblockHeight: %v\nerr: %v\nwant: %v", test.stored, test.new, test.blockHeight, err, test.wantErr)
		}
	}
}
