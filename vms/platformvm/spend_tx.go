package platformvm

import (
	"github.com/ava-labs/gecko/ids"
)

// SpendTx is a tx that spends AVA
type SpendTx interface {
	ID() ids.ID
}
