// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	"io"

	ethtypes "github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
)

// HeaderExtra is a struct that contains extra fields used by Avalanche
// in the block header.
type HeaderExtra struct {
}

func (h *HeaderExtra) EncodeRLP(eth *ethtypes.Header, writer io.Writer) error {
	panic("not implemented")
}

func (h *HeaderExtra) DecodeRLP(eth *ethtypes.Header, stream *rlp.Stream) error {
	panic("not implemented")
}

func (h *HeaderExtra) EncodeJSON(eth *ethtypes.Header) ([]byte, error) {
	panic("not implemented")
}

func (h *HeaderExtra) DecodeJSON(eth *ethtypes.Header, input []byte) error {
	panic("not implemented")
}

func (h *HeaderExtra) PostCopy(dst *ethtypes.Header) {
	panic("not implemented")
}
