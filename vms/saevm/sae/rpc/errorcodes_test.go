// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseErrorCodes(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "empty",
		},
		{
			name: "single_result",
			body: `{"jsonrpc":"2.0","id":1,"result":"0x1"}`,
		},
		{
			name: "single_error",
			body: `{"jsonrpc":"2.0","id":1,"error":{"code":3,"message":"execution reverted","data":"0x2a"}}` + "\n",
			want: "3;count=1",
		},
		{
			name: "single_null_error",
			body: `{"jsonrpc":"2.0","id":1,"error":null,"result":"0x1"}`,
		},
		{
			name: "result_before_error_field",
			body: `{"id":1,"result":{"error":{"code":-1}}}`,
		},
		{
			name: "batch",
			body: `[` +
				`{"jsonrpc":"2.0","id":1,"result":{"error":{"code":-1}}},` +
				`{"jsonrpc":"2.0","id":2,"error":{"code":-32002,"message":"request timed out"}},` +
				`{"jsonrpc":"2.0","id":3,"error":{"code":3,"message":"execution reverted"}},` +
				`{"jsonrpc":"2.0","id":4,"error":{"code":3,"message":"execution reverted"}}` +
				`]`,
			want: "-32002;count=1, 3;count=2",
		},
		{
			name: "batch_leading_whitespace",
			body: " \r\n\t[" + `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"x"}}]`,
			want: "-32601;count=1",
		},
		{
			name: "batch_no_errors",
			body: `[{"jsonrpc":"2.0","id":1,"result":"0x1"}]`,
		},
		{
			name: "malformed",
			body: `[{"jsonrpc":"2.0","id":1,"error":{"code":3}`,
		},
		{
			name: "not_json",
			body: "method not allowed\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, formatErrorCodesHeader(parseErrorCodes([]byte(tt.body))))
		})
	}
}
