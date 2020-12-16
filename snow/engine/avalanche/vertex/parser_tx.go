// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm/conflicts"
)

// ParserTx parses bytes into a transaction.
type ParserTx interface {
	// Parse a transaction from a slice of bytes
	ParseTx(tx []byte) (conflicts.Tx, error)
}
