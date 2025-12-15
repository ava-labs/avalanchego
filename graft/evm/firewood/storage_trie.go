// Copyright 2025 the libevm authors.
//
// The libevm additions to go-ethereum are free software: you can redistribute
// them and/or modify them under the terms of the GNU Lesser General Public License
// as published by the Free Software Foundation, either version 3 of the License,
// or (at your option) any later version.
//
// The libevm additions are distributed in the hope that they will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU Lesser
// General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see
// <http://www.gnu.org/licenses/>.

package firewood

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/trie/trienode"
)

var _ state.Trie = (*storageTrie)(nil)

type storageTrie struct {
	*accountTrie
}

// `newStorageTrie` returns a wrapper around an `accountTrie` since Firewood
// does not require a separate storage trie. All changes are managed by the account trie.
func newStorageTrie(accountTrie *accountTrie) *storageTrie {
	return &storageTrie{
		accountTrie: accountTrie,
	}
}

// Commit is a no-op for storage tries, as all changes are managed by the account trie.
// It always returns a nil NodeSet and zero hash.
func (*storageTrie) Commit(bool) (common.Hash, *trienode.NodeSet, error) {
	return common.Hash{}, nil, nil
}

// Hash returns an empty hash, as the storage roots are managed internally to Firewood.
func (*storageTrie) Hash() common.Hash {
	return common.Hash{}
}

// Copy returns nil, as storage tries do not need to be copied separately.
// All usage of a copied storage trie should first ensure it is non-nil.
func (*storageTrie) Copy() *storageTrie {
	return nil
}
