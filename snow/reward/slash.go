// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package reward

import (
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	metricLabel   string = "metric"
	subnetIDLabel string = "subnetID"
)

var (
	slashPrefix = []byte("slash")

	_ Slasher = (*SlashDB)(nil)
)

type Slasher interface {
	// Slash slashes the staking rewards for [nodeID] on [subnetID].
	// This only affects the staking period corresponding to the [txID] of the
	// transaction that added the validator to the staking set on the P-Chain.
	Slash(
		nodeID ids.NodeID,
		txID ids.ID,
		subnetID ids.ID,
		metricName string,
	) error
	// Slashed returns whether or not [nodeID] is slashed for [subnetID] for the
	// staking cycle corresponding to [txID] on the P-Chain.
	Slashed(nodeID ids.NodeID, txID ids.ID, subnetID ids.ID) (bool, error)
	// Reset resets the slashed status of [nodeID] on [subnetID] for the staking
	// cycle corresponding to [txID] on the P-Chain.
	Reset(nodeID ids.NodeID, txID ids.ID, subnetID ids.ID) error
}

// SlashDB is an implementation of Slasher that tracks slashed nodes in a
// database.
type SlashDB struct {
	db      database.Database
	log     logging.Logger
	metrics *prometheus.SummaryVec
}

// NewSlashDB returns a new instance of SlashDB
func NewSlashDB(
	db database.Database,
	log logging.Logger,
	metrics prometheus.Registerer,
) (*SlashDB, error) {
	m := prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace: "slasher",
			Name:      "slash",
			Help:      "Times this node slashed uptimes",
		},
		[]string{metricLabel, subnetIDLabel},
	)

	if err := metrics.Register(m); err != nil {
		return &SlashDB{}, err
	}

	return &SlashDB{
		db:      prefixdb.New(slashPrefix, db),
		log:     log,
		metrics: m,
	}, nil
}

func (s *SlashDB) Slash(
	nodeID ids.NodeID,
	txID ids.ID,
	subnetID ids.ID,
	metric string,
) error {
	s.log.Debug("slashing node uptime",
		zap.Stringer("nodeID", nodeID),
		zap.Stringer("txID", txID),
		zap.Stringer("subnetID", subnetID),
		zap.String("metric", metric),
	)

	m, err := s.metrics.GetMetricWith(map[string]string{
		subnetIDLabel: subnetID.String(),
		metricLabel:   metric,
	})

	if err != nil {
		return err
	}
	m.Observe(1)

	subnetDB := prefixdb.New(subnetID[:], s.db)
	key := append(nodeID[:], txID[:]...)

	return subnetDB.Put(key, nil)
}

func (s *SlashDB) Slashed(
	nodeID ids.NodeID,
	txID ids.ID,
	subnetID ids.ID,
) (bool, error) {
	subnetDB := prefixdb.New(subnetID[:], s.db)
	key := append(nodeID[:], txID[:]...)

	return subnetDB.Has(key)
}

func (s *SlashDB) Reset(nodeID ids.NodeID, txID ids.ID, subnetID ids.ID) error {
	subnetDB := prefixdb.New(subnetID[:], s.db)
	key := append(nodeID[:], txID[:]...)

	return subnetDB.Delete(key)
}
