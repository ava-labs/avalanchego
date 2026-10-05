// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fork

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
)

// unhealthyAfterSwitch is how long after T+Δ the node tolerates not knowing
// H_fork before reporting unhealthy.
const unhealthyAfterSwitch = 10 * time.Minute

var (
	ErrLateJoiner        = errors.New("fork time has passed and this node was not armed before it; late joiners are not supported")
	ErrConfigChanged     = errors.New("fork config differs from the config this node was armed with")
	ErrInvalidForkPoint  = errors.New("invalid fork point encoding")
	ErrForkHeightUnknown = errors.New("the P-chain has not accepted a block at or after the fork time; issue a P-chain transaction")

	markerKey = []byte("fork marker")
)

// Phase is the node's position in the fork lifecycle.
type Phase uint8

const (
	// Observing: before T. The node behaves like a normal node.
	Observing Phase = iota
	// Grace: between T and T+Δ. The proposer rule applies; polling and
	// peering are unchanged.
	Grace
	// Switched: after T+Δ, or the node restarted after T. Only fork
	// validators are polled and peered with.
	Switched
)

func (p Phase) String() string {
	switch p {
	case Observing:
		return "observing"
	case Grace:
		return "grace"
	case Switched:
		return "switched"
	default:
		return fmt.Sprintf("unknown(%d)", uint8(p))
	}
}

// Arm records that this node takes part in the fork described by [c], and
// returns whether the node must start in the Switched phase.
//
// Before T, Arm (re)writes the marker with [c]'s hash, overwriting any marker
// left by an earlier config, so an operator may edit the config until T. At or
// after T, the node must already be armed with the same config: a missing
// marker is ErrLateJoiner and a different hash is ErrConfigChanged.
func Arm(db database.KeyValueReaderWriter, c *Config, now time.Time) (bool, error) {
	hash := c.Hash()
	if !c.IsForked(now) {
		if err := db.Put(markerKey, hash[:]); err != nil {
			return false, fmt.Errorf("writing fork marker: %w", err)
		}
		return false, nil
	}

	stored, err := db.Get(markerKey)
	switch {
	case errors.Is(err, database.ErrNotFound):
		return false, ErrLateJoiner
	case err != nil:
		return false, fmt.Errorf("reading fork marker: %w", err)
	case !bytes.Equal(stored, hash[:]):
		return false, ErrConfigChanged
	default:
		return true, nil
	}
}

// ForkPoint is a chain's last block timestamped before the fork time.
type ForkPoint struct {
	BlockID ids.ID `json:"blockID"`
	Height  uint64 `json:"height"`
}

// Bytes encodes the fork point as blockID || bigEndian(height).
func (p ForkPoint) Bytes() []byte {
	b := make([]byte, ids.IDLen+8)
	copy(b, p.BlockID[:])
	binary.BigEndian.PutUint64(b[ids.IDLen:], p.Height)
	return b
}

// ParseForkPoint decodes the output of ForkPoint.Bytes.
func ParseForkPoint(b []byte) (ForkPoint, error) {
	if len(b) != ids.IDLen+8 {
		return ForkPoint{}, fmt.Errorf("%w: length %d", ErrInvalidForkPoint, len(b))
	}
	var p ForkPoint
	copy(p.BlockID[:], b[:ids.IDLen])
	p.Height = binary.BigEndian.Uint64(b[ids.IDLen:])
	return p, nil
}

// Report is the fork health check's details.
type Report struct {
	Phase      string               `json:"phase"`
	ForkHeight *uint64              `json:"forkHeight,omitempty"`
	ForkPoints map[string]ForkPoint `json:"forkPoints"`
}

// Status tracks the node's fork progress. It is safe for concurrent use.
type Status struct {
	config *Config
	now    func() time.Time

	switched      atomic.Bool
	hasForkHeight atomic.Bool
	forkHeight    atomic.Uint64

	lock       sync.Mutex
	forkPoints map[ids.ID]ForkPoint
}

func NewStatus(c *Config, now func() time.Time) *Status {
	return &Status{
		config:     c,
		now:        now,
		forkPoints: make(map[ids.ID]ForkPoint),
	}
}

func (s *Status) MarkSwitched() {
	s.switched.Store(true)
}

func (s *Status) Phase() Phase {
	switch {
	case s.switched.Load():
		return Switched
	case !s.config.IsForked(s.now()):
		return Observing
	default:
		return Grace
	}
}

// SetForkHeight records H_fork, the height of the first accepted P-chain
// block timestamped at or after the fork time.
func (s *Status) SetForkHeight(height uint64) {
	s.forkHeight.Store(height)
	s.hasForkHeight.Store(true)
}

func (s *Status) ForkHeight() (uint64, bool) {
	if !s.hasForkHeight.Load() {
		return 0, false
	}
	return s.forkHeight.Load(), true
}

func (s *Status) SetForkPoint(chainID ids.ID, p ForkPoint) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.forkPoints[chainID] = p
}

func (s *Status) Report() Report {
	s.lock.Lock()
	defer s.lock.Unlock()

	r := Report{
		Phase:      s.Phase().String(),
		ForkPoints: make(map[string]ForkPoint, len(s.forkPoints)),
	}
	if height, ok := s.ForkHeight(); ok {
		r.ForkHeight = &height
	}
	for chainID, p := range s.forkPoints {
		r.ForkPoints[chainID.String()] = p
	}
	return r
}

// HealthCheck reports the fork progress. It is unhealthy if the node switched
// long ago but H_fork is still unknown, which leaves Warp on the source
// network's validator set.
func (s *Status) HealthCheck(context.Context) (interface{}, error) {
	r := s.Report()
	if r.ForkHeight == nil &&
		s.Phase() == Switched &&
		s.now().After(s.config.SwitchTime().Add(unhealthyAfterSwitch)) {
		return r, ErrForkHeightUnknown
	}
	return r, nil
}

// RegisterMetrics registers a "phase" gauge: 0 observing, 1 grace, 2 switched.
func (s *Status) RegisterMetrics(reg prometheus.Registerer) error {
	return reg.Register(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "phase",
			Help: "fork phase: 0 observing, 1 grace, 2 switched",
		},
		func() float64 { return float64(s.Phase()) },
	))
}
