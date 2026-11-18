// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmtest

import (
	"maps"
	"math/big"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/options"

	"github.com/ava-labs/avalanchego/vms/evm/imp/vmconfig"
	"github.com/ava-labs/avalanchego/vms/evm/saetest"
)

// SUTConfig configures a VM's SUT. Chain holds the options specific to the
// VM.
type SUTConfig[C any] struct {
	Genesis core.Genesis
	Clock   *saetest.Clock
	// VMConfig is the shared part of the config that the VM is initialized
	// with.
	VMConfig vmconfig.Config
	Chain    C
}

// A SUTOption configures a [SUTConfig].
type SUTOption[C any] = options.Option[SUTConfig[C]]

// WithChainConfig applies fn to [SUTConfig.Chain].
func WithChainConfig[C any](fn func(*C)) SUTOption[C] {
	return options.Func[SUTConfig[C]](func(c *SUTConfig[C]) {
		fn(&c.Chain)
	})
}

// WithVMConfig applies fn to [SUTConfig.VMConfig].
func WithVMConfig[C any](fn func(*vmconfig.Config)) SUTOption[C] {
	return options.Func[SUTConfig[C]](func(c *SUTConfig[C]) {
		fn(&c.VMConfig)
	})
}

// WithMaxAllocFor configures the SUT's genesis to allocate the maximum possible
// balance to each address, leaving the rest of the genesis allocation intact.
func WithMaxAllocFor[C any](addrs ...common.Address) SUTOption[C] {
	return options.Func[SUTConfig[C]](func(c *SUTConfig[C]) {
		maps.Copy(c.Genesis.Alloc, saetest.MaxAllocFor(addrs...))
	})
}

// WithAccount adds an account to the SUT's genesis, leaving the rest of the
// genesis allocation intact.
func WithAccount[C any](addr common.Address, acc types.Account) SUTOption[C] {
	if acc.Balance == nil {
		acc.Balance = big.NewInt(0)
	}
	return options.Func[SUTConfig[C]](func(c *SUTConfig[C]) {
		c.Genesis.Alloc[addr] = acc
	})
}

// WithVMTime sets the clock used by the VM and returns it so that tests can
// control it. The option MAY be passed to multiple SUTs so that they share the
// clock.
func WithVMTime[C any](startTime time.Time) (SUTOption[C], *saetest.Clock) {
	clock := saetest.NewClock(startTime, time.Millisecond)
	opt := options.Func[SUTConfig[C]](func(c *SUTConfig[C]) {
		c.Clock = clock
	})
	return opt, clock
}
