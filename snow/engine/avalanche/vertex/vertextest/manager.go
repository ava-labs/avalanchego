// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertextest

import (
	"testing"

	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
)

var _ vertex.Manager = (*TestManager)(nil)

type TestManager struct {
	TestBuilder
	TestParser
	TestStorage
}

func NewTestManager(t *testing.T) *TestManager {
	return &TestManager{
		TestBuilder: TestBuilder{T: t},
		TestParser:  TestParser{T: t},
		TestStorage: TestStorage{T: t},
	}
}

func (m *TestManager) Default(cant bool) {
	m.TestBuilder.Default(cant)
	m.TestParser.Default(cant)
	m.TestStorage.Default(cant)
}
