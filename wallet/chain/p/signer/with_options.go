// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package signer

import (
	stdcontext "context"

	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

var _ Signer = (*withOptions)(nil)

type withOptions struct {
	signer  Signer
	options []common.Option
}

// WithOptions returns a new signer that will use the given options by default.
//
//   - [signer] is the signer that will be called to perform the underlying
//     operations.
//   - [options] will be provided to the signer in addition to the options
//     provided in the method calls.
func WithOptions(signer Signer, options ...common.Option) Signer {
	return &withOptions{
		signer:  signer,
		options: options,
	}
}

func (w *withOptions) Sign(ctx stdcontext.Context, tx *txs.Tx, options ...common.Option) error {
	return w.signer.Sign(ctx, tx, common.UnionOptions(w.options, options)...)
}

