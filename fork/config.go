// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package fork implements fork mode: a node follows its source network until
// blocks are timestamped at or after a scheduled fork time, after which only
// a configured validator set may propose, vote, and peer. See FORK.md.
package fork

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

// DefaultGracePeriod is used when the config does not set a grace period.
const DefaultGracePeriod = 61 * time.Second

var (
	ErrMissingForkTime     = errors.New("fork time must be set")
	ErrSubSecondForkTime   = errors.New("fork time must be a whole second")
	ErrNegativeGracePeriod = errors.New("grace period must not be negative")
	ErrNoValidators        = errors.New("fork config must list at least one validator")
	ErrDuplicateNodeID     = errors.New("duplicate validator node ID")
	ErrZeroWeight          = errors.New("validator weight must be non-zero")
	ErrMissingSigner       = errors.New("validator is missing a BLS signer")
	ErrMissingIP           = errors.New("validator is missing an IP")
)

// Validator is a member of the fork's validator set.
type Validator struct {
	NodeID ids.NodeID                `json:"nodeID"`
	Weight uint64                    `json:"weight"`
	Signer *signer.ProofOfPossession `json:"signer"`
	// IP is used to connect to the validator before the switch and as a
	// beacon after it.
	IP netip.AddrPort `json:"ip"`
}

// Config is a validated fork configuration.
type Config struct {
	// Time is the fork time T. Blocks timestamped at or after Time must be
	// proposed by Validators.
	Time time.Time
	// GracePeriod is Δ: polling and peering switch to Validators at Time+Δ.
	GracePeriod time.Duration
	// Validators is sorted by node ID.
	Validators []Validator

	validatorSet map[ids.NodeID]*validators.GetValidatorOutput
	warpSet      validators.WarpSet
	hash         [sha256.Size]byte
}

type jsonConfig struct {
	ForkTime time.Time `json:"forkTime"`
	// GracePeriod is a duration string; empty means DefaultGracePeriod.
	GracePeriod string      `json:"gracePeriod,omitempty"`
	Validators  []Validator `json:"validators"`
}

// New validates the provided values and returns a Config.
func New(forkTime time.Time, gracePeriod time.Duration, vdrs []Validator) (*Config, error) {
	c := &Config{
		Time:        forkTime.UTC(),
		GracePeriod: gracePeriod,
		Validators:  slices.Clone(vdrs),
	}
	slices.SortFunc(c.Validators, func(a, b Validator) int {
		return a.NodeID.Compare(b.NodeID)
	})
	if err := c.verify(); err != nil {
		return nil, err
	}

	c.validatorSet = make(map[ids.NodeID]*validators.GetValidatorOutput, len(c.Validators))
	for _, v := range c.Validators {
		c.validatorSet[v.NodeID] = &validators.GetValidatorOutput{
			NodeID:    v.NodeID,
			PublicKey: v.Signer.Key(),
			Weight:    v.Weight,
		}
	}

	var err error
	c.warpSet, err = validators.FlattenValidatorSet(c.validatorSet)
	if err != nil {
		return nil, fmt.Errorf("building fork warp set: %w", err)
	}
	canonical, err := c.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("encoding fork config: %w", err)
	}
	c.hash = sha256.Sum256(canonical)
	return c, nil
}

// Parse decodes and validates a JSON fork config.
func Parse(b []byte) (*Config, error) {
	var raw jsonConfig
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("unmarshalling fork config: %w", err)
	}

	gracePeriod := DefaultGracePeriod
	if raw.GracePeriod != "" {
		d, err := time.ParseDuration(raw.GracePeriod)
		if err != nil {
			return nil, fmt.Errorf("parsing grace period: %w", err)
		}
		gracePeriod = d
	}
	return New(raw.ForkTime, gracePeriod, raw.Validators)
}

// MarshalJSON returns the canonical encoding of the config. It is also the
// input to Hash.
func (c *Config) MarshalJSON() ([]byte, error) {
	return json.Marshal(jsonConfig{
		ForkTime:    c.Time.UTC(),
		GracePeriod: c.GracePeriod.String(),
		Validators:  c.Validators,
	})
}

func (c *Config) verify() error {
	if c.Time.IsZero() {
		return ErrMissingForkTime
	}
	// Block timestamps are whole seconds, so a sub-second fork time would
	// make block builders wait for a time no block can carry.
	if c.Time.Nanosecond() != 0 {
		return ErrSubSecondForkTime
	}
	if c.GracePeriod < 0 {
		return ErrNegativeGracePeriod
	}
	if len(c.Validators) == 0 {
		return ErrNoValidators
	}

	var totalWeight uint64
	for i, v := range c.Validators {
		if i > 0 && c.Validators[i-1].NodeID == v.NodeID {
			return fmt.Errorf("%w: %s", ErrDuplicateNodeID, v.NodeID)
		}
		if v.Weight == 0 {
			return fmt.Errorf("%w: %s", ErrZeroWeight, v.NodeID)
		}
		if v.Signer == nil {
			return fmt.Errorf("%w: %s", ErrMissingSigner, v.NodeID)
		}
		if err := v.Signer.Verify(); err != nil {
			return fmt.Errorf("verifying proof of possession of %s: %w", v.NodeID, err)
		}
		if !v.IP.IsValid() || v.IP.Port() == 0 {
			return fmt.Errorf("%w: %s", ErrMissingIP, v.NodeID)
		}
		var err error
		totalWeight, err = safemath.Add(totalWeight, v.Weight)
		if err != nil {
			return fmt.Errorf("summing validator weights: %w", err)
		}
	}
	return nil
}

// Hash identifies the config. It is stable across encodings of the same
// values.
func (c *Config) Hash() [sha256.Size]byte {
	return c.hash
}

// SwitchTime is T+Δ, when polling and peering switch to the fork validators.
func (c *Config) SwitchTime() time.Time {
	return c.Time.Add(c.GracePeriod)
}

// IsForked reports whether a block timestamped [ts] is subject to the fork
// proposer rule.
func (c *Config) IsForked(ts time.Time) bool {
	return !ts.Before(c.Time)
}

// NodeIDs returns the fork validators' node IDs.
func (c *Config) NodeIDs() set.Set[ids.NodeID] {
	s := set.NewSet[ids.NodeID](len(c.Validators))
	for _, v := range c.Validators {
		s.Add(v.NodeID)
	}
	return s
}

// ValidatorSet returns the fork validator set. The returned map is shared and
// must not be modified.
func (c *Config) ValidatorSet() map[ids.NodeID]*validators.GetValidatorOutput {
	return c.validatorSet
}

// WarpSet returns the fork validators' canonical warp set.
func (c *Config) WarpSet() validators.WarpSet {
	return c.warpSet
}
