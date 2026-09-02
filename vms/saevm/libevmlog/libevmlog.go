// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package libevmlog routes libevm's process-global logger to an avalanchego
// logger.
package libevmlog

import (
	"context"
	"slices"

	"go.uber.org/zap"
	"golang.org/x/exp/slog"

	"github.com/ava-labs/avalanchego/utils/logging"

	ethlog "github.com/ava-labs/libevm/log"
)

// Route forwards everything written to libevm's process-global root logger to
// log. libevm's default handler discards all records, which hides snapshot
// lifecycle events (load failures, rebuilds, generation progress) and every
// other libevm-internal log.
//
// The libevm root logger is process-global, so the last Route call wins across
// all chains in the process.
func Route(log logging.Logger) {
	ethlog.SetDefault(ethlog.NewLogger(&handler{log: log}))
}

var _ slog.Handler = (*handler)(nil)

// handler forwards [slog.Record]s to an avalanchego logger, mapping libevm's
// levels onto theirs. Crit maps to Fatal in level only: libevm's log.Crit
// exits the process itself after the record is handled.
type handler struct {
	log   logging.Logger
	attrs []zap.Field
}

func (h *handler) Enabled(_ context.Context, lvl slog.Level) bool {
	return h.log.Enabled(avaLevel(lvl))
}

func (h *handler) Handle(_ context.Context, r slog.Record) error {
	fields := slices.Clone(h.attrs)
	r.Attrs(func(a slog.Attr) bool {
		fields = append(fields, zap.Any(a.Key, a.Value.Any()))
		return true
	})

	switch avaLevel(r.Level) {
	case logging.Trace:
		h.log.Trace(r.Message, fields...)
	case logging.Debug:
		h.log.Debug(r.Message, fields...)
	case logging.Info:
		h.log.Info(r.Message, fields...)
	case logging.Warn:
		h.log.Warn(r.Message, fields...)
	case logging.Error:
		h.log.Error(r.Message, fields...)
	default:
		h.log.Fatal(r.Message, fields...)
	}
	return nil
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	fields := slices.Clone(h.attrs)
	for _, a := range attrs {
		fields = append(fields, zap.Any(a.Key, a.Value.Any()))
	}
	return &handler{log: h.log, attrs: fields}
}

// WithGroup returns the handler unchanged: libevm never logs through groups,
// so qualifying keys would only complicate the output.
func (h *handler) WithGroup(string) slog.Handler { return h }

// avaLevel maps libevm levels onto avalanchego's. Warn deliberately maps to
// Info: libevm warns on routine cold-start paths (e.g. rebuilding the snapshot
// on a fresh datadir), while this repo reserves Warn and above for actionable
// conditions — a convention [loggingtest] enforces by failing tests on them.
func avaLevel(lvl slog.Level) logging.Level {
	switch {
	case lvl <= ethlog.LevelTrace:
		return logging.Trace
	case lvl <= ethlog.LevelDebug:
		return logging.Debug
	case lvl <= ethlog.LevelWarn:
		return logging.Info
	case lvl <= ethlog.LevelError:
		return logging.Error
	default: // ethlog.LevelCrit
		return logging.Fatal
	}
}
