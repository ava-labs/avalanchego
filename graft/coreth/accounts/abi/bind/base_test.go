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
// Copyright 2019 The go-ethereum Authors
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

package bind_test

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"strings"
	"testing"

	"github.com/ava-labs/coreth/accounts/abi"
	"github.com/ava-labs/coreth/accounts/abi/bind"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/core/vm"
	"github.com/ava-labs/coreth/interfaces"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/assert"
)

func mockSign(addr common.Address, tx *types.Transaction) (*types.Transaction, error) { return tx, nil }

type mockTransactor struct {
	baseFee                *big.Int
	gasTipCap              *big.Int
	gasPrice               *big.Int
	suggestGasTipCapCalled bool
	suggestGasPriceCalled  bool
}

func (mt *mockTransactor) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	return &types.Header{BaseFee: mt.baseFee}, nil
}

func (mt *mockTransactor) AcceptedCodeAt(ctx context.Context, account common.Address) ([]byte, error) {
	return []byte{1}, nil
}

func (mt *mockTransactor) AcceptedNonceAt(ctx context.Context, account common.Address) (uint64, error) {
	return 0, nil
}

func (mt *mockTransactor) SuggestGasPrice(ctx context.Context) (*big.Int, error) {
	mt.suggestGasPriceCalled = true
	return mt.gasPrice, nil
}

func (mt *mockTransactor) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	mt.suggestGasTipCapCalled = true
	return mt.gasTipCap, nil
}

func (mt *mockTransactor) EstimateGas(ctx context.Context, call interfaces.CallMsg) (gas uint64, err error) {
	return 0, nil
}

func (mt *mockTransactor) SendTransaction(ctx context.Context, tx *types.Transaction) error {
	return nil
}

type mockCaller struct {
	codeAtBlockNumber       *big.Int
	callContractBlockNumber *big.Int
	callContractBytes       []byte
	callContractErr         error
	codeAtBytes             []byte
	codeAtErr               error
}

func (mc *mockCaller) CodeAt(ctx context.Context, contract common.Address, blockNumber *big.Int) ([]byte, error) {
	mc.codeAtBlockNumber = blockNumber
	return mc.codeAtBytes, mc.codeAtErr
}

func (mc *mockCaller) CallContract(ctx context.Context, call interfaces.CallMsg, blockNumber *big.Int) ([]byte, error) {
	mc.callContractBlockNumber = blockNumber
	return mc.callContractBytes, mc.callContractErr
}

type mockAcceptedCaller struct {
	*mockCaller
	acceptedCodeAtBytes        []byte
	acceptedCodeAtErr          error
	acceptedCodeAtCalled       bool
	acceptedCallContractCalled bool
	acceptedCallContractBytes  []byte
	acceptedCallContractErr    error
}

func (mc *mockAcceptedCaller) AcceptedCodeAt(ctx context.Context, contract common.Address) ([]byte, error) {
	mc.acceptedCodeAtCalled = true
	return mc.acceptedCodeAtBytes, mc.acceptedCodeAtErr
}

func (mc *mockAcceptedCaller) AcceptedCallContract(ctx context.Context, call interfaces.CallMsg) ([]byte, error) {
	mc.acceptedCallContractCalled = true
	return mc.acceptedCallContractBytes, mc.acceptedCallContractErr
}
func TestPassingBlockNumber(t *testing.T) {
	mc := &mockAcceptedCaller{
		mockCaller: &mockCaller{
			codeAtBytes: []byte{1, 2, 3},
		},
	}

	bc := bind.NewBoundContract(common.HexToAddress("0x0"), abi.ABI{
		Methods: map[string]abi.Method{
			"something": {
				Name:    "something",
				Outputs: abi.Arguments{},
			},
		},
	}, mc, nil, nil)

	blockNumber := big.NewInt(42)

	bc.Call(&bind.CallOpts{BlockNumber: blockNumber}, nil, "something")

	if mc.callContractBlockNumber != blockNumber {
		t.Fatalf("CallContract() was not passed the block number")
	}

	if mc.codeAtBlockNumber != blockNumber {
		t.Fatalf("CodeAt() was not passed the block number")
	}

	bc.Call(&bind.CallOpts{}, nil, "something")

	if mc.callContractBlockNumber != nil {
		t.Fatalf("CallContract() was passed a block number when it should not have been")
	}

	if mc.codeAtBlockNumber != nil {
		t.Fatalf("CodeAt() was passed a block number when it should not have been")
	}

	bc.Call(&bind.CallOpts{BlockNumber: blockNumber, Accepted: true}, nil, "something")

	if !mc.acceptedCallContractCalled {
		t.Fatalf("CallContract() was not passed the block number")
	}

	if !mc.acceptedCodeAtCalled {
		t.Fatalf("CodeAt() was not passed the block number")
	}
}

