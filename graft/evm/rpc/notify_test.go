// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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

package rpc_test

import (
	"bytes"
	"math/big"
	"strings"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/libevmtest"
	"github.com/ava-labs/avalanchego/graft/evm/rpc"
)

func TestNotify(t *testing.T) {
	out := new(bytes.Buffer)
	id := rpc.ID("test")
	notifier := rpc.NewTestNotifier(out, id)

	msg := &types.Header{
		ParentHash: common.HexToHash("0x01"),
		Number:     big.NewInt(100),
	}
	require.NoError(t, notifier.Notify(id, msg))
	have := strings.TrimSpace(out.String())

	// Expected JSON for C-Chain (includes extDataHash and extDataGasUsed)
	wantCChain := `{"jsonrpc":"2.0","method":"_subscription","params":{"subscription":"test","result":{"parentHash":"0x0000000000000000000000000000000000000000000000000000000000000001","sha3Uncles":"0x0000000000000000000000000000000000000000000000000000000000000000","miner":"0x0000000000000000000000000000000000000000","stateRoot":"0x0000000000000000000000000000000000000000000000000000000000000000","transactionsRoot":"0x0000000000000000000000000000000000000000000000000000000000000000","receiptsRoot":"0x0000000000000000000000000000000000000000000000000000000000000000","logsBloom":"0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000","difficulty":null,"number":"0x64","gasLimit":"0x0","gasUsed":"0x0","timestamp":"0x0","extraData":"0x","mixHash":"0x0000000000000000000000000000000000000000000000000000000000000000","nonce":"0x0000000000000000","extDataHash":"0x0000000000000000000000000000000000000000000000000000000000000000","baseFeePerGas":null,"extDataGasUsed":null,"blockGasCost":null,"blobGasUsed":null,"excessBlobGas":null,"parentBeaconBlockRoot":null,"timestampMilliseconds":null,"minDelayExcess":null,"hash":"0x9a9ca1b5790a674785245afedcee9fc1a90d3514c6faa1cbd26f696136d6fd12"}}}`

	// Expected JSON for Subnet-EVM (no extDataHash or extDataGasUsed)
	wantSubnetEVM := `{"jsonrpc":"2.0","method":"_subscription","params":{"subscription":"test","result":{"parentHash":"0x0000000000000000000000000000000000000000000000000000000000000001","sha3Uncles":"0x0000000000000000000000000000000000000000000000000000000000000000","miner":"0x0000000000000000000000000000000000000000","stateRoot":"0x0000000000000000000000000000000000000000000000000000000000000000","transactionsRoot":"0x0000000000000000000000000000000000000000000000000000000000000000","receiptsRoot":"0x0000000000000000000000000000000000000000000000000000000000000000","logsBloom":"0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000","difficulty":null,"number":"0x64","gasLimit":"0x0","gasUsed":"0x0","timestamp":"0x0","extraData":"0x","mixHash":"0x0000000000000000000000000000000000000000000000000000000000000000","nonce":"0x0000000000000000","baseFeePerGas":null,"blockGasCost":null,"blobGasUsed":null,"excessBlobGas":null,"parentBeaconBlockRoot":null,"timestampMilliseconds":null,"minDelayExcess":null,"hash":"0xe5fb877dde471b45b9742bb4bb4b3d74a761e2fb7cb849a3d2b687eed90fb604"}}}`

	switch libevmtest.CurrentVariant() {
	case libevmtest.CChainVariant:
		require.Equal(t, wantCChain, have)
	case libevmtest.SubnetEVMVariant:
		require.Equal(t, wantSubnetEVM, have)
	default:
		require.FailNow(t, "libevm extras not registered (use libevmtest to aid tracking registrations)")
	}
}
