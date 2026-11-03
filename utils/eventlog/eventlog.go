// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package eventlog writes one JSON line for each input a node handles, with
// what the input caused as fields on that line.
//
// Every line starts with "timestamp", the Unix time in nanoseconds when the
// input arrived, then the Logger's fields, then "op".
package eventlog

import (
	"context"
	"encoding/json"
	"io"
	"slices"
	"strconv"
	"sync"
	"time"
)

// A Logger writes events as JSON lines. A nil *Logger writes nothing.
type Logger struct {
	out *output
	// fields holds the encoded `,"key":value` pairs that With added, and keys
	// their keys.
	fields []byte
	keys   []string
}

type output struct {
	mu sync.Mutex
	w  io.Writer
}

// write drops errors. An event log is for debugging, and a node must not stop
// because its event log cannot be written.
func (o *output) write(line []byte) {
	o.mu.Lock()
	defer o.mu.Unlock()

	_, _ = o.w.Write(line)
}

// NewLogger returns a Logger that writes to w. Lines are written whole, one
// at a time.
func NewLogger(w io.Writer) *Logger {
	return &Logger{out: &output{w: w}}
}

// With returns a Logger that adds key and v to every line. It panics if key
// is "timestamp", "op", "err", or a key that l already adds.
func (l *Logger) With(key string, v any) *Logger {
	if l == nil {
		return nil
	}

	checkKey(l.keys, key)

	return &Logger{
		out:    l.out,
		fields: appendField(slices.Clone(l.fields), key, encode(v)),
		keys:   append(slices.Clone(l.keys), key),
	}
}

// checkKey panics if key would repeat a key on the line: one that every line
// has, or one in keys. A repeated key is a bug in the caller, and JSON
// decoders keep only one of the values without saying so.
func checkKey(keys []string, key string) {
	switch {
	case key == "timestamp" || key == "op" || key == "err":
		panic("eventlog: key " + strconv.Quote(key) + " is reserved")
	case slices.Contains(keys, key):
		panic("eventlog: key " + strconv.Quote(key) + " is already set")
	}
}

// An Event collects fields while a node handles one input. A nil *Event does
// nothing, so callers need no check when events are off.
type Event struct {
	logger *Logger
	start  time.Time
	op     string

	mu     sync.Mutex
	fields []field
	index  map[string]int
	ended  bool
}

type field struct {
	key    string
	values []json.RawMessage
	list   bool
}

type contextKey struct{}

// Start returns ctx carrying a new event for op. If l is nil, Start returns
// ctx and a nil *Event.
func (l *Logger) Start(ctx context.Context, op string) (context.Context, *Event) {
	if l == nil {
		return ctx, nil
	}

	e := &Event{
		logger: l,
		start:  time.Now(),
		op:     op,
		index:  make(map[string]int),
	}

	return context.WithValue(ctx, contextKey{}, e), e
}

// FromContext returns the event in ctx, or nil.
func FromContext(ctx context.Context) *Event {
	e, _ := ctx.Value(contextKey{}).(*Event)
	return e
}

// Set sets key to v. v is encoded now, so the caller may change it after Set
// returns. It panics if key is already set, by Set, [Event.Append], or
// [Logger.With], or is "timestamp", "op", or "err".
func (e *Event) Set(key string, v any) {
	e.add(key, v, false)
}

// Append adds v to the list at key. v is encoded now, as in [Event.Set]. It
// panics if [Event.Set] or [Logger.With] set key, or key is reserved, as in
// [Event.Set].
func (e *Event) Append(key string, v any) {
	e.add(key, v, true)
}

func (e *Event) add(key string, v any, list bool) {
	if e == nil {
		return
	}

	raw := encode(v)
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.ended {
		return
	}

	i, ok := e.index[key]
	switch {
	case ok && list && e.fields[i].list:
		e.fields[i].values = append(e.fields[i].values, raw)
		return
	case ok:
		panic("eventlog: key " + strconv.Quote(key) + " is already set")
	}

	checkKey(e.logger.keys, key)
	e.index[key] = len(e.fields)
	e.fields = append(e.fields, field{key: key, values: []json.RawMessage{raw}, list: list})
}

// End writes the event, with err if it is not nil. Fields added after End are
// dropped.
func (e *Event) End(err error) {
	if e == nil {
		return
	}

	line, ok := e.end()
	if !ok {
		return
	}

	if err != nil {
		line = appendField(line, "err", encode(err.Error()))
	}
	e.logger.out.write(append(line, '}', '\n'))
}

// end marks e ended and returns its line so far. It reports false if e had
// already ended. End writes the line after end returns, so a slow writer
// does not block callers waiting on e.mu.
func (e *Event) end() ([]byte, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.ended {
		return nil, false
	}
	e.ended = true

	line := e.head()
	for _, f := range e.fields {
		line = appendField(line, f.key, f.encode())
	}

	return line, true
}

// head returns the start of a line: the timestamp, the Logger's fields, and
// the op.
func (e *Event) head() []byte {
	line := append([]byte(`{"timestamp":`), strconv.FormatInt(e.start.UnixNano(), 10)...)
	line = append(line, e.logger.fields...)

	return appendField(line, "op", encode(e.op))
}

func (f *field) encode() json.RawMessage {
	if !f.list {
		return f.values[0]
	}

	b := []byte{'['}
	for i, v := range f.values {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, v...)
	}

	return append(b, ']')
}

func appendField(b []byte, key string, v json.RawMessage) []byte {
	b = append(b, ',')
	b = append(b, encode(key)...)
	b = append(b, ':')

	return append(b, v...)
}

// encode returns v as JSON. A value that cannot be encoded becomes a string
// that says why, so the line stays valid and the failure is visible.
func encode(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		b, _ = json.Marshal("!error: " + err.Error()) // a string always encodes
	}

	return b
}
