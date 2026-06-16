// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sidecar

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/warp/external"
)

var _ external.SidecarClient = (*ExternalInteropValidationRule)(nil)

// Q: Is this the top level rule where I get to customize like "at least 2 out of
// the three RPC clients I provided must succeed" or something like that?

// ExternalInteropValidationRule is the top-level validation policy for an
// external chain event. It implements external.SidecarClient so that it can be
// passed directly to ExternalChainVerifier.
//
// All Required rules must pass. If any required rule fails (conclusively or due
// to an infrastructure error), Verify returns an error immediately without
// evaluating further rules.
//
// Among the Quorum rules, at least MinQuorum must return (true, nil).
// Infrastructure errors in quorum rules count as abstentions, not failures, so
// a quorum can still be reached by the remaining rules. An empty Quorum slice
// with MinQuorum == 0 passes trivially.
type ExternalInteropValidationRule struct {
	Required  []ValidationRule
	Quorum    []ValidationRule
	MinQuorum int
}

func (r *ExternalInteropValidationRule) Verify(ctx context.Context, event *external.ExternalEvent) error {
	for i, rule := range r.Required {
		ok, err := rule.Validate(ctx, event)
		if err != nil {
			// Q: Is this how the rest of this codebase returns errors? Isn't there
			// some custom package like common.AppError or something? Let's be consistent
			return fmt.Errorf("required rule %d: infrastructure error: %w", i, err)
		}
		if !ok {
			return fmt.Errorf("required rule %d: verification failed", i)
		}
	}

	passed := 0
	for _, rule := range r.Quorum {
		ok, err := rule.Validate(ctx, event)
		if err != nil {
			// Infrastructure failure: abstain from quorum, don't count against.
			continue
		}
		if ok {
			passed++
		}
	}

	if passed < r.MinQuorum {
		return fmt.Errorf("quorum not met: %d of %d rules passed, need %d", passed, len(r.Quorum), r.MinQuorum)
	}

	return nil
}
