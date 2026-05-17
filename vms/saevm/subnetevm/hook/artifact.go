// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hook

import (
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/gastime"
)

//go:generate go run github.com/StephenButtolph/canoto/canoto $GOFILE

// gasConfigArtifact is the subnet-evm-specific projection of gaspricemanager
// state that is persisted as a hook artifact (see [hook.Points.ExecutionArtifact]).
// It is later combined with the consuming block's header in [Points.GasConfigAfter]
// to derive the gas target and price config.
//
//nolint:revive // struct-tag: canoto allows unexported fields
type gasConfigArtifact struct {
	ValidatorTargetGas bool                   `canoto:"bool,1"`
	TargetGas          gas.Gas                `canoto:"uint,2"`
	GasPriceConfig     gastime.GasPriceConfig `canoto:"value,3"`

	canotoData canotoData_gasConfigArtifact
}

// effective returns the gas target and price config represented by the
// artifact. Validity is the producer's responsibility:
// [Points.ExecutionArtifact] only writes configs that pass
// [commontype.GasPriceConfig.Verify], so consumer-side re-validation here
// would be redundant.
func (a *gasConfigArtifact) effective(headerTarget gas.Gas) (gas.Gas, gastime.GasPriceConfig) {
	if a.ValidatorTargetGas {
		return headerTarget, a.GasPriceConfig
	}
	return a.TargetGas, a.GasPriceConfig
}
