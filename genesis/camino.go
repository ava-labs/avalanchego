package genesis

import (
	"errors"
	"fmt"
)

func validateCaminoConfig(config *Config) error {
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

		if uint64(offer.MinDuration) < offer.UnlockHalfPeriodDuration {
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
