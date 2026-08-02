// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nat

import (
	"errors"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	testIntPort uint16 = 9651
	testExtPort uint16 = 9652
	testDesc           = "avalanchego-test"

	waitTimeout = 5 * time.Second
)

var (
	errTest = errors.New("test error")

	_ Router = (*testRouter)(nil)
)

type portPair struct {
	intPort uint16
	extPort uint16
}

// testRouter is a Router implementation that records calls and returns
// scripted results, allowing Mapper to be exercised without a network.
type testRouter struct {
	lock sync.Mutex

	supportsNAT   bool
	mapPortErrs   []error // consumed one per MapPort call; nil once exhausted
	externalIP    netip.Addr
	externalIPErr error

	mapCalls        []portPair
	unmapCalls      []portPair
	externalIPCalls int

	// mapped is signaled on every MapPort call so tests can wait for
	// mapping renewals without sleeping.
	mapped chan struct{}
}

func newTestRouter(supportsNAT bool, mapPortErrs ...error) *testRouter {
	return &testRouter{
		supportsNAT: supportsNAT,
		mapPortErrs: mapPortErrs,
		mapped:      make(chan struct{}, 64),
	}
}

func (r *testRouter) SupportsNAT() bool {
	return r.supportsNAT
}

func (r *testRouter) MapPort(intPort, extPort uint16, _ string, _ time.Duration) error {
	r.lock.Lock()
	r.mapCalls = append(r.mapCalls, portPair{intPort: intPort, extPort: extPort})
	var err error
	if len(r.mapPortErrs) > 0 {
		err = r.mapPortErrs[0]
		r.mapPortErrs = r.mapPortErrs[1:]
	}
	r.lock.Unlock()

	select {
	case r.mapped <- struct{}{}:
	default:
	}
	return err
}

func (r *testRouter) UnmapPort(intPort, extPort uint16) error {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.unmapCalls = append(r.unmapCalls, portPair{intPort: intPort, extPort: extPort})
	return nil
}

func (r *testRouter) ExternalIP() (netip.Addr, error) {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.externalIPCalls++
	return r.externalIP, r.externalIPErr
}

func (r *testRouter) numMapCalls() int {
	r.lock.Lock()
	defer r.lock.Unlock()

	return len(r.mapCalls)
}

func (r *testRouter) numExternalIPCalls() int {
	r.lock.Lock()
	defer r.lock.Unlock()

	return r.externalIPCalls
}

func (r *testRouter) unmapPortCalls() []portPair {
	r.lock.Lock()
	defer r.lock.Unlock()

	return r.unmapCalls
}

// waitForMapCalls blocks until MapPort has been called [n] more times or
// fails the test after a timeout.
func (r *testRouter) waitForMapCalls(t *testing.T, n int) {
	t.Helper()

	for i := 0; i < n; i++ {
		select {
		case <-r.mapped:
		case <-time.After(waitTimeout):
			t.Fatal("timed out waiting for MapPort call")
		}
	}
}

func TestRetryMapPort(t *testing.T) {
	tests := []struct {
		name         string
		mapPortErrs  []error
		wantErr      error
		wantAttempts int
	}{
		{
			name:         "first attempt succeeds",
			mapPortErrs:  nil,
			wantErr:      nil,
			wantAttempts: 1,
		},
		{
			name:         "succeeds after transient failure",
			mapPortErrs:  []error{errTest},
			wantErr:      nil,
			wantAttempts: 2,
		},
		{
			name:         "exhausts all retries",
			mapPortErrs:  []error{errTest, errTest, errTest},
			wantErr:      errTest,
			wantAttempts: maxRefreshRetries,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			router := newTestRouter(true, tt.mapPortErrs...)
			mapper := NewPortMapper(logging.NoLog{}, router)

			err := mapper.retryMapPort(testIntPort, testExtPort, testDesc, mapTimeout)
			require.ErrorIs(err, tt.wantErr)
			require.Equal(tt.wantAttempts, router.numMapCalls())
		})
	}
}

func TestMapperMapSkipsUnsupportedNAT(t *testing.T) {
	require := require.New(t)

	router := newTestRouter(false)
	mapper := NewPortMapper(logging.NoLog{}, router)

	mapper.Map(testIntPort, testExtPort, testDesc, nil, time.Hour)
	require.Zero(router.numMapCalls())

	mapper.UnmapAllPorts()
	require.Empty(router.unmapPortCalls())
}

func TestMapperMapAndUnmapOnClose(t *testing.T) {
	require := require.New(t)

	router := newTestRouter(true)
	mapper := NewPortMapper(logging.NoLog{}, router)

	// A long update interval ensures no renewals fire during the test.
	mapper.Map(testIntPort, testExtPort, testDesc, nil, time.Hour)
	require.Equal(1, router.numMapCalls())

	mapper.UnmapAllPorts()
	unmapCalls := router.unmapPortCalls()
	require.Len(unmapCalls, 1)
	require.Equal(testIntPort, unmapCalls[0].intPort)
	require.Equal(testExtPort, unmapCalls[0].extPort)
}

func TestMapperRenewsMapping(t *testing.T) {
	require := require.New(t)

	router := newTestRouter(true)
	router.externalIP = netip.MustParseAddr("5.6.7.8")
	mapper := NewPortMapper(logging.NoLog{}, router)

	ip := utils.NewAtomic(netip.AddrPortFrom(
		netip.MustParseAddr("1.2.3.4"),
		testIntPort,
	))

	mapper.Map(testIntPort, testExtPort, testDesc, ip, 10*time.Millisecond)

	// Wait for the initial mapping plus at least two renewals.
	router.waitForMapCalls(t, 3)
	mapper.UnmapAllPorts()

	require.GreaterOrEqual(router.numMapCalls(), 3)
	unmapCalls := router.unmapPortCalls()
	require.Len(unmapCalls, 1)
	require.Equal(testIntPort, unmapCalls[0].intPort)
	require.Equal(testExtPort, unmapCalls[0].extPort)

	// Renewals also refresh the external IP, preserving the port.
	require.Equal(
		netip.AddrPortFrom(netip.MustParseAddr("5.6.7.8"), testIntPort),
		ip.Get(),
	)
}

func TestUpdateIP(t *testing.T) {
	initial := netip.AddrPortFrom(netip.MustParseAddr("1.2.3.4"), testIntPort)

	tests := []struct {
		name          string
		externalIP    netip.Addr
		externalIPErr error
		want          netip.AddrPort
	}{
		{
			name:          "lookup failure keeps current ip",
			externalIPErr: errTest,
			want:          initial,
		},
		{
			name:       "unchanged ip",
			externalIP: initial.Addr(),
			want:       initial,
		},
		{
			name:       "changed ip preserves port",
			externalIP: netip.MustParseAddr("5.6.7.8"),
			want:       netip.AddrPortFrom(netip.MustParseAddr("5.6.7.8"), testIntPort),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			router := newTestRouter(true)
			router.externalIP = tt.externalIP
			router.externalIPErr = tt.externalIPErr
			mapper := NewPortMapper(logging.NoLog{}, router)

			ip := utils.NewAtomic(initial)
			mapper.updateIP(ip)
			require.Equal(tt.want, ip.Get())
		})
	}
}

func TestUpdateIPNil(t *testing.T) {
	require := require.New(t)

	router := newTestRouter(true)
	mapper := NewPortMapper(logging.NoLog{}, router)

	mapper.updateIP(nil)
	require.Zero(router.numExternalIPCalls())
}
