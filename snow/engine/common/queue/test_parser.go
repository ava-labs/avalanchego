// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package queue

import (
	"context"
	"errors"
	"testing"
)

var errParse = errors.New("unexpectedly called Parse")

// TestParser is a test Parser
type TestParser struct {
	T *testing.T

	CantParse bool

	ParseF func(context.Context, []byte) (Job, error)
}

func (p *TestParser) Default(cant bool) {
	p.CantParse = cant
}

func (p *TestParser) Parse(ctx context.Context, b []byte) (Job, error) {
	if p.ParseF != nil {
		return p.ParseF(ctx, b)
	}
	if p.CantParse && p.T != nil {
		p.T.Fatal(errParse)
	}
	return nil, errParse
}