const hexData = "0x000000000000000000000000376c47978271565f56deb45495afa69e59c16ab200000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000060000000000000000000000000000000000000000000000000000000000000000158"

func TestUnpackIndexedStringTyLogIntoMap(t *testing.T) {
	hash := crypto.Keccak256Hash([]byte("testName"))
	topics := []common.Hash{
		crypto.Keccak256Hash([]byte("received(string,address,uint256,bytes)")),
		hash,
	}
	mockLog := newMockLog(topics, common.HexToHash("0x0"))

	abiString := `[{"anonymous":false,"inputs":[{"indexed":true,"name":"name","type":"string"},{"indexed":false,"name":"sender","type":"address"},{"indexed":false,"name":"amount","type":"uint256"},{"indexed":false,"name":"memo","type":"bytes"}],"name":"received","type":"event"}]`
	parsedAbi, _ := abi.JSON(strings.NewReader(abiString))
	bc := bind.NewBoundContract(common.HexToAddress("0x0"), parsedAbi, nil, nil, nil)

	expectedReceivedMap := map[string]interface{}{
		"name":   hash,
		"sender": common.HexToAddress("0x376c47978271565f56DEB45495afa69E59c16Ab2"),
		"amount": big.NewInt(1),
		"memo":   []byte{88},
	}
	unpackAndCheck(t, bc, expectedReceivedMap, mockLog)
}

func TestUnpackAnonymousLogIntoMap(t *testing.T) {
	mockLog := newMockLog(nil, common.HexToHash("0x0"))

	abiString := `[{"anonymous":false,"inputs":[{"indexed":false,"name":"amount","type":"uint256"}],"name":"received","type":"event"}]`
	parsedAbi, _ := abi.JSON(strings.NewReader(abiString))
	bc := bind.NewBoundContract(common.HexToAddress("0x0"), parsedAbi, nil, nil, nil)

	var received map[string]interface{}
	err := bc.UnpackLogIntoMap(received, "received", mockLog)
	if err == nil {
		t.Error("unpacking anonymous event is not supported")
	}
	if err.Error() != "no event signature" {
		t.Errorf("expected error 'no event signature', got '%s'", err)
	}
}

func TestUnpackIndexedSliceTyLogIntoMap(t *testing.T) {
	sliceBytes, err := rlp.EncodeToBytes([]string{"name1", "name2", "name3", "name4"})
	if err != nil {
		t.Fatal(err)
	}
	hash := crypto.Keccak256Hash(sliceBytes)
	topics := []common.Hash{
		crypto.Keccak256Hash([]byte("received(string[],address,uint256,bytes)")),
		hash,
	}
	mockLog := newMockLog(topics, common.HexToHash("0x0"))

	abiString := `[{"anonymous":false,"inputs":[{"indexed":true,"name":"names","type":"string[]"},{"indexed":false,"name":"sender","type":"address"},{"indexed":false,"name":"amount","type":"uint256"},{"indexed":false,"name":"memo","type":"bytes"}],"name":"received","type":"event"}]`
	parsedAbi, _ := abi.JSON(strings.NewReader(abiString))
	bc := bind.NewBoundContract(common.HexToAddress("0x0"), parsedAbi, nil, nil, nil)

	expectedReceivedMap := map[string]interface{}{
		"names":  hash,
		"sender": common.HexToAddress("0x376c47978271565f56DEB45495afa69E59c16Ab2"),
		"amount": big.NewInt(1),
		"memo":   []byte{88},
	}
	unpackAndCheck(t, bc, expectedReceivedMap, mockLog)
}

