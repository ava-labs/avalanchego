// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blocktest"
)

func TestChangeNotifier(t *testing.T) {
	testVM := blocktest.VM{
		BuildBlockF: func(context.Context) (snowman.Block, error) {
			return &snowmantest.Block{}, nil
		},
	}

	for _, testCase := range []struct {
		name string
		f    func(*ChangeNotifier)
	}{
		{
			name: "SetPreference",
			f: func(n *ChangeNotifier) {
				require.NoError(t, n.SetPreference(context.Background(), ids.Empty))
			},
		},
		{
			name: "SetState",
			f: func(n *ChangeNotifier) {
				require.NoError(t, n.SetState(context.Background(), snow.NormalOp))
			},
		},
		{
			name: "BuildBlock",
			f: func(n *ChangeNotifier) {
				_, err := n.BuildBlock(context.Background())
				require.NoError(t, err)
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var invoked bool
			nf := ChangeNotifier{
				OnChange: func() {
					invoked = true
				},
				ChainVM: &testVM,
			}
			testCase.f(&nf)
			require.True(t, invoked, "expected to have been invoked")
		})
	}
}
