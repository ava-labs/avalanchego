// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
)

var _ platform.TxVisitor = (*txLifetime)(nil)

// fork is a network upgrade that can bound the lifetime of a tx type.
type fork struct {
	isActivated func(upgrades *upgrade.Config, timestamp time.Time) bool
	// errNotActive is returned for a tx type introduced by this fork before
	// it activates.
	errNotActive error
	// errDeprecated is returned for a tx type deprecated by this fork once it
	// activates.
	errDeprecated error
}

// newFork returns the fork named name. Its errors are created once here, so
// they can be matched with errors.Is.
func newFork(name string, isActivated func(*upgrade.Config, time.Time) bool) *fork {
	return &fork{
		isActivated:   isActivated,
		errNotActive:  fmt.Errorf("attempting to use a feature of the %s upgrade prior to activation", name),
		errDeprecated: fmt.Errorf("transaction type is deprecated post-%s", name),
	}
}

var (
	durango = newFork("Durango", (*upgrade.Config).IsDurangoActivated)
	etna    = newFork("Etna", (*upgrade.Config).IsEtnaActivated)
	helicon = newFork("Helicon", (*upgrade.Config).IsHeliconActivated)
)

// verifyTxActivation is the first verification phase of tx processing: it rejects
// tx types that are not enabled at the current chain time. It reads nothing
// but the static upgrade schedule and a snapshot of the chain time, so it
// runs before any other work is spent on the tx.
func verifyTxActivation(upgrades upgrade.Config, timestamp time.Time, tx platform.UnsignedTx) error {
	var lifetime txLifetime
	if err := tx.Visit(&lifetime); err != nil {
		return err
	}

	if lifetime.introduced != nil && !lifetime.introduced.isActivated(&upgrades, timestamp) {
		return fmt.Errorf("%w: %T", lifetime.introduced.errNotActive, tx)
	}

	if lifetime.deprecated != nil && lifetime.deprecated.isActivated(&upgrades, timestamp) {
		return fmt.Errorf("%w: %T", lifetime.deprecated.errDeprecated, tx)
	}

	return nil
}

// txLifetime records the forks that bound the lifetime of a tx type: the fork
// that introduced it and the fork that deprecated it. A nil fork leaves that
// end of the lifetime unbounded.
//
// Only rules on the current chain time belong here. Note:
//   - [platform.AdvanceTimeTx] gate depends on the proposed timestamp, not the
//     current chain time; it remains in proposalTxExecutor.
//   - [platform.AddValidatorTx], [platform.AddSubnetValidatorTx], and
//     [platform.AddDelegatorTx] must be issued in a standard block after
//     Banff. The proposal execution path separately rejects them after Banff.
type txLifetime struct {
	introduced *fork
	deprecated *fork
}

func (*txLifetime) AddSubnetValidatorTx(*platform.AddSubnetValidatorTx) error {
	return nil
}

func (*txLifetime) CreateChainTx(*platform.CreateChainTx) error {
	return nil
}

func (*txLifetime) CreateSubnetTx(*platform.CreateSubnetTx) error {
	return nil
}

func (*txLifetime) ImportTx(*platform.ImportTx) error {
	return nil
}

func (*txLifetime) ExportTx(*platform.ExportTx) error {
	return nil
}

func (*txLifetime) AdvanceTimeTx(*platform.AdvanceTimeTx) error {
	return nil
}

func (*txLifetime) RewardValidatorTx(*platform.RewardValidatorTx) error {
	return nil
}

func (*txLifetime) RemoveSubnetValidatorTx(*platform.RemoveSubnetValidatorTx) error {
	return nil
}

func (*txLifetime) AddPermissionlessValidatorTx(*platform.AddPermissionlessValidatorTx) error {
	return nil
}

func (*txLifetime) AddPermissionlessDelegatorTx(*platform.AddPermissionlessDelegatorTx) error {
	return nil
}

func (l *txLifetime) AddValidatorTx(*platform.AddValidatorTx) error {
	l.deprecated = durango
	return nil
}

func (l *txLifetime) AddDelegatorTx(*platform.AddDelegatorTx) error {
	l.deprecated = durango
	return nil
}

func (l *txLifetime) TransferSubnetOwnershipTx(*platform.TransferSubnetOwnershipTx) error {
	l.introduced = durango
	return nil
}

func (l *txLifetime) BaseTx(*platform.BaseTx) error {
	l.introduced = durango
	return nil
}

func (l *txLifetime) TransformSubnetTx(*platform.TransformSubnetTx) error {
	l.deprecated = etna
	return nil
}

func (l *txLifetime) ConvertSubnetToL1Tx(*platform.ConvertSubnetToL1Tx) error {
	l.introduced = etna
	return nil
}

func (l *txLifetime) RegisterL1ValidatorTx(*platform.RegisterL1ValidatorTx) error {
	l.introduced = etna
	return nil
}

func (l *txLifetime) SetL1ValidatorWeightTx(*platform.SetL1ValidatorWeightTx) error {
	l.introduced = etna
	return nil
}

func (l *txLifetime) IncreaseL1ValidatorBalanceTx(*platform.IncreaseL1ValidatorBalanceTx) error {
	l.introduced = etna
	return nil
}

func (l *txLifetime) DisableL1ValidatorTx(*platform.DisableL1ValidatorTx) error {
	l.introduced = etna
	return nil
}

func (l *txLifetime) AddAutoRenewedValidatorTx(*platform.AddAutoRenewedValidatorTx) error {
	l.introduced = helicon
	return nil
}

func (l *txLifetime) SetAutoRenewedValidatorConfigTx(*platform.SetAutoRenewedValidatorConfigTx) error {
	l.introduced = helicon
	return nil
}

func (l *txLifetime) RewardAutoRenewedValidatorTx(*platform.RewardAutoRenewedValidatorTx) error {
	l.introduced = helicon
	return nil
}
