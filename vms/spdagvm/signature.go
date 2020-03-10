// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spdagvm

import (
	"sort"

	"github.com/ava-labs/gecko/utils"
	"github.com/ava-labs/gecko/utils/crypto"
)

// Sig is a signature on a transaction
type Sig struct {
	index        uint32
	sig          []byte
	parsedPubKey []byte
}

// InputSigner stores the keys used to sign an input
type InputSigner struct {
	Keys []*crypto.PrivateKeySECP256K1R
}

type sortTxSig []*Sig

func (tsp sortTxSig) Less(i, j int) bool { return tsp[i].index < tsp[j].index }
func (tsp sortTxSig) Len() int           { return len(tsp) }
func (tsp sortTxSig) Swap(i, j int)      { tsp[j], tsp[i] = tsp[i], tsp[j] }

// SortTxSig sorts the tx signature list by index
func SortTxSig(sigs []*Sig)                  { sort.Sort(sortTxSig(sigs)) }
func isSortedAndUniqueTxSig(sig []*Sig) bool { return utils.IsSortedAndUnique(sortTxSig(sig)) }
