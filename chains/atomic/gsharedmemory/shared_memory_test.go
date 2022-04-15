// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gsharedmemory

import (
	"context"
	"io"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"

	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"github.com/chain4travel/caminogo/chains/atomic"
	"github.com/chain4travel/caminogo/database"
	"github.com/chain4travel/caminogo/database/memdb"
	"github.com/chain4travel/caminogo/database/prefixdb"
	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/utils/logging"
	"github.com/chain4travel/caminogo/utils/units"

	sharedmemorypb "github.com/chain4travel/caminogo/proto/pb/sharedmemory"
)

const (
	bufSize = units.MiB
)

func TestInterface(t *testing.T) {
	assert := assert.New(t)

	chainID0 := ids.GenerateTestID()
	chainID1 := ids.GenerateTestID()

	for _, test := range atomic.SharedMemoryTests {
		m := atomic.Memory{}
		baseDB := memdb.New()
		memoryDB := prefixdb.New([]byte{0}, baseDB)
		testDB := prefixdb.New([]byte{1}, baseDB)

		err := m.Initialize(logging.NoLog{}, memoryDB)
		assert.NoError(err)

		sm0, conn0 := wrapSharedMemory(t, m.NewSharedMemory(chainID0), baseDB)
		sm1, conn1 := wrapSharedMemory(t, m.NewSharedMemory(chainID1), baseDB)

		test(t, chainID0, chainID1, sm0, sm1, testDB)

		err = conn0.Close()
		assert.NoError(err)

		err = conn1.Close()
		assert.NoError(err)
	}
}

func wrapSharedMemory(t *testing.T, sm atomic.SharedMemory, db database.Database) (atomic.SharedMemory, io.Closer) {
	listener := bufconn.Listen(bufSize)
	server := grpc.NewServer()
	sharedmemorypb.RegisterSharedMemoryServer(server, NewServer(sm, db))
	go func() {
		if err := server.Serve(listener); err != nil {
			t.Logf("Server exited with error: %v", err)
		}
	}()

	dialer := grpc.WithContextDialer(
		func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		},
	)

	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "", dialer, grpc.WithInsecure())
	if err != nil {
		t.Fatalf("Failed to dial: %s", err)
	}

	rpcsm := NewClient(sharedmemorypb.NewSharedMemoryClient(conn))
	return rpcsm, conn
}
