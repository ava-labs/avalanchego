// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmconfig

import (
	"encoding/json"
	"testing"

	"github.com/arr4n/shed/testerr"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/utils/set"
)

// extended extends [Config], as the C-Chain and L1 configs do.
type extended struct {
	Config
	Extra string `json:"extra"`
	nested
}

type nested struct {
	Nested bool `json:"nested"`
}

func TestParse(t *testing.T) {
	// with applies mod to the defaults so each override case asserts that its
	// JSON field overrides the default while the rest are preserved.
	with := func(mod func(*extended)) extended {
		e := extended{Config: Default()}
		mod(&e)
		return e
	}
	nodeID := ids.GenerateTestNodeID()
	unrecognized := func(options ...string) []*loggingtest.Record {
		return []*loggingtest.Record{{
			Level:  logging.Warn,
			Msg:    "ignoring unrecognized config options",
			Fields: []zap.Field{zap.Strings("options", options)},
		}}
	}

	tests := []struct {
		name         string
		json         string
		want         extended
		wantWarnings []*loggingtest.Record
		wantErr      testerr.Want
	}{
		{
			name: "empty_input_keeps_defaults",
			want: with(func(*extended) {}),
		},
		{
			name: "promoted_fields",
			json: `{"pruning-enabled":false,"state-sync-ids":["` + nodeID.String() + `"],"extra":"x","nested":true}`,
			want: with(func(e *extended) {
				e.Pruning = false
				e.StateSyncIDs = set.Of(nodeID)
				e.Extra = "x"
				e.Nested = true
			}),
		},
		{
			name:         "unrecognized_options",
			json:         `{"rpc-gas-cap":50,"hello":1,"extra":"x"}`,
			want:         with(func(e *extended) { e.Extra = "x" }),
			wantWarnings: unrecognized("hello", "rpc-gas-cap"),
		},
		{
			name:    "invalid_json",
			json:    "invalid",
			want:    with(func(*extended) {}),
			wantErr: testerr.As(func(*json.SyntaxError) string { return "" }),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log := loggingtest.NewRecorder(logging.Warn)
			got := extended{Config: Default()}
			err := Parse(log, []byte(test.json), &got)
			if diff := testerr.Diff(err, test.wantErr); diff != "" {
				t.Fatalf("Parse(...) error (-want +got)\n%s", diff)
			}
			require.Equal(t, test.want, got, "Parse(...)")
			require.Equal(t, test.wantWarnings, log.Records, "Parse(...) logs")
		})
	}
}
