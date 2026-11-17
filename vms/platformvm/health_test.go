// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

func TestHealthCheckLocalValidatorMetrics(t *testing.T) {
	const accruedFees = 100
	tests := []struct {
		name              string
		subnetValidator   bool
		l1Validator       bool
		endAccumulatedFee uint64
		removeL1Validator bool
		wantValidating    int
		wantBalance       string // Empty if no balance is reported.
	}{
		{
			name: "not_validator",
		},
		{
			name:            "subnet_validator",
			subnetValidator: true,
			wantValidating:  1,
		},
		{
			name:              "active_l1_validator",
			l1Validator:       true,
			endAccumulatedFee: accruedFees + 900,
			wantValidating:    1,
			wantBalance:       "900",
		},
		{
			name:        "inactive_l1_validator",
			l1Validator: true,
			wantBalance: "0",
		},
		{
			name:              "removed_l1_validator",
			l1Validator:       true,
			endAccumulatedFee: accruedFees + 900,
			removeL1Validator: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			vm, _, _ := defaultVM(t, upgradetest.Latest)
			vm.ctx.Lock.Lock()
			defer vm.ctx.Lock.Unlock()

			sk, err := localsigner.New()
			require.NoError(err)
			pk := sk.PublicKey()

			subnetID := ids.GenerateTestID()
			vm.TrackedSubnets.Add(subnetID)
			if test.subnetValidator {
				require.NoError(vm.state.PutCurrentValidator(&state.Staker{
					TxID:      ids.GenerateTestID(),
					SubnetID:  constants.PrimaryNetworkID,
					NodeID:    vm.ctx.NodeID,
					PublicKey: pk,
					Weight:    1,
					EndTime:   time.Unix(1, 0),
					Priority:  platform.PrimaryNetworkValidatorCurrentPriority,
				}))
				require.NoError(vm.state.PutCurrentValidator(&state.Staker{
					TxID:     ids.GenerateTestID(),
					SubnetID: subnetID,
					NodeID:   vm.ctx.NodeID,
					Weight:   1,
					EndTime:  time.Unix(1, 0),
					Priority: platform.SubnetPermissionedValidatorCurrentPriority,
				}))
			}
			l1Validator := state.L1Validator{
				ValidationID:      ids.GenerateTestID(),
				SubnetID:          subnetID,
				NodeID:            vm.ctx.NodeID,
				PublicKey:         bls.PublicKeyToUncompressedBytes(pk),
				Weight:            1,
				EndAccumulatedFee: test.endAccumulatedFee,
			}
			if test.l1Validator {
				require.NoError(vm.state.PutL1Validator(l1Validator))
			}
			vm.state.SetAccruedFees(accruedFees)
			require.NoError(vm.state.Commit())

			_, err = vm.HealthCheck(t.Context())
			require.NoError(err)

			if test.removeL1Validator {
				l1Validator.Weight = 0
				require.NoError(vm.state.PutL1Validator(l1Validator))
				require.NoError(vm.state.Commit())

				_, err = vm.HealthCheck(t.Context())
				require.NoError(err)
			}

			want := fmt.Sprintf(`
# HELP local_validating 1 if this node is in the current validator set of the subnet, 0 otherwise
# TYPE local_validating gauge
local_validating{subnetID="%s"} %d
`,
				subnetID,
				test.wantValidating,
			)
			if test.wantBalance != "" {
				want += fmt.Sprintf(`# HELP local_l1_validator_balance Remaining balance (in nAVAX) of this node's L1 validator
# TYPE local_l1_validator_balance gauge
local_l1_validator_balance{subnetID="%s"} %s
`,
					subnetID,
					test.wantBalance,
				)
			}
			require.NoError(testutil.GatherAndCompare(
				vm.ctx.Metrics,
				strings.NewReader(want),
				"local_validating",
				"local_l1_validator_balance",
			))
		})
	}
}
