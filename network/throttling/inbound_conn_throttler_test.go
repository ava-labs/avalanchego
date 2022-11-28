// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

var _ net.Listener = (*MockListener)(nil)

type MockListener struct {
	t         *testing.T
	OnAcceptF func() (net.Conn, error)
	OnCloseF  func() error
	OnAddrF   func() net.Addr
}

func (ml *MockListener) Accept() (net.Conn, error) {
	if ml.OnAcceptF == nil {
		ml.t.Fatal("unexpectedly called Accept")
		return nil, nil
	}
	return ml.OnAcceptF()
}

func (ml *MockListener) Close() error {
	if ml.OnCloseF == nil {
		ml.t.Fatal("unexpectedly called Close")
		return nil
	}
	return ml.OnCloseF()
}

func (ml *MockListener) Addr() net.Addr {
	if ml.OnAddrF == nil {
		ml.t.Fatal("unexpectedly called Addr")
		return nil
	}
	return ml.OnAddrF()
}

func TestInboundConnThrottlerClose(t *testing.T) {
	closed := false
	l := &MockListener{
		t: t,
		OnCloseF: func() error {
			closed = true
			return nil
		},
	}
	wrappedL := NewThrottledListener(l, 1)
	err := wrappedL.Close()
	require.NoError(t, err)
	require.True(t, closed)
	select {
	case <-wrappedL.(*throttledListener).ctx.Done():
	default:
		t.Fatal("should have closed context")
	}

	// Accept() should return an error because the context is cancelled
	_, err = wrappedL.Accept()
	require.Error(t, err)
}

func TestInboundConnThrottlerAddr(t *testing.T) {
	addrCalled := false
	l := &MockListener{
		t: t,
		OnAddrF: func() net.Addr {
			addrCalled = true
			return nil
		},
	}
	wrappedL := NewThrottledListener(l, 1)
	_ = wrappedL.Addr()
	require.True(t, addrCalled)
}

func TestInboundConnThrottlerAccept(t *testing.T) {
	acceptCalled := false
	l := &MockListener{
		t: t,
		OnAcceptF: func() (net.Conn, error) {
			acceptCalled = true
			return nil, nil
		},
	}
	wrappedL := NewThrottledListener(l, 1)
	_, err := wrappedL.Accept()
	require.NoError(t, err)
	require.True(t, acceptCalled)
}
