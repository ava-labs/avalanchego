// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"math/big"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	ethcommon "github.com/ethereum/go-ethereum/common"
)

type PrimaryChainAlias string

const (
	PChainAlias PrimaryChainAlias = "P"
	XChainAlias PrimaryChainAlias = "X"
	CChainAlias PrimaryChainAlias = "C"

	defaultPollFrequency = 100 * time.Millisecond
)

// Signature of the function that will be called after a transaction has been issued.
type TxIssuanceHandler func(
	// Identifies the primary chain ("P", "X" or "C")
	chainAlias PrimaryChainAlias,
	// ID of the confirmed transaction
	txID ids.ID,
	// The time from initiation to issuance
	duration time.Duration,
)

// Signature of the function that will be called after a transaction has been confirmed.
type TxConfirmationHandler func(
	// Identifies the primary chain ("P", "X" or "C")
	chainAlias PrimaryChainAlias,
	// ID of the confirmed transaction
	txID ids.ID,
	// The time from initiation to confirmation (includes duration of issuance)
	totalDuration time.Duration,
	// The time from issuance to confirmation (does not include duration of issuance)
	issuanceToConfirmationDuration time.Duration,
)

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

	postIssuanceHandler     TxIssuanceHandler
	postConfirmationHandler TxConfirmationHandler
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

func (o *Options) PostIssuanceHandler() TxIssuanceHandler {
	return o.postIssuanceHandler
}

func (o *Options) PostConfirmationHandler() TxConfirmationHandler {
	return o.postConfirmationHandler
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

func WithPostIssuanceHandler(f TxIssuanceHandler) Option {
	return func(o *Options) {
		o.postIssuanceHandler = f
	}
}

func WithPostConfirmationHandler(f TxConfirmationHandler) Option {
	return func(o *Options) {
		o.postConfirmationHandler = f
	}
}

func WithLoggedIssuranceAndConfirmation(log logging.Logger) Option {
	return func(o *Options) {
		WithPostIssuanceHandler(
			func(chainAlias PrimaryChainAlias, txID ids.ID, duration time.Duration) {
				log.Info("issued transaction",
					zap.String("chainAlias", string(chainAlias)),
					zap.Stringer("txID", txID),
					zap.Duration("duration", duration),
				)
			},
		)(o)
		WithPostConfirmationHandler(
			func(chainAlias PrimaryChainAlias, txID ids.ID, totalDuration time.Duration, issuanceToConfirmationDuration time.Duration) {
				log.Info("confirmed transaction",
					zap.String("chainAlias", string(chainAlias)),
					zap.Stringer("txID", txID),
					zap.Duration("totalDuration", totalDuration),
					zap.Duration("issuanceToConfirmationDuration", issuanceToConfirmationDuration),
				)
			},
		)(o)
	}
}