func TestUnpackIndexedArrayTyLogIntoMap(t *testing.T) {
	arrBytes, err := rlp.EncodeToBytes([2]common.Address{common.HexToAddress("0x0"), common.HexToAddress("0x376c47978271565f56DEB45495afa69E59c16Ab2")})
	if err != nil {
		t.Fatal(err)
	}
	hash := crypto.Keccak256Hash(arrBytes)
	topics := []common.Hash{
		crypto.Keccak256Hash([]byte("received(address[2],address,uint256,bytes)")),
		hash,
	}
	mockLog := newMockLog(topics, common.HexToHash("0x0"))

	abiString := `[{"anonymous":false,"inputs":[{"indexed":true,"name":"addresses","type":"address[2]"},{"indexed":false,"name":"sender","type":"address"},{"indexed":false,"name":"amount","type":"uint256"},{"indexed":false,"name":"memo","type":"bytes"}],"name":"received","type":"event"}]`
	parsedAbi, _ := abi.JSON(strings.NewReader(abiString))
	bc := bind.NewBoundContract(common.HexToAddress("0x0"), parsedAbi, nil, nil, nil)

	expectedReceivedMap := map[string]interface{}{
		"addresses": hash,
		"sender":    common.HexToAddress("0x376c47978271565f56DEB45495afa69E59c16Ab2"),
		"amount":    big.NewInt(1),
		"memo":      []byte{88},
	}
	unpackAndCheck(t, bc, expectedReceivedMap, mockLog)
}

func TestUnpackIndexedFuncTyLogIntoMap(t *testing.T) {
	mockAddress := common.HexToAddress("0x376c47978271565f56DEB45495afa69E59c16Ab2")
	addrBytes := mockAddress.Bytes()
	hash := crypto.Keccak256Hash([]byte("mockFunction(address,uint)"))
	functionSelector := hash[:4]
	functionTyBytes := append(addrBytes, functionSelector...)
	var functionTy [24]byte
	copy(functionTy[:], functionTyBytes[0:24])
	topics := []common.Hash{
		crypto.Keccak256Hash([]byte("received(function,address,uint256,bytes)")),
		common.BytesToHash(functionTyBytes),
	}
	mockLog := newMockLog(topics, common.HexToHash("0x5c698f13940a2153440c6d19660878bc90219d9298fdcf37365aa8d88d40fc42"))
	abiString := `[{"anonymous":false,"inputs":[{"indexed":true,"name":"function","type":"function"},{"indexed":false,"name":"sender","type":"address"},{"indexed":false,"name":"amount","type":"uint256"},{"indexed":false,"name":"memo","type":"bytes"}],"name":"received","type":"event"}]`
	parsedAbi, _ := abi.JSON(strings.NewReader(abiString))
	bc := bind.NewBoundContract(common.HexToAddress("0x0"), parsedAbi, nil, nil, nil)

	expectedReceivedMap := map[string]interface{}{
		"function": functionTy,
		"sender":   common.HexToAddress("0x376c47978271565f56DEB45495afa69E59c16Ab2"),
		"amount":   big.NewInt(1),
		"memo":     []byte{88},
	}
	unpackAndCheck(t, bc, expectedReceivedMap, mockLog)
}

