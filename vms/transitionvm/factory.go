// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms"
)

var _ vms.Factory = (*Factory)(nil)

// Factory builds a transition [VM] from a pre- and a post-transition factory.
type Factory struct {
	PreFactory     vms.Factory
	PostFactory    vms.Factory
	TransitionTime time.Time
	// APIDrainTimeout bounds how long the transition waits for in-flight API
	// requests to the pre-transition chain to return before shutting it down.
	APIDrainTimeout time.Duration
}

var errInvalidVMType = errors.New("invalid VM type")

func (f *Factory) New(log logging.Logger) (interface{}, error) {
	preIntf, err := f.PreFactory.New(log)
	if err != nil {
		return nil, err
	}
	pre, ok := preIntf.(Chain)
	if !ok {
		return nil, fmt.Errorf("%w: pre-transition chain: %T", errInvalidVMType, preIntf)
	}

	postIntf, err := f.PostFactory.New(log)
	if err != nil {
		return nil, err
	}
	post, ok := postIntf.(Chain)
	if !ok {
		return nil, fmt.Errorf("%w: post-transition chain: %T", errInvalidVMType, postIntf)
	}

	return &VM{
		preTransitionChain:  pre,
		postTransitionChain: post,
		transitionTime:      f.TransitionTime,
		apiDrainTimeout:     f.APIDrainTimeout,

		// [VM.Version] and [VM.Shutdown] may be called before [VM.Initialize],
		// so mark the pre-transition chain current up front. The placeholder
		// context supplies a lock for the methods that acquire one; [initChain]
		// replaces it with the real per-chain context during [VM.Initialize].
		current: &current{
			chain:    pre,
			chainCtx: &snow.Context{},
		},
	}, nil
}
