// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package libevmlog

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/exp/slog"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"

	ethlog "github.com/ava-labs/libevm/log"
)

func TestRouteForwardsToAvalanchegoLogger(t *testing.T) {
	rec := loggingtest.NewRecorder(logging.Trace)
	Route(rec)
	t.Cleanup(func() {
		ethlog.SetDefault(ethlog.NewLogger(ethlog.DiscardHandler()))
	})

	ethlog.Info("generation resumed", "root", "0xabc")
	ethlog.Warn("head mismatch") // Warn deliberately lands at Info, see avaLevel
	ethlog.Error("generation failed")

	infos := rec.At(logging.Info)
	require.Len(t, infos, 2, "records at Info after ethlog.Info and ethlog.Warn")
	require.Equal(t, "generation resumed", infos[0].Msg, "Info record message")
	require.Len(t, infos[0].Fields, 1, "Info record fields")
	require.Equal(t, "root", infos[0].Fields[0].Key, "Info record field key")
	require.Equal(t, "head mismatch", infos[1].Msg, "demoted Warn record message")

	errs := rec.At(logging.Error)
	require.Len(t, errs, 1, "records at Error after ethlog.Error")
	require.Equal(t, "generation failed", errs[0].Msg, "Error record message")
}

func TestAvaLevel(t *testing.T) {
	tests := []struct {
		in   slog.Level
		want logging.Level
	}{
		{ethlog.LevelTrace, logging.Trace},
		{ethlog.LevelDebug, logging.Debug},
		{ethlog.LevelInfo, logging.Info},
		{ethlog.LevelWarn, logging.Info}, // deliberate demotion, see avaLevel
		{ethlog.LevelError, logging.Error},
		{ethlog.LevelCrit, logging.Fatal},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, avaLevel(tt.in), "avaLevel(%v)", tt.in)
	}
}