func TestUnpackIndexedBytesTyLogIntoMap(t *testing.T) {
	bytes := []byte{1, 2, 3, 4, 5}
	hash := crypto.Keccak256Hash(bytes)
	topics := []common.Hash{
		crypto.Keccak256Hash([]byte("received(bytes,address,uint256,bytes)")),
		hash,
	}
	mockLog := newMockLog(topics, common.HexToHash("0x5c698f13940a2153440c6d19660878bc90219d9298fdcf37365aa8d88d40fc42"))

	abiString := `[{"anonymous":false,"inputs":[{"indexed":true,"name":"content","type":"bytes"},{"indexed":false,"name":"sender","type":"address"},{"indexed":false,"name":"amount","type":"uint256"},{"indexed":false,"name":"memo","type":"bytes"}],"name":"received","type":"event"}]`
	parsedAbi, _ := abi.JSON(strings.NewReader(abiString))
	bc := bind.NewBoundContract(common.HexToAddress("0x0"), parsedAbi, nil, nil, nil)

	expectedReceivedMap := map[string]interface{}{
		"content": hash,
		"sender":  common.HexToAddress("0x376c47978271565f56DEB45495afa69E59c16Ab2"),
		"amount":  big.NewInt(1),
		"memo":    []byte{88},
	}
	unpackAndCheck(t, bc, expectedReceivedMap, mockLog)
}

func TestTransactNativeAssetCallNilAssetAmount(t *testing.T) {
	assert := assert.New(t)
	mt := &mockTransactor{}
	bc := bind.NewBoundContract(common.Address{}, abi.ABI{}, nil, mt, nil)
	opts := &bind.TransactOpts{
		Signer: mockSign,
	}
	// fails if asset amount is nil
	opts.NativeAssetCall = &bind.NativeAssetCallOpts{
		AssetID:     common.Hash{},
		AssetAmount: nil,
	}
	_, err := bc.Transact(opts, "")
	assert.ErrorIs(err, bind.ErrNilAssetAmount)
}

func TestTransactNativeAssetCallNonZeroValue(t *testing.T) {
	assert := assert.New(t)
	mt := &mockTransactor{}
	bc := bind.NewBoundContract(common.Address{}, abi.ABI{}, nil, mt, nil)
	opts := &bind.TransactOpts{
		Signer: mockSign,
	}
	opts.NativeAssetCall = &bind.NativeAssetCallOpts{
		AssetID:     common.Hash{},
		AssetAmount: big.NewInt(11),
	}
	// fails if value > 0
	opts.Value = big.NewInt(11)
	_, err := bc.Transact(opts, "")
	assert.Equal(err.Error(), fmt.Sprintf("value must be 0 when performing native asset call, found %v", opts.Value))
	// fails if value < 0
	opts.Value = big.NewInt(-11)
	_, err = bc.Transact(opts, "")
	assert.Equal(err.Error(), fmt.Sprintf("value must be 0 when performing native asset call, found %v", opts.Value))
}

func TestTransactNativeAssetCall(t *testing.T) {
	assert := assert.New(t)
	json := `[{"type":"function","name":"method","inputs":[{"type":"uint256" },{"type":"string"}]}]`
	parsed, err := abi.JSON(strings.NewReader(json))
	assert.Nil(err)
	mt := &mockTransactor{}
	contractAddr := common.Address{11}
	bc := bind.NewBoundContract(contractAddr, parsed, nil, mt, nil)
	opts := &bind.TransactOpts{
		Signer: mockSign,
	}
	// normal call tx
	methodName := "method"
	arg1 := big.NewInt(22)
	arg2 := "33"
	normalCallTx, err := bc.Transact(opts, methodName, arg1, arg2)
	assert.Nil(err)
	// native asset call tx
	assetID := common.Hash{44}
	assetAmount := big.NewInt(55)
	opts.NativeAssetCall = &bind.NativeAssetCallOpts{
		AssetID:     assetID,
		AssetAmount: assetAmount,
	}
	nativeCallTx, err := bc.Transact(opts, methodName, arg1, arg2)
	assert.Nil(err)
	// verify transformations
	assert.Equal(vm.NativeAssetCallAddr, *nativeCallTx.To())
	unpackedAddr, unpackedAssetID, unpackedAssetAmount, unpackedData, err := vm.UnpackNativeAssetCallInput(nativeCallTx.Data())
	assert.Nil(err)
	assert.NotEmpty(unpackedData)
	assert.Equal(unpackedData, normalCallTx.Data())
	assert.Equal(unpackedAddr, contractAddr)
	assert.Equal(unpackedAssetID, assetID)
	assert.Equal(unpackedAssetAmount, assetAmount)
}

