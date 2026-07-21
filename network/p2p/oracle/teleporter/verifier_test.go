// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package teleporter

import (
	"context"
	"errors"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/oracle"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

func TestJustificationRoundTrip(t *testing.T) {
	j := Justification{
		TxHash:      [32]byte{1, 2, 3, 4, 5},
		Outbox:      common.HexToAddress("0xA50A51c09a5c451C52BB714527E1974b686D8e77"),
		BlockHeight: 1334,
	}
	got, err := ParseJustification(j.Encode())
	require.NoError(t, err)
	require.Equal(t, j, got)
}

func TestParseJustificationBadLength(t *testing.T) {
	_, err := ParseJustification([]byte{1, 2, 3})
	require.ErrorContains(t, err, "must be 60 bytes")
}

// mockSidecar captures the OracleEvent the verifier constructs so the test can
// assert the Teleporter warp message + justification were translated correctly.
type mockSidecar struct {
	got *oracle.OracleEvent
	err error
}

func (m *mockSidecar) Verify(_ context.Context, e *oracle.OracleEvent) error {
	m.got = e
	return m.err
}

func teleporterWarpMessage(t *testing.T, teleporterBytes []byte, warpAdapter common.Address) *warp.UnsignedMessage {
	t.Helper()
	ac, err := payload.NewAddressedCall(warpAdapter.Bytes(), teleporterBytes)
	require.NoError(t, err)
	um, err := warp.NewUnsignedMessage(88888, ids.GenerateTestID(), ac.Bytes())
	require.NoError(t, err)
	return um
}

func TestVerifyReconstructsOracleMessage(t *testing.T) {
	teleporterBytes := []byte("abi.encode(TeleporterMessageV2) stand-in")
	outbox := common.HexToAddress("0xA50A51c09a5c451C52BB714527E1974b686D8e77")
	warpAdapter := common.HexToAddress("0x1111111111111111111111111111111111111111")

	um := teleporterWarpMessage(t, teleporterBytes, warpAdapter)
	just := Justification{TxHash: [32]byte{9, 9, 9}, Outbox: outbox, BlockHeight: 1334}.Encode()

	mock := &mockSidecar{}
	v := NewTeleporterVerifier(mock)

	appErr := v.Verify(context.Background(), um, just)
	require.Nil(t, appErr)
	require.NotNil(t, mock.got)

	// The verifier must have built an EVM OracleMessage pointing at the outbox,
	// at the right height, carrying the Teleporter bytes verbatim, and passed the
	// raw tx hash to the sidecar.
	require.Equal(t, oracle.SourceTypeEVM, mock.got.Message.SourceType)
	require.Equal(t, outbox.Hex(), mock.got.Message.SourceAddress)
	require.Equal(t, uint64(1334), mock.got.Message.SourceBlockHeight)
	require.Equal(t, teleporterBytes, mock.got.Message.Payload)
	require.Equal(t, [32]byte{9, 9, 9}, [32]byte(mock.got.Justification))
}

func TestVerifySidecarRejection(t *testing.T) {
	um := teleporterWarpMessage(t, []byte("payload"), common.Address{1})
	just := Justification{Outbox: common.Address{2}, BlockHeight: 1}.Encode()

	v := NewTeleporterVerifier(&mockSidecar{err: errors.New("no matching log")})
	appErr := v.Verify(context.Background(), um, just)
	require.NotNil(t, appErr)
	require.Equal(t, errCodeVerify, appErr.Code)
}

func TestVerifyBadJustification(t *testing.T) {
	um := teleporterWarpMessage(t, []byte("payload"), common.Address{1})
	v := NewTeleporterVerifier(&mockSidecar{})
	appErr := v.Verify(context.Background(), um, []byte("too short"))
	require.NotNil(t, appErr)
	require.Equal(t, errCodeParse, appErr.Code)
}
