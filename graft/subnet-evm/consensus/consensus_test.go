// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package consensus

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
)

type headerOnlyReader struct{}

func (*headerOnlyReader) Config() *params.ChainConfig {
	return nil
}

func (*headerOnlyReader) CurrentHeader() *types.Header {
	return nil
}

func (*headerOnlyReader) GetHeader(common.Hash, uint64) *types.Header {
	return nil
}

func (*headerOnlyReader) GetHeaderByNumber(uint64) *types.Header {
	return nil
}

func (*headerOnlyReader) GetHeaderByHash(common.Hash) *types.Header {
	return nil
}

func TestChainHeaderReaderDoesNotRequireState(t *testing.T) {
	if _, ok := any(&headerOnlyReader{}).(ChainHeaderReader); !ok {
		t.Fatal("expected header-only implementation to satisfy ChainHeaderReader")
	}
}