func TestTransactGasFee(t *testing.T) {
	assert := assert.New(t)

	// GasTipCap and GasFeeCap
	// When opts.GasTipCap and opts.GasFeeCap are nil
	mt := &mockTransactor{baseFee: big.NewInt(100), gasTipCap: big.NewInt(5)}
	bc := bind.NewBoundContract(common.Address{}, abi.ABI{}, nil, mt, nil)
	opts := &bind.TransactOpts{Signer: mockSign}
	tx, err := bc.Transact(opts, "")
	assert.Nil(err)
	assert.Equal(big.NewInt(5), tx.GasTipCap())
	assert.Equal(big.NewInt(205), tx.GasFeeCap())
	assert.Nil(opts.GasTipCap)
	assert.Nil(opts.GasFeeCap)
	assert.True(mt.suggestGasTipCapCalled)

	// Second call to Transact should use latest suggested GasTipCap
	mt.gasTipCap = big.NewInt(6)
	mt.suggestGasTipCapCalled = false
	tx, err = bc.Transact(opts, "")
	assert.Nil(err)
	assert.Equal(big.NewInt(6), tx.GasTipCap())
	assert.Equal(big.NewInt(206), tx.GasFeeCap())
	assert.True(mt.suggestGasTipCapCalled)

	// GasPrice
	// When opts.GasPrice is nil
	mt = &mockTransactor{gasPrice: big.NewInt(5)}
	bc = bind.NewBoundContract(common.Address{}, abi.ABI{}, nil, mt, nil)
	opts = &bind.TransactOpts{Signer: mockSign}
	tx, err = bc.Transact(opts, "")
	assert.Nil(err)
	assert.Equal(big.NewInt(5), tx.GasPrice())
	assert.Nil(opts.GasPrice)
	assert.True(mt.suggestGasPriceCalled)

	// Second call to Transact should use latest suggested GasPrice
	mt.gasPrice = big.NewInt(6)
	mt.suggestGasPriceCalled = false
	tx, err = bc.Transact(opts, "")
	assert.Nil(err)
	assert.Equal(big.NewInt(6), tx.GasPrice())
	assert.True(mt.suggestGasPriceCalled)
}

func unpackAndCheck(t *testing.T, bc *bind.BoundContract, expected map[string]interface{}, mockLog types.Log) {
	received := make(map[string]interface{})
	if err := bc.UnpackLogIntoMap(received, "received", mockLog); err != nil {
		t.Error(err)
	}

	if len(received) != len(expected) {
		t.Fatalf("unpacked map length %v not equal expected length of %v", len(received), len(expected))
	}
	for name, elem := range expected {
		if !reflect.DeepEqual(elem, received[name]) {
			t.Errorf("field %v does not match expected, want %v, got %v", name, elem, received[name])
		}
	}
}

func newMockLog(topics []common.Hash, txHash common.Hash) types.Log {
	return types.Log{
		Address:     common.HexToAddress("0x0"),
		Topics:      topics,
		Data:        hexutil.MustDecode(hexData),
		BlockNumber: uint64(26),
		TxHash:      txHash,
		TxIndex:     111,
		BlockHash:   common.BytesToHash([]byte{1, 2, 3, 4, 5}),
		Index:       7,
		Removed:     false,
	}
}

