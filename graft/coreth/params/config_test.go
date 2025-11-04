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
	"math"
	"math/big"
	"reflect"
	"testing"
	"time"

	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/utils"
	ethparams "github.com/ava-labs/libevm/params"
)

func TestCheckCompatible(t *testing.T) {
	type test struct {
		stored, new   *ChainConfig
		headBlock     uint64
		headTimestamp uint64
		wantErr       *ethparams.ConfigCompatError
	}
	tests := []test{
		{stored: TestChainConfig, new: TestChainConfig, headBlock: 0, headTimestamp: 0, wantErr: nil},
		{stored: TestChainConfig, new: TestChainConfig, headBlock: 0, headTimestamp: uint64(time.Now().Unix()), wantErr: nil},
		{stored: TestChainConfig, new: TestChainConfig, headBlock: 100, wantErr: nil},
		{
			stored:        &ChainConfig{EIP150Block: big.NewInt(10)},
			new:           &ChainConfig{EIP150Block: big.NewInt(20)},
			headBlock:     9,
			headTimestamp: 90,
			wantErr:       nil,
		},
		{
			stored:        TestChainConfig,
			new:           &ChainConfig{HomesteadBlock: nil},
			headBlock:     3,
			headTimestamp: 30,
			wantErr: &ethparams.ConfigCompatError{
				What:          "Homestead fork block",
				StoredBlock:   big.NewInt(0),
				NewBlock:      nil,
				RewindToBlock: 0,
			},
		},
		{
			stored:        TestChainConfig,
			new:           &ChainConfig{HomesteadBlock: big.NewInt(1)},
			headBlock:     3,
			headTimestamp: 30,
			wantErr: &ethparams.ConfigCompatError{
				What:          "Homestead fork block",
				StoredBlock:   big.NewInt(0),
				NewBlock:      big.NewInt(1),
				RewindToBlock: 0,
			},
		},
		{
			stored:        &ChainConfig{HomesteadBlock: big.NewInt(30), EIP150Block: big.NewInt(10)},
			new:           &ChainConfig{HomesteadBlock: big.NewInt(25), EIP150Block: big.NewInt(20)},
			headBlock:     25,
			headTimestamp: 250,
			wantErr: &ethparams.ConfigCompatError{
				What:          "EIP150 fork block",
				StoredBlock:   big.NewInt(10),
				NewBlock:      big.NewInt(20),
				RewindToBlock: 9,
			},
		},
		{
			stored:        &ChainConfig{ConstantinopleBlock: big.NewInt(30)},
			new:           &ChainConfig{ConstantinopleBlock: big.NewInt(30), PetersburgBlock: big.NewInt(30)},
			headBlock:     40,
			headTimestamp: 400,
			wantErr:       nil,
		},
		{
			stored:        &ChainConfig{ConstantinopleBlock: big.NewInt(30)},
			new:           &ChainConfig{ConstantinopleBlock: big.NewInt(30), PetersburgBlock: big.NewInt(31)},
			headBlock:     40,
			headTimestamp: 400,
			wantErr: &ethparams.ConfigCompatError{
				What:          "Petersburg fork block",
				StoredBlock:   nil,
				NewBlock:      big.NewInt(31),
				RewindToBlock: 30,
			},
		},
		{
			stored:        TestApricotPhase5Config,
			new:           TestApricotPhase4Config,
			headBlock:     0,
			headTimestamp: 0,
			wantErr: &ethparams.ConfigCompatError{
				What:         "ApricotPhase5 fork block timestamp",
				StoredTime:   utils.NewUint64(0),
				NewTime:      nil,
				RewindToTime: 0,
			},
		},
		{
			stored:        TestApricotPhase5Config,
			new:           TestApricotPhase4Config,
			headBlock:     10,
			headTimestamp: 100,
			wantErr: &ethparams.ConfigCompatError{
				What:         "ApricotPhase5 fork block timestamp",
				StoredTime:   utils.NewUint64(0),
				NewTime:      nil,
				RewindToTime: 0,
			},
		},
	}

	for _, test := range tests {
		err := test.stored.CheckCompatible(test.new, test.headBlock, test.headTimestamp)
		if !reflect.DeepEqual(err, test.wantErr) {
			t.Errorf("error mismatch:\nstored: %v\nnew: %v\nblockHeight: %v\nerr: %v\nwant: %v", test.stored, test.new, test.headBlock, err, test.wantErr)
		}
	}
}

func TestConfigRules(t *testing.T) {
	c := WithExtra(
		&ChainConfig{},
		&extras.ChainConfig{
			NetworkUpgrades: extras.NetworkUpgrades{
				CortinaBlockTimestamp: utils.NewUint64(500),
			},
		},
	)
	var stamp uint64
	if r := c.Rules(big.NewInt(0), IsMergeTODO, stamp); GetRulesExtra(r).IsCortina {
		t.Errorf("expected %v to not be cortina", stamp)
	}
	stamp = 500
	if r := c.Rules(big.NewInt(0), IsMergeTODO, stamp); !GetRulesExtra(r).IsCortina {
		t.Errorf("expected %v to be cortina", stamp)
	}
	stamp = math.MaxInt64
	if r := c.Rules(big.NewInt(0), IsMergeTODO, stamp); !GetRulesExtra(r).IsCortina {
		t.Errorf("expected %v to be cortina", stamp)
	}
}
