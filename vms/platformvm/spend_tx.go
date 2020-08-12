package platformvm

import (
	"github.com/ava-labs/gecko/ids"
)

// SpendTx is a tx that spends AVAX
type SpendTx interface {
	ID() ids.ID
}