func TestCall(t *testing.T) {
	var method, methodWithArg = "something", "somethingArrrrg"
	tests := []struct {
		name, method string
		opts         *bind.CallOpts
		mc           bind.ContractCaller
		results      *[]interface{}
		wantErr      bool
		wantErrExact error
	}{{
		name: "ok not accepted",
		mc: &mockCaller{
			codeAtBytes: []byte{0},
		},
		method: method,
	}, {
		name: "ok accepted",
		mc: &mockAcceptedCaller{
			acceptedCodeAtBytes: []byte{0},
		},
		opts: &bind.CallOpts{
			Accepted: true,
		},
		method: method,
	}, {
		name:    "pack error, no method",
		mc:      new(mockCaller),
		method:  "else",
		wantErr: true,
	}, {
		name: "interface error, accepted but not a AcceptedContractCaller",
		mc:   new(mockCaller),
		opts: &bind.CallOpts{
			Accepted: true,
		},
		method:       method,
		wantErrExact: bind.ErrNoAcceptedState,
	}, {
		name: "accepted call canceled",
		mc: &mockAcceptedCaller{
			acceptedCallContractErr: context.DeadlineExceeded,
		},
		opts: &bind.CallOpts{
			Accepted: true,
		},
		method:       method,
		wantErrExact: context.DeadlineExceeded,
	}, {
		name: "accepted code at error",
		mc: &mockAcceptedCaller{
			acceptedCodeAtErr: errors.New(""),
		},
		opts: &bind.CallOpts{
			Accepted: true,
		},
		method:  method,
		wantErr: true,
	}, {
		name: "no accepted code at",
		mc:   new(mockAcceptedCaller),
		opts: &bind.CallOpts{
			Accepted: true,
		},
		method:       method,
		wantErrExact: bind.ErrNoCode,
	}, {
		name: "call contract error",
		mc: &mockCaller{
			callContractErr: context.DeadlineExceeded,
		},
		method:       method,
		wantErrExact: context.DeadlineExceeded,
	}, {
		name: "code at error",
		mc: &mockCaller{
			codeAtErr: errors.New(""),
		},
		method:  method,
		wantErr: true,
	}, {
		name:         "no code at",
		mc:           new(mockCaller),
		method:       method,
		wantErrExact: bind.ErrNoCode,
	}, {
		name: "unpack error missing arg",
		mc: &mockCaller{
			codeAtBytes: []byte{0},
		},
		method:  methodWithArg,
		wantErr: true,
	}, {
		name: "interface unpack error",
		mc: &mockCaller{
			codeAtBytes: []byte{0},
		},
		method:  method,
		results: &[]interface{}{0},
		wantErr: true,
	}}
	for _, test := range tests {
		bc := bind.NewBoundContract(common.HexToAddress("0x0"), abi.ABI{
			Methods: map[string]abi.Method{
				method: {
					Name:    method,
					Outputs: abi.Arguments{},
				},
				methodWithArg: {
					Name:    methodWithArg,
					Outputs: abi.Arguments{abi.Argument{}},
				},
			},
		}, test.mc, nil, nil)
		err := bc.Call(test.opts, test.results, test.method)
		if test.wantErr || test.wantErrExact != nil {
			if err == nil {
				t.Fatalf("%q expected error", test.name)
			}
			if test.wantErrExact != nil && !errors.Is(err, test.wantErrExact) {
				t.Fatalf("%q expected error %q but got %q", test.name, test.wantErrExact, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q unexpected error: %v", test.name, err)
		}
	}
}

// TestCrashers contains some strings which previously caused the abi codec to crash.
func TestCrashers(t *testing.T) {
	abi.JSON(strings.NewReader(`[{"inputs":[{"type":"tuple[]","components":[{"type":"bool","name":"_1"}]}]}]`))
	abi.JSON(strings.NewReader(`[{"inputs":[{"type":"tuple[]","components":[{"type":"bool","name":"&"}]}]}]`))
	abi.JSON(strings.NewReader(`[{"inputs":[{"type":"tuple[]","components":[{"type":"bool","name":"----"}]}]}]`))
	abi.JSON(strings.NewReader(`[{"inputs":[{"type":"tuple[]","components":[{"type":"bool","name":"foo.Bar"}]}]}]`))
}
