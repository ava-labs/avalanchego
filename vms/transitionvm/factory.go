// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms"
)

var _ vms.Factory = (*Factory)(nil)

type Factory struct {
	PreFactory     vms.Factory
	PostFactory    vms.Factory
	TransitionTime time.Time
}

var errInvalidVMType = errors.New("invalid VM type")

func (f *Factory) New(log logging.Logger) (interface{}, error) {
	preIntf, err := f.PreFactory.New(log)
	if err != nil {
		return nil, err
	}
	pre, ok := preIntf.(chain)
	if !ok {
		return nil, fmt.Errorf("%w: %T", errInvalidVMType, preIntf)
	}

	postIntf, err := f.PostFactory.New(log)
	if err != nil {
		return nil, err
	}
	post, ok := postIntf.(chain)
	if !ok {
		return nil, fmt.Errorf("%w: %T", errInvalidVMType, postIntf)
	}

	return &VM{
		preTransitionChain:  pre,
		postTransitionChain: post,
		transitionTime:      f.TransitionTime,
	}, nil
}
