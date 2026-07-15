// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpctest

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/arr4n/shed/testerr"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/rpc"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/saevm/cmputils"
)

// Case specifies one RPC call and its expected outcome: a reply equal to Want,
// or an error matching WantErr.
type Case struct {
	Method       string
	Args         []any
	Want         any // untyped nil means no return value
	WantErr      testerr.Want
	Parallel     bool
	Eventually   bool
	ExtraCmpOpts []cmp.Option
}

// Run executes each Case against client and fails t on any mismatch. It is the
// RPC assertion harness shared by the SAE and C-Chain suites.
func Run(ctx context.Context, t *testing.T, client *rpc.Client, cases ...Case) {
	t.Helper()
	opts := []cmp.Option{
		cmputils.NilSlicesAreEmpty[hexutil.Bytes](),
		cmputils.IfIn[params.ChainConfig](cmp.Options{
			cmputils.BigInts(),
			cmpopts.IgnoreUnexported(params.ChainConfig{}),
		}),
		cmputils.Headers(),
		cmputils.HexutilBigs(),
		cmputils.TransactionsByHash(),
		cmputils.Receipts(),
	}

	for _, tc := range cases {
		test := func(t require.TestingT) {
			// A nil Want means no return value. Box it so it is not confused
			// with an empty reply.
			if tc.Want == nil {
				tc.Want = struct{ json.RawMessage }{}
			}

			got := reflect.New(reflect.TypeOf(tc.Want))
			err := client.CallContext(ctx, got.Interface(), tc.Method, tc.Args...)
			if diff := testerr.Diff(err, tc.WantErr); diff != "" {
				t.Errorf("CallContext(...) %s", diff)
				t.FailNow()
			}
			cmpOpts := append(opts, tc.ExtraCmpOpts...)
			if diff := cmp.Diff(tc.Want, got.Elem().Interface(), cmpOpts...); diff != "" {
				t.Errorf("Unmarshalled %T diff (-want +got):\n%s", got.Elem().Interface(), diff)
			}
		}

		t.Run(tc.Method, func(t *testing.T) {
			if tc.Parallel {
				t.Parallel()
			}
			t.Logf("CallContext(ctx, %T, %q, %v...)", &tc.Want, tc.Method, tc.Args)
			if tc.Eventually {
				require.EventuallyWithT(t, func(c *assert.CollectT) {
					test(c)
				}, time.Second, 10*time.Millisecond)
			} else {
				test(t)
			}
		})
	}
}
