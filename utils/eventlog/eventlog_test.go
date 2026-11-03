// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package eventlog

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

// lines decodes each line that a Logger wrote.
func lines(t *testing.T, b *bytes.Buffer) []map[string]any {
	t.Helper()

	var got []map[string]any
	for line := range strings.Lines(b.String()) {
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &m), "line %q", line)
		got = append(got, m)
	}

	return got
}

func TestEvent(t *testing.T) {
	var b bytes.Buffer
	l := NewLogger(&b).With("node", "n1")

	ctx, ev := l.With("chain", "P").Start(t.Context(), "chits")
	require.Same(t, ev, FromContext(ctx))
	ev.Set("from", "n2")
	ev.Append("accepted", 1)
	ev.Append("accepted", 2)
	ev.End(errors.New("boom"))

	got := lines(t, &b)
	require.Len(t, got, 1)
	require.Positive(t, got[0]["timestamp"])
	delete(got[0], "timestamp")
	require.Equal(t, map[string]any{
		"node":     "n1",
		"chain":    "P",
		"op":       "chits",
		"from":     "n2",
		"accepted": []any{1.0, 2.0},
		"err":      "boom",
	}, got[0])
}

func TestEventOrder(t *testing.T) {
	var b bytes.Buffer
	_, ev := NewLogger(&b).With("node", "n1").Start(t.Context(), "notify")
	ev.Set("b", 1)
	ev.Set("a", 2)
	ev.End(nil)

	line := b.String()
	require.True(t, strings.HasPrefix(line, `{"timestamp":`), "line %q", line)
	require.True(t, strings.HasSuffix(line, `,"node":"n1","op":"notify","b":1,"a":2}`+"\n"), "line %q", line)
}

func TestEventSnapshots(t *testing.T) {
	var b bytes.Buffer
	_, ev := NewLogger(&b).Start(t.Context(), "app_gossip")
	ids := []string{"a"}
	ev.Set("ids", ids)
	ids[0] = "b"
	ev.End(nil)

	got := lines(t, &b)
	require.Len(t, got, 1)
	require.Equal(t, []any{"a"}, got[0]["ids"])
}

func TestEventKeyUsedTwice(t *testing.T) {
	tests := []struct {
		name string
		add  func(*Event)
	}{
		{name: "set_twice", add: func(ev *Event) { ev.Set("a", 1); ev.Set("a", 2) }},
		{name: "append_after_set", add: func(ev *Event) { ev.Set("a", 1); ev.Append("a", 2) }},
		{name: "set_after_append", add: func(ev *Event) { ev.Append("a", 1); ev.Set("a", 2) }},
		{name: "timestamp", add: func(ev *Event) { ev.Set("timestamp", 1) }},
		{name: "op", add: func(ev *Event) { ev.Set("op", 1) }},
		{name: "err", add: func(ev *Event) { ev.Append("err", 1) }},
		{name: "logger_key", add: func(ev *Event) { ev.Set("node", 1) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ev := NewLogger(io.Discard).With("node", "n1").Start(t.Context(), "chits")
			require.Panics(t, func() { tt.add(ev) })
		})
	}
}

func TestLoggerKeyUsedTwice(t *testing.T) {
	l := NewLogger(io.Discard).With("node", "n1")
	require.Panics(t, func() { l.With("node", "n2") })
	require.Panics(t, func() { l.With("op", "chits") })
}

func TestEventConcurrent(t *testing.T) {
	const n = 10

	var b bytes.Buffer
	_, ev := NewLogger(&b).Start(t.Context(), "chits")
	var eg errgroup.Group
	for i := range n {
		eg.Go(func() error {
			ev.Append("accepted", i)
			return nil
		})
	}
	require.NoError(t, eg.Wait())
	ev.End(nil)

	got := lines(t, &b)
	require.Len(t, got, 1)
	require.Len(t, got[0]["accepted"], n)
}

func TestEventLate(t *testing.T) {
	var b bytes.Buffer
	_, ev := NewLogger(&b).Start(t.Context(), "app_gossip")
	ev.End(nil)
	ev.Set("from", "n2")
	ev.Append("to", "n3")
	ev.End(nil)

	got := lines(t, &b)
	require.Len(t, got, 1)
	require.NotContains(t, got[0], "from")
	require.NotContains(t, got[0], "to")
}

func TestEventUnencodable(t *testing.T) {
	var b bytes.Buffer
	_, ev := NewLogger(&b).Start(t.Context(), "notify")
	ev.Set("ch", make(chan int))
	ev.End(nil)

	got := lines(t, &b)
	require.Len(t, got, 1)
	require.Contains(t, got[0]["ch"], "!error")
}

func TestEventOff(t *testing.T) {
	var l *Logger
	ctx, ev := l.Start(t.Context(), "notify")
	require.Nil(t, ev)
	require.Nil(t, FromContext(ctx))

	ev.Set("a", 1)
	ev.Append("b", 2)
	ev.End(nil)

	require.Nil(t, l.With("node", "n1"))
}
