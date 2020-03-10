// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dagwallet

import (
	"fmt"
	"strings"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/vms/spdagvm"
)

// UtxoSet ...
type UtxoSet struct {
	// This can be used to iterate over. However, it should not be modified externally.
	utxoMap map[[32]byte]int
	Utxos   []*spdagvm.UTXO
}

// Put ...
func (us *UtxoSet) Put(utxo *spdagvm.UTXO) {
	if us.utxoMap == nil {
		us.utxoMap = make(map[[32]byte]int)
	}
	if _, ok := us.utxoMap[utxo.ID().Key()]; !ok {
		us.utxoMap[utxo.ID().Key()] = len(us.Utxos)
		us.Utxos = append(us.Utxos, utxo)
	}
}

// Get ...
func (us *UtxoSet) Get(id ids.ID) *spdagvm.UTXO {
	if us.utxoMap == nil {
		return nil
	}
	if i, ok := us.utxoMap[id.Key()]; ok {
		utxo := us.Utxos[i]
		return utxo
	}
	return nil
}

// Remove ...
func (us *UtxoSet) Remove(id ids.ID) *spdagvm.UTXO {
	i, ok := us.utxoMap[id.Key()]
	if !ok {
		return nil
	}
	utxoI := us.Utxos[i]

	j := len(us.Utxos) - 1
	utxoJ := us.Utxos[j]

	us.Utxos[i] = us.Utxos[j]
	us.Utxos = us.Utxos[:j]

	us.utxoMap[utxoJ.ID().Key()] = i
	delete(us.utxoMap, utxoI.ID().Key())

	return utxoI
}

func (us *UtxoSet) string(prefix string) string {
	s := strings.Builder{}

	for i, utxo := range us.Utxos {
		out := utxo.Out().(*spdagvm.OutputPayment)
		sourceID, sourceIndex := utxo.Source()

		s.WriteString(fmt.Sprintf("%sUtxo[%d]:"+
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

func (us *UtxoSet) String() string {
	return us.string("")
}
