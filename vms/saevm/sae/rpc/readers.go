// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"errors"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/rpc"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
)

// neverErrs is a convenience wrapper, intended for use with [rawdb] Read*()
// functions, to avoid having to cast them as [blocks.DBReader] before calling
// [blocks.DBReader.WithNilErr]. It makes call sites cleaner.
func neverErrs[T any](r blocks.DBReader[T]) blocks.DBReaderWithErr[T] {
	return r.WithNilErr()
}

func notFoundIsNil[T any](x *T, err error) (*T, error) {
	if errors.Is(err, blocks.ErrNotFound) {
		return nil, nil
	}
	return x, err
}

func readByNumber[T any](c Chain, n rpc.BlockNumber, read blocks.DBReader[T]) (*T, error) {
	return notFoundIsNil(blocks.FromNumber(c, n, read.WithNilErr()))
}

func readByHash[T any](c Chain, hash common.Hash, fromMem blocks.Extractor[T], fromDB blocks.DBReader[T]) (*T, error) {
	return notFoundIsNil(blocks.FromHash(c, hash, fromMem, fromDB.WithNilErr()))
}

func readByNumberOrHash[T any](c Chain, blockNrOrHash rpc.BlockNumberOrHash, fromMem blocks.Extractor[T], fromDB blocks.DBReaderWithErr[T]) (*T, error) {
	return notFoundIsNil(blocks.FromNumberOrHash(c, blockNrOrHash, fromMem, fromDB))
}

func readByNumberAndHash[T any](c Chain, h common.Hash, num rpc.BlockNumber, fromMem blocks.Extractor[T], fromDB blocks.DBReader[T]) (*T, error) {
	return notFoundIsNil(blocks.FromNumberAndHash(c, h, num, fromMem, fromDB.WithNilErr()))
}
