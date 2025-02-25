// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

var (
	_ metrics = (*noopMetrics)(nil)
)

type noopMetrics struct {
}

func (m *noopMetrics) HashCalculated() {
	return
}

func (m *noopMetrics) DatabaseNodeRead() {
	return
}

func (m *noopMetrics) DatabaseNodeWrite() {
	return
}

func (m *noopMetrics) ValueNodeCacheHit() {
	return
}

func (m *noopMetrics) ValueNodeCacheMiss() {
	return
}

func (m *noopMetrics) IntermediateNodeCacheHit() {
	return
}

func (m *noopMetrics) IntermediateNodeCacheMiss() {
	return
}

func (m *noopMetrics) ViewChangesValueHit() {
	return
}

func (m *noopMetrics) ViewChangesValueMiss() {
	return
}

func (m *noopMetrics) ViewChangesNodeHit() {
	return
}

func (m *noopMetrics) ViewChangesNodeMiss() {
	return
}
