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

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

var _ feeBackend = &backendMock{}

// TestSetFeeDefaults tests the logic for filling in default fee values works as expected.
func TestSetFeeDefaults(t *testing.T) {
	type test struct {
		name string
		fork string // options: legacy, london, cancun
		in   *TransactionArgs
		want *TransactionArgs
		err  error
	}

	var (
		b        = newBackendMock()
		zero     = (*hexutil.Big)(big.NewInt(0))
		fortytwo = (*hexutil.Big)(big.NewInt(42))
		maxFee   = (*hexutil.Big)(new(big.Int).Add(new(big.Int).Mul(b.current.BaseFee, big.NewInt(2)), fortytwo.ToInt()))
		al       = &types.AccessList{types.AccessTuple{Address: common.Address{0xaa}, StorageKeys: []common.Hash{{0x01}}}}
	)

	tests := []test{
		// Legacy txs
		{
			"legacy tx pre-London",
			"legacy",
			&TransactionArgs{},
			&TransactionArgs{GasPrice: fortytwo},
			nil,
		},
		{
			"legacy tx pre-London with zero price",
			"legacy",
			&TransactionArgs{GasPrice: zero},
			&TransactionArgs{GasPrice: zero},
			nil,
		},
		{
			"legacy tx post-London, explicit gas price",
			"london",
			&TransactionArgs{GasPrice: fortytwo},
			&TransactionArgs{GasPrice: fortytwo},
			nil,
		},
		{
			"legacy tx post-London with zero price",
			"london",
			&TransactionArgs{GasPrice: zero},
			nil,
			errors.New("gasPrice must be non-zero after london fork"),
		},

		// Access list txs
		{
			"access list tx pre-London",
			"legacy",
			&TransactionArgs{AccessList: al},
			&TransactionArgs{AccessList: al, GasPrice: fortytwo},
			nil,
		},
		{
			"access list tx post-London, explicit gas price",
			"legacy",
			&TransactionArgs{AccessList: al, GasPrice: fortytwo},
			&TransactionArgs{AccessList: al, GasPrice: fortytwo},
			nil,
		},
		{
			"access list tx post-London",
			"london",
			&TransactionArgs{AccessList: al},
			&TransactionArgs{AccessList: al, MaxFeePerGas: maxFee, MaxPriorityFeePerGas: fortytwo},
			nil,
		},
		{
			"access list tx post-London, only max fee",
			"london",
			&TransactionArgs{AccessList: al, MaxFeePerGas: maxFee},
			&TransactionArgs{AccessList: al, MaxFeePerGas: maxFee, MaxPriorityFeePerGas: fortytwo},
			nil,
		},
		{
			"access list tx post-London, only priority fee",
			"london",
			&TransactionArgs{AccessList: al, MaxFeePerGas: maxFee},
			&TransactionArgs{AccessList: al, MaxFeePerGas: maxFee, MaxPriorityFeePerGas: fortytwo},
			nil,
		},

		// Dynamic fee txs
		{
			"dynamic tx post-London",
			"london",
			&TransactionArgs{},
			&TransactionArgs{MaxFeePerGas: maxFee, MaxPriorityFeePerGas: fortytwo},
			nil,
		},
		{
			"dynamic tx post-London, only max fee",
			"london",
			&TransactionArgs{MaxFeePerGas: maxFee},
			&TransactionArgs{MaxFeePerGas: maxFee, MaxPriorityFeePerGas: fortytwo},
			nil,
		},
		{
			"dynamic tx post-London, only priority fee",
			"london",
			&TransactionArgs{MaxFeePerGas: maxFee},
			&TransactionArgs{MaxFeePerGas: maxFee, MaxPriorityFeePerGas: fortytwo},
			nil,
		},
		{
			"dynamic fee tx pre-London, maxFee set",
			"legacy",
			&TransactionArgs{MaxFeePerGas: maxFee},
			nil,
			errors.New("maxFeePerGas and maxPriorityFeePerGas are not valid before London is active"),
		},
		{
			"dynamic fee tx pre-London, priorityFee set",
			"legacy",
			&TransactionArgs{MaxPriorityFeePerGas: fortytwo},
			nil,
			errors.New("maxFeePerGas and maxPriorityFeePerGas are not valid before London is active"),
		},
		{
			"dynamic fee tx, maxFee < priorityFee",
			"london",
			&TransactionArgs{MaxFeePerGas: maxFee, MaxPriorityFeePerGas: (*hexutil.Big)(big.NewInt(1000))},
			nil,
			errors.New("maxFeePerGas (0x3e) < maxPriorityFeePerGas (0x3e8)"),
		},
		{
			"dynamic fee tx, maxFee < priorityFee while setting default",
			"london",
			&TransactionArgs{MaxFeePerGas: (*hexutil.Big)(big.NewInt(7))},
			nil,
			errors.New("maxFeePerGas (0x7) < maxPriorityFeePerGas (0x2a)"),
		},
		{
			"dynamic fee tx post-London, explicit gas price",
			"london",
			&TransactionArgs{MaxFeePerGas: zero, MaxPriorityFeePerGas: zero},
			nil,
			errors.New("maxFeePerGas must be non-zero"),
		},

		// Misc
		{
			"set all fee parameters",
			"legacy",
			&TransactionArgs{GasPrice: fortytwo, MaxFeePerGas: maxFee, MaxPriorityFeePerGas: fortytwo},
			nil,
			errors.New("both gasPrice and (maxFeePerGas or maxPriorityFeePerGas) specified"),
		},
		{
			"set gas price and maxPriorityFee",
			"legacy",
			&TransactionArgs{GasPrice: fortytwo, MaxPriorityFeePerGas: fortytwo},
			nil,
			errors.New("both gasPrice and (maxFeePerGas or maxPriorityFeePerGas) specified"),
		},
		{
			"set gas price and maxFee",
			"london",
			&TransactionArgs{GasPrice: fortytwo, MaxFeePerGas: maxFee},
			nil,
			errors.New("both gasPrice and (maxFeePerGas or maxPriorityFeePerGas) specified"),
		},
		// EIP-4844
		{
			"set gas price and maxFee for blob transaction",
			"cancun",
			&TransactionArgs{GasPrice: fortytwo, MaxFeePerGas: maxFee, BlobHashes: []common.Hash{}},
			nil,
			errors.New("both gasPrice and (maxFeePerGas or maxPriorityFeePerGas) specified"),
		},
		{
			"fill maxFeePerBlobGas",
			"cancun",
			&TransactionArgs{BlobHashes: []common.Hash{}},
			&TransactionArgs{BlobHashes: []common.Hash{}, BlobFeeCap: (*hexutil.Big)(big.NewInt(4)), MaxFeePerGas: maxFee, MaxPriorityFeePerGas: fortytwo},
			nil,
		},
		{
			"fill maxFeePerBlobGas when dynamic fees are set",
			"cancun",
			&TransactionArgs{BlobHashes: []common.Hash{}, MaxFeePerGas: maxFee, MaxPriorityFeePerGas: fortytwo},
			&TransactionArgs{BlobHashes: []common.Hash{}, BlobFeeCap: (*hexutil.Big)(big.NewInt(4)), MaxFeePerGas: maxFee, MaxPriorityFeePerGas: fortytwo},
			nil,
		},
	}

	ctx := context.Background()
	for i, test := range tests {
		if err := b.setFork(test.fork); err != nil {
			t.Fatalf("failed to set fork: %v", err)
		}
		got := test.in
		err := got.setFeeDefaults(ctx, b)
		if err != nil {
			if test.err == nil {
				t.Fatalf("test %d (%s): unexpected error: %s", i, test.name, err)
			} else if err.Error() != test.err.Error() {
				t.Fatalf("test %d (%s): unexpected error: (got: %s, want: %s)", i, test.name, err, test.err)
			}
			// Matching error.
			continue
		} else if test.err != nil {
			t.Fatalf("test %d (%s): expected error: %s", i, test.name, test.err)
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
	var cancunTime uint64 = 600
	config := &params.ChainConfig{
		ChainID:             big.NewInt(42),
		HomesteadBlock:      big.NewInt(0),
		DAOForkBlock:        nil,
		DAOForkSupport:      true,
		EIP150Block:         big.NewInt(0),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		MuirGlacierBlock:    big.NewInt(0),
		CancunTime:          &cancunTime,
		NetworkUpgrades: params.NetworkUpgrades{
			ApricotPhase3BlockTimestamp: utils.NewUint64(100),
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

func (b *backendMock) setFork(fork string) error {
	if fork == "legacy" {
		b.current.Time = uint64(90) // Before ApricotPhase3BlockTimestamp
	} else if fork == "london" {
		b.current.Time = uint64(110) // After ApricotPhase3BlockTimestamp
	} else if fork == "cancun" {
		b.current.Number = big.NewInt(1100)
		b.current.Time = 700
		// Blob base fee will be 2
		excess := uint64(2314058)
		b.current.ExcessBlobGas = &excess
	} else {
		return errors.New("invalid fork")
	}
	return nil
}

func (b *backendMock) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	return big.NewInt(42), nil
}
func (b *backendMock) CurrentHeader() *types.Header     { return b.current }
func (b *backendMock) ChainConfig() *params.ChainConfig { return b.config }
