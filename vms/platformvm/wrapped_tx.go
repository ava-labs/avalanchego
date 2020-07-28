package platformvm

import (
	"bytes"
	"fmt"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/utils/formatting"
)

// WrappedTx contains the status, raw byte representation
// and JSON representation of a transaction
type WrappedTx struct {
	// ID of the tx
	ID ids.ID `serialize:"true"`
	// Status of the tx
	Status choices.Status `serialize:"true"`
	// Raw byte representation of the tx
	Tx []byte `serialize:"true"`
}

// MarshalJSON marshals [tx] to JSON
func (tx *WrappedTx) MarshalJSON() ([]byte, error) {
	buffer := bytes.NewBufferString("{")
	buffer.WriteString(fmt.Sprintf("\"id\":\"%s\",", tx.ID))
	buffer.WriteString(fmt.Sprintf("\"status\":\"%s\",", tx.Status))
	buffer.WriteString(fmt.Sprintf("\"raw\":\"%s\",", formatting.CB58{Bytes: tx.Tx}))
	buffer.WriteString("}")
	return buffer.Bytes(), nil
}
