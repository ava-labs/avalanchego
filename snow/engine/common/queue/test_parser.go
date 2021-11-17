// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package queue

import (
	"errors"
	"testing"
)

var errParse = errors.New("unexpectedly called Parse")

// TestParser is a test Parser
type TestParser struct {
	T *testing.T

	CantParse bool

	ParseF func([]byte) (Job, error)
}

func (p *TestParser) Default(cant bool) { p.CantParse = cant }

func (p *TestParser) Parse(b []byte) (Job, error) {
	if p.ParseF != nil {
		return p.ParseF(b)
	}
	if p.CantParse && p.T != nil {
		p.T.Fatal(errParse)
	}
	return nil, errParse
}
