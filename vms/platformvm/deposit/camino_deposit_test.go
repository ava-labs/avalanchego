package deposit

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTotalReward(t *testing.T) {
	require := require.New(t)

	tests := map[string]struct {
		InterestRateNominator   uint64
		NoRewardsPeriodDuration uint32
		Amount                  uint64
		DepositDuration         uint64
	}{
		"MinValidatorStake": {
			InterestRateNominator:   1,
			NoRewardsPeriodDuration: 2,
			Amount:                  2000000000000,
			DepositDuration:         5,
		},
		"MinStakeDuration": {
			InterestRateNominator:   1,
			NoRewardsPeriodDuration: 2,
			Amount:                  2000000000000,
			DepositDuration:         1000,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			rewardsPeriodDuration := tt.DepositDuration - uint64(tt.NoRewardsPeriodDuration)
			expectedRewardAmount := (tt.Amount * tt.InterestRateNominator * rewardsPeriodDuration) / uint64(interestRateDenominator)

			dep := Deposit{
				Amount:   tt.Amount,
				Duration: uint32(tt.DepositDuration),
			}

			depOffer := Offer{
				InterestRateNominator:   tt.InterestRateNominator,
				NoRewardsPeriodDuration: tt.NoRewardsPeriodDuration,
			}

			require.EqualValues(expectedRewardAmount, dep.TotalReward(&depOffer))
		})
	}
}
