// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/vms/platformvm/block"
)

const blkLabel = "blk"

var (
	_ block.Visitor = (*blockMetrics)(nil)

	blkLabels = []string{blkLabel}
)

type blockMetrics struct {
	txMetrics *txMetrics
	numBlocks *prometheus.CounterVec
}

func newBlockMetrics(registerer prometheus.Registerer) (*blockMetrics, error) {
	txMetrics, err := newTxMetrics(registerer)
	if err != nil {
		return nil, err
	}

	m := &blockMetrics{
		txMetrics: txMetrics,
		numBlocks: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "blks_accepted",
				Help: "number of blocks accepted",
			},
			blkLabels,
		),
	}
	return m, registerer.Register(m.numBlocks)
}

func (m *blockMetrics) BanffAbortBlock(*block.BanffAbortBlock) error {
	m.numBlocks.With(prometheus.Labels{
		blkLabel: "abort",
	}).Inc()
	return nil
}

func (m *blockMetrics) BanffCommitBlock(*block.BanffCommitBlock) error {
	m.numBlocks.With(prometheus.Labels{
		blkLabel: "commit",
	}).Inc()
	return nil
}

func (m *blockMetrics) BanffProposalBlock(b *block.BanffProposalBlock) error {
	m.numBlocks.With(prometheus.Labels{
		blkLabel: "proposal",
	}).Inc()
	for _, tx := range b.Transactions {
		if err := tx.Unsigned.Visit(m.txMetrics); err != nil {
			return err
		}
	}
	return b.Tx.Unsigned.Visit(m.txMetrics)
}

func (m *blockMetrics) BanffStandardBlock(b *block.BanffStandardBlock) error {
	m.numBlocks.With(prometheus.Labels{
		blkLabel: "standard",
	}).Inc()
	for _, tx := range b.Transactions {
		if err := tx.Unsigned.Visit(m.txMetrics); err != nil {
			return err
		}
	}
	return nil
}

func (m *blockMetrics) ApricotAbortBlock(*block.ApricotAbortBlock) error {
	m.numBlocks.With(prometheus.Labels{
		blkLabel: "abort",
	}).Inc()
	return nil
}

func (m *blockMetrics) ApricotCommitBlock(*block.ApricotCommitBlock) error {
	m.numBlocks.With(prometheus.Labels{
		blkLabel: "commit",
	}).Inc()
	return nil
}

func (m *blockMetrics) ApricotProposalBlock(b *block.ApricotProposalBlock) error {
	m.numBlocks.With(prometheus.Labels{
		blkLabel: "proposal",
	}).Inc()
	return b.Tx.Unsigned.Visit(m.txMetrics)
}

func (m *blockMetrics) ApricotStandardBlock(b *block.ApricotStandardBlock) error {
	m.numBlocks.With(prometheus.Labels{
		blkLabel: "standard",
	}).Inc()
	for _, tx := range b.Transactions {
		if err := tx.Unsigned.Visit(m.txMetrics); err != nil {
			return err
		}
	}
	return nil
}

func (m *blockMetrics) ApricotAtomicBlock(b *block.ApricotAtomicBlock) error {
	m.numBlocks.With(prometheus.Labels{
		blkLabel: "atomic",
	}).Inc()
	return b.Tx.Unsigned.Visit(m.txMetrics)
}
