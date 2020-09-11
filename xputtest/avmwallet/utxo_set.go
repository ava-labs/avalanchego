// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avmwallet

import (
	"fmt"
	"strings"

	"github.com/ava-labs/avalanche-go/ids"
	"github.com/ava-labs/avalanche-go/vms/components/avax"
)

// UTXOSet ...
type UTXOSet struct {
	// Key: The id of a UTXO
	// Value: The index in UTXOs of that UTXO
	utxoMap map[[32]byte]int

	// List of UTXOs in this set
	// This can be used to iterate over. It should not be modified externally.
	UTXOs []*avax.UTXO
}

// Put ...
func (us *UTXOSet) Put(utxo *avax.UTXO) {
	if us.utxoMap == nil {
		us.utxoMap = make(map[[32]byte]int)
	}
	utxoID := utxo.InputID()
	utxoKey := utxoID.Key()
	if _, ok := us.utxoMap[utxoKey]; !ok {
		us.utxoMap[utxoKey] = len(us.UTXOs)
		us.UTXOs = append(us.UTXOs, utxo)
	}
}

// Get ...
func (us *UTXOSet) Get(id ids.ID) *avax.UTXO {
	if us.utxoMap == nil {
		return nil
	}
	if i, ok := us.utxoMap[id.Key()]; ok {
		utxo := us.UTXOs[i]
		return utxo
	}
	return nil
}

// Remove ...
func (us *UTXOSet) Remove(id ids.ID) *avax.UTXO {
	i, ok := us.utxoMap[id.Key()]
	if !ok {
		return nil
	}
	utxoI := us.UTXOs[i]

	j := len(us.UTXOs) - 1
	utxoJ := us.UTXOs[j]

	us.UTXOs[i] = us.UTXOs[j]
	us.UTXOs = us.UTXOs[:j]

	us.utxoMap[utxoJ.InputID().Key()] = i
	delete(us.utxoMap, utxoI.InputID().Key())

	return utxoI
}

// PrefixedString returns a string with each new line prefixed with [prefix]
func (us *UTXOSet) PrefixedString(prefix string) string {
	s := strings.Builder{}

	s.WriteString(fmt.Sprintf("UTXOs (length=%d):", len(us.UTXOs)))
	for i, utxo := range us.UTXOs {
		utxoID := utxo.InputID()
		txID, txIndex := utxo.InputSource()

		s.WriteString(fmt.Sprintf("\n%sUTXO[%d]:"+
			"\n%s    UTXOID: %s"+
			"\n%s    TxID: %s"+
			"\n%s    TxIndex: %d",
			prefix, i,
			prefix, utxoID,
			prefix, txID,
			prefix, txIndex))
	}

	return s.String()
}

func (us *UTXOSet) String() string { return us.PrefixedString("  ") }
