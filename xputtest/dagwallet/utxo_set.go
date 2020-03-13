// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dagwallet

import (
	"fmt"
	"strings"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/vms/spdagvm"
)

// UTXOSet ...
type UTXOSet struct {
	// This can be used to iterate over. However, it should not be modified externally.
	utxoMap map[[32]byte]int
	UTXOs   []*spdagvm.UTXO
}

// Put ...
func (us *UTXOSet) Put(utxo *spdagvm.UTXO) {
	if us.utxoMap == nil {
		us.utxoMap = make(map[[32]byte]int)
	}
	if _, ok := us.utxoMap[utxo.ID().Key()]; !ok {
		us.utxoMap[utxo.ID().Key()] = len(us.UTXOs)
		us.UTXOs = append(us.UTXOs, utxo)
	}
}

// Get ...
func (us *UTXOSet) Get(id ids.ID) *spdagvm.UTXO {
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
func (us *UTXOSet) Remove(id ids.ID) *spdagvm.UTXO {
	i, ok := us.utxoMap[id.Key()]
	if !ok {
		return nil
	}
	utxoI := us.UTXOs[i]

	j := len(us.UTXOs) - 1
	utxoJ := us.UTXOs[j]

	us.UTXOs[i] = us.UTXOs[j]
	us.UTXOs = us.UTXOs[:j]

	us.utxoMap[utxoJ.ID().Key()] = i
	delete(us.utxoMap, utxoI.ID().Key())

	return utxoI
}

func (us *UTXOSet) string(prefix string) string {
	s := strings.Builder{}

	for i, utxo := range us.UTXOs {
		out := utxo.Out().(*spdagvm.OutputPayment)
		sourceID, sourceIndex := utxo.Source()

		s.WriteString(fmt.Sprintf("%sUTXO[%d]:"+
			"\n%s    InputID: %s"+
			"\n%s    InputIndex: %d"+
			"\n%s    Locktime: %d"+
			"\n%s    Amount: %d\n",
			prefix, i,
			prefix, sourceID,
			prefix, sourceIndex,
			prefix, out.Locktime(),
			prefix, out.Amount()))
	}

	return strings.TrimSuffix(s.String(), "\n")
}

func (us *UTXOSet) String() string {
	return us.string("")
}
