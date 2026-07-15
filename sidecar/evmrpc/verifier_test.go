// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmrpc

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/network/p2p/oracle"
)

const (
	testContract = "0xA50A51c09a5c451C52BB714527E1974b686D8e77"
	testPayload  = "0x00000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000020"
)

var testTxHash = make([]byte, 32)

type stubClient struct {
	receipt    *txReceipt
	receiptErr error
	tagHeight  uint64
	tagErr     error
}

func (s *stubClient) getTransactionReceipt(context.Context, string) (*txReceipt, error) {
	return s.receipt, s.receiptErr
}

func (s *stubClient) getBlockNumberByTag(context.Context, string) (uint64, error) {
	return s.tagHeight, s.tagErr
}

func testMessage(t *testing.T, height uint64) *oracle.OracleMessage {
	t.Helper()
	payload, err := hexBytes(testPayload)
	require.NoError(t, err)
	msg, err := oracle.NewOracleMessage(oracle.SourceTypeEVM, testContract, common.Address{1}, height, 1, payload)
	require.NoError(t, err)
	return msg
}

func goodReceipt() *txReceipt {
	return &txReceipt{
		Status:      "0x1",
		BlockNumber: "0x64", // 100
		Logs: []txLog{
			{Address: testContract, Data: testPayload},
		},
	}
}

func newTestVerifier(t *testing.T, client rpcClient, finality string, allowed []string) *EVMVerifier {
	t.Helper()
	allowedMap := make(map[string]struct{}, len(allowed))
	for _, a := range allowed {
		allowedMap[a] = struct{}{}
	}
	return &EVMVerifier{client: client, allowedContracts: allowedMap, finality: finality}
}

func TestNewEVMVerifierConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr bool
	}{
		{name: "valid minimal", config: `{"rpc_url":"http://localhost:9545"}`},
		{name: "valid full", config: `{"rpc_url":"http://localhost:9545","allowed_contracts":["` + testContract + `"],"finality":"finalized"}`},
		{name: "missing rpc_url", config: `{}`, wantErr: true},
		{name: "empty config bytes", config: "", wantErr: true},
		{name: "unknown finality", config: `{"rpc_url":"x","finality":"probably"}`, wantErr: true},
		{name: "invalid json", config: `{`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewEVMVerifier([]byte(tt.config), nil)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestVerify(t *testing.T) {
	successReceipt := goodReceipt()

	failedReceipt := goodReceipt()
	failedReceipt.Status = "0x0"

	wrongDataReceipt := goodReceipt()
	wrongDataReceipt.Logs[0].Data = "0xdeadbeef"

	wrongContractReceipt := goodReceipt()
	wrongContractReceipt.Logs[0].Address = "0x0000000000000000000000000000000000000001"

	tests := []struct {
		name    string
		client  *stubClient
		height  uint64
		wantErr string
	}{
		{
			name:   "valid",
			client: &stubClient{receipt: successReceipt},
			height: 100,
		},
		{
			name:    "transaction not found",
			client:  &stubClient{},
			height:  100,
			wantErr: "transaction not found",
		},
		{
			name:    "rpc unavailable",
			client:  &stubClient{receiptErr: errors.New("connection refused")},
			height:  100,
			wantErr: "source chain unavailable",
		},
		{
			name:    "reverted transaction",
			client:  &stubClient{receipt: failedReceipt},
			height:  100,
			wantErr: "did not succeed",
		},
		{
			name:    "block height mismatch",
			client:  &stubClient{receipt: successReceipt},
			height:  99,
			wantErr: "block height mismatch",
		},
		{
			name:    "payload mismatch",
			client:  &stubClient{receipt: wrongDataReceipt},
			height:  100,
			wantErr: "no matching log",
		},
		{
			name:    "log from different contract",
			client:  &stubClient{receipt: wrongContractReceipt},
			height:  100,
			wantErr: "no matching log",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newTestVerifier(t, tt.client, FinalityLatest, nil)
			err := v.Verify(t.Context(), testMessage(t, tt.height), testTxHash)
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}

func TestVerifyFinality(t *testing.T) {
	tests := []struct {
		name      string
		finality  string
		tagHeight uint64
		tagErr    error
		wantErr   string
	}{
		{name: "latest ignores tag", finality: FinalityLatest, tagErr: errors.New("must not be called")},
		{name: "finalized past block", finality: FinalityFinalized, tagHeight: 100},
		{name: "finalized exactly at tag", finality: FinalityFinalized, tagHeight: 100},
		{name: "not yet finalized", finality: FinalityFinalized, tagHeight: 99, wantErr: "not yet finalized"},
		{name: "tag lookup fails", finality: FinalityFinalized, tagErr: errors.New("unknown block"), wantErr: "source chain unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &stubClient{receipt: goodReceipt(), tagHeight: tt.tagHeight, tagErr: tt.tagErr}
			v := newTestVerifier(t, client, tt.finality, nil)
			err := v.Verify(t.Context(), testMessage(t, 100), testTxHash)
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}

func TestVerifyAllowlist(t *testing.T) {
	tests := []struct {
		name    string
		allowed []string
		wantErr string
	}{
		{name: "empty allowlist allows all"},
		{name: "contract allowed (lowercased)", allowed: []string{"0xa50a51c09a5c451c52bb714527e1974b686d8e77"}},
		{name: "contract not allowed", allowed: []string{"0x0000000000000000000000000000000000000001"}, wantErr: "not in the allowed contracts"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newTestVerifier(t, &stubClient{receipt: goodReceipt()}, FinalityLatest, tt.allowed)
			err := v.Verify(t.Context(), testMessage(t, 100), testTxHash)
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}

func TestVerifyJustificationLength(t *testing.T) {
	v := newTestVerifier(t, &stubClient{receipt: goodReceipt()}, FinalityLatest, nil)

	short, err := hex.DecodeString("deadbeef")
	require.NoError(t, err)
	err = v.Verify(t.Context(), testMessage(t, 100), short)
	require.ErrorContains(t, err, "32-byte transaction hash")
}
