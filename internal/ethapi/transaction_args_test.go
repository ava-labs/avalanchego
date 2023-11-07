// (c) 2022, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2022 The go-ethereum Authors
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

package ethapi

import (
	"context"
	"errors"
	"math/big"
	"reflect"
	"testing"

	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

var _ feeBackend = &backendMock{}

// TestSetFeeDefaults tests the logic for filling in default fee values works as expected.
func TestSetFeeDefaults(t *testing.T) {
	type test struct {
		name     string
		isLondon bool
		in       *TransactionArgs
		want     *TransactionArgs
		err      error
	}

	var (
		b        = newBackendMock()
		fortytwo = (*hexutil.Big)(big.NewInt(42))
		maxFee   = (*hexutil.Big)(new(big.Int).Add(new(big.Int).Mul(b.current.BaseFee, big.NewInt(2)), fortytwo.ToInt()))
		al       = &types.AccessList{types.AccessTuple{Address: common.Address{0xaa}, StorageKeys: []common.Hash{{0x01}}}}
	)

	tests := []test{
		// Legacy txs
		{
			"legacy tx pre-London",
			false,
			&TransactionArgs{},
			&TransactionArgs{GasPrice: fortytwo},
			nil,
		},
		{
			"legacy tx post-London, explicit gas price",
			true,
			&TransactionArgs{GasPrice: fortytwo},
			&TransactionArgs{GasPrice: fortytwo},
			nil,
		},

		// Access list txs
		{
			"access list tx pre-London",
			false,
			&TransactionArgs{AccessList: al},
			&TransactionArgs{AccessList: al, GasPrice: fortytwo},
			nil,
		},
		{
			"access list tx post-London, explicit gas price",
			false,
			&TransactionArgs{AccessList: al, GasPrice: fortytwo},
			&TransactionArgs{AccessList: al, GasPrice: fortytwo},
			nil,
		},
		{
			"access list tx post-London",
			true,
			&TransactionArgs{AccessList: al},
			&TransactionArgs{AccessList: al, MaxFeePerGas: maxFee, MaxPriorityFeePerGas: fortytwo},
			nil,
		},
		{
			"access list tx post-London, only max fee",
			true,
			&TransactionArgs{AccessList: al, MaxFeePerGas: maxFee},
			&TransactionArgs{AccessList: al, MaxFeePerGas: maxFee, MaxPriorityFeePerGas: fortytwo},
			nil,
		},
		{
			"access list tx post-London, only priority fee",
			true,
			&TransactionArgs{AccessList: al, MaxFeePerGas: maxFee},
			&TransactionArgs{AccessList: al, MaxFeePerGas: maxFee, MaxPriorityFeePerGas: fortytwo},
			nil,
		},

		// Dynamic fee txs
		{
			"dynamic tx post-London",
			true,
			&TransactionArgs{},
			&TransactionArgs{MaxFeePerGas: maxFee, MaxPriorityFeePerGas: fortytwo},
			nil,
		},
		{
			"dynamic tx post-London, only max fee",
			true,
			&TransactionArgs{MaxFeePerGas: maxFee},
			&TransactionArgs{MaxFeePerGas: maxFee, MaxPriorityFeePerGas: fortytwo},
			nil,
		},
		{
			"dynamic tx post-London, only priority fee",
			true,
			&TransactionArgs{MaxFeePerGas: maxFee},
			&TransactionArgs{MaxFeePerGas: maxFee, MaxPriorityFeePerGas: fortytwo},
			nil,
		},
		{
			"dynamic fee tx pre-London, maxFee set",
			false,
			&TransactionArgs{MaxFeePerGas: maxFee},
			nil,
			errors.New("maxFeePerGas and maxPriorityFeePerGas are not valid before London is active"),
		},
		{
			"dynamic fee tx pre-London, priorityFee set",
			false,
			&TransactionArgs{MaxPriorityFeePerGas: fortytwo},
			nil,
			errors.New("maxFeePerGas and maxPriorityFeePerGas are not valid before London is active"),
		},
		{
			"dynamic fee tx, maxFee < priorityFee",
			true,
			&TransactionArgs{MaxFeePerGas: maxFee, MaxPriorityFeePerGas: (*hexutil.Big)(big.NewInt(1000))},
			nil,
			errors.New("maxFeePerGas (0x3e) < maxPriorityFeePerGas (0x3e8)"),
		},
		{
			"dynamic fee tx, maxFee < priorityFee while setting default",
			true,
			&TransactionArgs{MaxFeePerGas: (*hexutil.Big)(big.NewInt(7))},
			nil,
			errors.New("maxFeePerGas (0x7) < maxPriorityFeePerGas (0x2a)"),
		},

		// Misc
		{
			"set all fee parameters",
			false,
			&TransactionArgs{GasPrice: fortytwo, MaxFeePerGas: maxFee, MaxPriorityFeePerGas: fortytwo},
			nil,
			errors.New("both gasPrice and (maxFeePerGas or maxPriorityFeePerGas) specified"),
		},
		{
			"set gas price and maxPriorityFee",
			false,
			&TransactionArgs{GasPrice: fortytwo, MaxPriorityFeePerGas: fortytwo},
			nil,
			errors.New("both gasPrice and (maxFeePerGas or maxPriorityFeePerGas) specified"),
		},
		{
			"set gas price and maxFee",
			true,
			&TransactionArgs{GasPrice: fortytwo, MaxFeePerGas: maxFee},
			nil,
			errors.New("both gasPrice and (maxFeePerGas or maxPriorityFeePerGas) specified"),
		},
	}

	ctx := context.Background()
	for i, test := range tests {
		if test.isLondon {
			b.activateLondon()
		} else {
			b.deactivateLondon()
		}
		got := test.in
		err := got.setFeeDefaults(ctx, b)
		if err != nil && err.Error() == test.err.Error() {
			// Test threw expected error.
			continue
		} else if err != nil {
			t.Fatalf("test %d (%s): unexpected error: %s", i, test.name, err)
		}
		if !reflect.DeepEqual(got, test.want) {
			t.Fatalf("test %d (%s): did not fill defaults as expected: (got: %v, want: %v)", i, test.name, got, test.want)
		}
	}
}

type backendMock struct {
	current *types.Header
	config  *params.ChainConfig
}

func newBackendMock() *backendMock {
	config := &params.ChainConfig{
		ChainID:             big.NewInt(42),
		HomesteadBlock:      big.NewInt(0),
		EIP150Block:         big.NewInt(0),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		MuirGlacierBlock:    big.NewInt(0),
		MandatoryNetworkUpgrades: params.MandatoryNetworkUpgrades{
			SubnetEVMTimestamp: utils.NewUint64(1000),
		},
	}
	return &backendMock{
		current: &types.Header{
			Difficulty: big.NewInt(10000000000),
			Number:     big.NewInt(1100),
			GasLimit:   8_000_000,
			GasUsed:    8_000_000,
			Time:       555,
			Extra:      make([]byte, 32),
			BaseFee:    big.NewInt(10),
		},
		config: config,
	}
}

func (b *backendMock) activateLondon() {
	b.current.Time = uint64(1100)
}

func (b *backendMock) deactivateLondon() {
	b.current.Time = uint64(900)
}
func (b *backendMock) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	return big.NewInt(42), nil
}
func (b *backendMock) CurrentHeader() *types.Header     { return b.current }
func (b *backendMock) ChainConfig() *params.ChainConfig { return b.config }
