// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"math/big"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	ethcommon "github.com/ava-labs/libevm/common"
)

const defaultPollFrequency = 100 * time.Millisecond

// IssuanceReceipt is the information known after issuing a transaction.
type IssuanceReceipt struct {
	// Identifies the primary chain ("P", "X" or "C")
	ChainAlias string
	// ID of the issued transaction
	TxID ids.ID
	// The time from initiation to issuance
	Duration time.Duration
}

// ConfirmationReceipt is the information known after issuing and confirming a
// transaction.
type ConfirmationReceipt struct {
	// Identifies the primary chain ("P", "X" or "C")
	ChainAlias string
	// ID of the issued transaction
	TxID ids.ID
	// The time from initiation to confirmation
	TotalDuration time.Duration
	// The time from issuance to confirmation. It does not include the duration
	// of issuance.
	ConfirmationDuration time.Duration
}

type Option func(*Options)

type Options struct {
	ctx context.Context

	customAddressesSet bool
	customAddresses    set.Set[ids.ShortID]

	customEthAddressesSet bool
	customEthAddresses    set.Set[ethcommon.Address]

	baseFee *big.Int

	minIssuanceTimeSet bool
	minIssuanceTime    uint64

	allowStakeableLocked bool

	changeOwner *secp256k1fx.OutputOwners

	memo []byte

	assumeDecided bool

	pollFrequencySet bool
	pollFrequency    time.Duration

	issuanceHandler     func(IssuanceReceipt)
	confirmationHandler func(ConfirmationReceipt)

	forceSignHash bool
}

func NewOptions(ops []Option) *Options {
	o := &Options{}
	o.applyOptions(ops)
	return o
}

func UnionOptions(first, second []Option) []Option {
	firstLen := len(first)
	newOptions := make([]Option, firstLen+len(second))
	copy(newOptions, first)
	copy(newOptions[firstLen:], second)
	return newOptions
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

func (o *Options) Addresses(defaultAddresses set.Set[ids.ShortID]) set.Set[ids.ShortID] {
	if o.customAddressesSet {
		return o.customAddresses
	}
	return defaultAddresses
}

func (o *Options) EthAddresses(defaultAddresses set.Set[ethcommon.Address]) set.Set[ethcommon.Address] {
	if o.customEthAddressesSet {
		return o.customEthAddresses
	}
	return defaultAddresses
}

func (o *Options) BaseFee(defaultBaseFee *big.Int) *big.Int {
	if o.baseFee != nil {
		return o.baseFee
	}
	return defaultBaseFee
}

func (o *Options) MinIssuanceTime() uint64 {
	if o.minIssuanceTimeSet {
		return o.minIssuanceTime
	}
	return uint64(time.Now().Unix())
}

func (o *Options) AllowStakeableLocked() bool {
	return o.allowStakeableLocked
}

func (o *Options) ChangeOwner(defaultOwner *secp256k1fx.OutputOwners) *secp256k1fx.OutputOwners {
	if o.changeOwner != nil {
		return o.changeOwner
	}
	return defaultOwner
}

func (o *Options) Memo() []byte {
	return o.memo
}

func (o *Options) AssumeDecided() bool {
	return o.assumeDecided
}

func (o *Options) PollFrequency() time.Duration {
	if o.pollFrequencySet {
		return o.pollFrequency
	}
	return defaultPollFrequency
}

func (o *Options) IssuanceHandler() func(IssuanceReceipt) {
	return o.issuanceHandler
}

func (o *Options) ConfirmationHandler() func(ConfirmationReceipt) {
	return o.confirmationHandler
}

func (o *Options) ForceSignHash() bool {
	return o.forceSignHash
}

func WithContext(ctx context.Context) Option {
	return func(o *Options) {
		o.ctx = ctx
	}
}

func WithCustomAddresses(addrs set.Set[ids.ShortID]) Option {
	return func(o *Options) {
		o.customAddressesSet = true
		o.customAddresses = addrs
	}
}

func WithCustomEthAddresses(addrs set.Set[ethcommon.Address]) Option {
	return func(o *Options) {
		o.customEthAddressesSet = true
		o.customEthAddresses = addrs
	}
}

func WithBaseFee(baseFee *big.Int) Option {
	return func(o *Options) {
		o.baseFee = baseFee
	}
}

func WithMinIssuanceTime(minIssuanceTime uint64) Option {
	return func(o *Options) {
		o.minIssuanceTimeSet = true
		o.minIssuanceTime = minIssuanceTime
	}
}

func WithStakeableLocked() Option {
	return func(o *Options) {
		o.allowStakeableLocked = true
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

func WithIssuanceHandler(f func(IssuanceReceipt)) Option {
	return func(o *Options) {
		o.issuanceHandler = f
	}
}

func WithConfirmationHandler(f func(ConfirmationReceipt)) Option {
	return func(o *Options) {
		o.confirmationHandler = f
	}
}


func WithForceSignHash() Option {
	return func(o *Options) {
		o.forceSignHash = true
	}
}
