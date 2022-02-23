// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

const defaultPollFrequency = 100 * time.Millisecond

type Option func(*Options)

type Options struct {
	ctx context.Context

	minIssuanceTimeSet bool
	minIssuanceTime    uint64

	changeOwner *secp256k1fx.OutputOwners

	memo []byte

	assumeDecided bool

	pollFrequencySet bool
	pollFrequency    time.Duration
}

func NewOptions(ops []Option) *Options {
	o := &Options{}
	o.applyOptions(ops)
	return o
}

func (o *Options) applyOptions(ops []Option) {
	for _, op := range ops {
		op(o)
	}
}

func (o *Options) Context() context.Context {
	if o.ctx != nil {
		return o.ctx
	}
	return context.Background()
}

func (o *Options) MinIssuanceTime() uint64 {
	if o.minIssuanceTimeSet {
		return o.minIssuanceTime
	}
	return uint64(time.Now().Unix())
}

func (o *Options) ChangeOwner(defaultOwner *secp256k1fx.OutputOwners) *secp256k1fx.OutputOwners {
	if o.changeOwner != nil {
		return o.changeOwner
	}
	return defaultOwner
}

func (o *Options) Memo() []byte { return o.memo }

func (o *Options) AssumeDecided() bool { return o.assumeDecided }

func (o *Options) PollFrequency() time.Duration {
	if o.pollFrequencySet {
		return o.pollFrequency
	}
	return defaultPollFrequency
}

func WithContext(ctx context.Context) Option {
	return func(o *Options) {
		o.ctx = ctx
	}
}

func WithMinIssuanceTime(minIssuanceTime uint64) Option {
	return func(o *Options) {
		o.minIssuanceTimeSet = true
		o.minIssuanceTime = minIssuanceTime
	}
}

func WithChangeOwner(changeOwner *secp256k1fx.OutputOwners) Option {
	return func(o *Options) {
		o.changeOwner = changeOwner
	}
}

func WithMemo(memo []byte) Option {
	return func(o *Options) {
		o.memo = memo
	}
}

func WithAssumeDecided() Option {
	return func(o *Options) {
		o.assumeDecided = true
	}
}

func WithPollFrequency(pollFrequency time.Duration) Option {
	return func(o *Options) {
		o.pollFrequencySet = true
		o.pollFrequency = pollFrequency
	}
}
