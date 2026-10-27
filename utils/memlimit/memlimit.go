// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package memlimit

import (
	"errors"
	"fmt"
	"os"

	"github.com/KimMachineGun/automemlimit/memlimit"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
)

// DefaultRatio is the fraction of the available memory used as the Go memory
// limit. The limit only bounds the Go heap; the remainder is headroom for
// memory allocated outside of it, primarily by firewood.
const DefaultRatio = 0.8

var ErrInvalidRatio = errors.New("memory limit ratio must be in the range [0, 1]")

// Set sets the Go runtime memory limit to [ratio] of the cgroup memory limit,
// falling back to total system memory when the process is not in a limited
// cgroup. A ratio of 0 disables the limit. An existing GOMEMLIMIT environment
// variable takes precedence.
func Set(ratio float64, log logging.Logger) error {
	if ratio < 0 || ratio > 1 {
		return fmt.Errorf("%w: %f", ErrInvalidRatio, ratio)
	}
	if ratio == 0 {
		log.Info("memory limit disabled")
		return nil
	}
	if val, ok := os.LookupEnv("GOMEMLIMIT"); ok {
		log.Info("GOMEMLIMIT already set, skipping memory limit configuration",
			zap.String("GOMEMLIMIT", val),
		)
		return nil
	}
	limit, err := memlimit.Set(
		memlimit.WithRatio(ratio),
		memlimit.WithProvider(memlimit.ApplyFallback(memlimit.FromCgroup, memlimit.FromSystem)),
	)
	if err != nil {
		return err
	}
	log.Info("set memory limit",
		zap.Float64("ratio", ratio),
		zap.Int64("limitBytes", limit),
	)
	return nil
}
