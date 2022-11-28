package genesis

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
)

func validateCaminoConfig(config *Config) error {
	_, err := address.Format(
		configChainIDAlias,
		constants.GetHRP(config.NetworkID),
		config.Camino.InitialAdmin.Bytes(),
	)
	if err != nil {
		return fmt.Errorf(
			"unable to format address from %s",
			config.Camino.InitialAdmin.String(),
		)
	}

	for _, offer := range config.Camino.DepositOffers {
		if offer.Start >= offer.End {
			return fmt.Errorf(
				"deposit offer starttime (%v) is not before its endtime (%v)",
				offer.Start,
				offer.End,
			)
		}

		if offer.MinDuration > offer.MaxDuration {
			return errors.New("deposit minimum duration is greater than maximum duration")
		}

		if offer.MinDuration < offer.UnlockHalfPeriodDuration {
			return fmt.Errorf(
				"deposit offer minimum duration (%v) is less than unlock half-period duration (%v)",
				offer.MinDuration,
				offer.UnlockHalfPeriodDuration,
			)
		}

		if offer.MinDuration == 0 {
			return errors.New("deposit offer has zero  minimum duration")
		}
	}

	return nil
}
