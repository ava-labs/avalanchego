// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package propertyfx

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	txBytes  = []byte{0, 1, 2, 3, 4, 5}
	sigBytes = [secp256k1.SignatureLen]byte{
		0x0e, 0x33, 0x4e, 0xbc, 0x67, 0xa7, 0x3f, 0xe8,
		0x24, 0x33, 0xac, 0xa3, 0x47, 0x88, 0xa6, 0x3d,
		0x58, 0xe5, 0x8e, 0xf0, 0x3a, 0xd5, 0x84, 0xf1,
		0xbc, 0xa3, 0xb2, 0xd2, 0x5d, 0x51, 0xd6, 0x9b,
		0x0f, 0x28, 0x5d, 0xcd, 0x3f, 0x71, 0x17, 0x0a,
		0xf9, 0xbf, 0x2d, 0xb1, 0x10, 0x26, 0x5c, 0xe9,
		0xdc, 0xc3, 0x9d, 0x7a, 0x01, 0x50, 0x9d, 0xe8,
		0x35, 0xbd, 0xcb, 0x29, 0x3a, 0xd1, 0x49, 0x32,
		0x00,
	}
	addr = [hashing.AddrLen]byte{
		0x01, 0x5c, 0xce, 0x6c, 0x55, 0xd6, 0xb5, 0x09,
		0x84, 0x5c, 0x8c, 0x4e, 0x30, 0xbe, 0xd9, 0x8d,
		0x39, 0x1a, 0xe7, 0xf0,
	}
)

func TestFxInitialize(t *testing.T) {
	vm := secp256k1fx.TestVM{
		Codec: linearcodec.NewDefault(),
		Log:   logging.NoLog{},
	}
	fx := Fx{}
	require.NoError(t, fx.Initialize(&vm))
}

func TestFxInitializeInvalid(t *testing.T) {
	fx := Fx{}
	err := fx.Initialize(nil)
	require.ErrorIs(t, err, secp256k1fx.ErrWrongVMType)
}

func TestFxVerifyMintOperation(t *testing.T) {
	require := require.New(t)

	vm := secp256k1fx.TestVM{
		Codec: linearcodec.NewDefault(),
		Log:   logging.NoLog{},
	}
	date := time.Date(2019, time.January, 19, 16, 25, 17, 3, time.UTC)
	vm.Clk.Set(date)

	fx := Fx{}
	require.NoError(fx.Initialize(&vm))
	tx := &secp256k1fx.TestTx{
		UnsignedBytes: txBytes,
	}
	cred := &Credential{Credential: secp256k1fx.Credential{
		Sigs: [][secp256k1.SignatureLen]byte{
			sigBytes,
		},
	}}
	utxo := &MintOutput{OutputOwners: secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			addr,
		},
	}}
	op := &MintOperation{
		MintInput: secp256k1fx.Input{
			SigIndices: []uint32{0},
		},
		MintOutput: MintOutput{OutputOwners: secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				addr,
			},
		}},
	}

	utxos := []interface{}{utxo}
	require.NoError(fx.VerifyOperation(tx, op, cred, utxos))
}

func TestFxVerifyMintOperationWrongTx(t *testing.T) {
	require := require.New(t)

	vm := secp256k1fx.TestVM{
		Codec: linearcodec.NewDefault(),
		Log:   logging.NoLog{},
	}
	date := time.Date(2019, time.January, 19, 16, 25, 17, 3, time.UTC)
	vm.Clk.Set(date)

	fx := Fx{}
	require.NoError(fx.Initialize(&vm))
	cred := &Credential{Credential: secp256k1fx.Credential{
		Sigs: [][secp256k1.SignatureLen]byte{
			sigBytes,
		},
	}}
	utxo := &MintOutput{OutputOwners: secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			addr,
		},
	}}
	op := &MintOperation{
		MintInput: secp256k1fx.Input{
			SigIndices: []uint32{0},
		},
	}

	utxos := []interface{}{utxo}
	err := fx.VerifyOperation(nil, op, cred, utxos)
	require.ErrorIs(err, errWrongTxType)
}

func TestFxVerifyMintOperationWrongNumberUTXOs(t *testing.T) {
	require := require.New(t)

	vm := secp256k1fx.TestVM{
		Codec: linearcodec.NewDefault(),
		Log:   logging.NoLog{},
	}
	date := time.Date(2019, time.January, 19, 16, 25, 17, 3, time.UTC)
	vm.Clk.Set(date)

	fx := Fx{}
	require.NoError(fx.Initialize(&vm))
	tx := &secp256k1fx.TestTx{
		UnsignedBytes: txBytes,
	}
	cred := &Credential{Credential: secp256k1fx.Credential{
		Sigs: [][secp256k1.SignatureLen]byte{
			sigBytes,
		},
	}}
	op := &MintOperation{
		MintInput: secp256k1fx.Input{
			SigIndices: []uint32{0},
		},
	}

	utxos := []interface{}{}
	err := fx.VerifyOperation(tx, op, cred, utxos)
	require.ErrorIs(err, errWrongNumberOfUTXOs)
}

func TestFxVerifyMintOperationWrongCredential(t *testing.T) {
	require := require.New(t)

	vm := secp256k1fx.TestVM{
		Codec: linearcodec.NewDefault(),
		Log:   logging.NoLog{},
	}
	date := time.Date(2019, time.January, 19, 16, 25, 17, 3, time.UTC)
	vm.Clk.Set(date)

	fx := Fx{}
	require.NoError(fx.Initialize(&vm))
	tx := &secp256k1fx.TestTx{
		UnsignedBytes: txBytes,
	}
	utxo := &MintOutput{OutputOwners: secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			addr,
		},
	}}
	op := &MintOperation{
		MintInput: secp256k1fx.Input{
			SigIndices: []uint32{0},
		},
	}

	utxos := []interface{}{utxo}
	err := fx.VerifyOperation(tx, op, nil, utxos)
	require.ErrorIs(err, errWrongCredentialType)
}

func TestFxVerifyMintOperationInvalidUTXO(t *testing.T) {
	require := require.New(t)

	vm := secp256k1fx.TestVM{
		Codec: linearcodec.NewDefault(),
		Log:   logging.NoLog{},
	}
	date := time.Date(2019, time.January, 19, 16, 25, 17, 3, time.UTC)
	vm.Clk.Set(date)

	fx := Fx{}
	require.NoError(fx.Initialize(&vm))
	tx := &secp256k1fx.TestTx{
		UnsignedBytes: txBytes,
	}
	cred := &Credential{Credential: secp256k1fx.Credential{
		Sigs: [][secp256k1.SignatureLen]byte{
			sigBytes,
		},
	}}
	op := &MintOperation{
		MintInput: secp256k1fx.Input{
			SigIndices: []uint32{0},
		},
	}

	utxos := []interface{}{nil}
	err := fx.VerifyOperation(tx, op, cred, utxos)
	require.ErrorIs(err, errWrongUTXOType)
}

func TestFxVerifyMintOperationFailingVerification(t *testing.T) {
	require := require.New(t)

	vm := secp256k1fx.TestVM{
		Codec: linearcodec.NewDefault(),
		Log:   logging.NoLog{},
	}
	date := time.Date(2019, time.January, 19, 16, 25, 17, 3, time.UTC)
	vm.Clk.Set(date)

	fx := Fx{}
	require.NoError(fx.Initialize(&vm))
	tx := &secp256k1fx.TestTx{
		UnsignedBytes: txBytes,
	}
	cred := &Credential{Credential: secp256k1fx.Credential{
		Sigs: [][secp256k1.SignatureLen]byte{
			sigBytes,
		},
	}}
	utxo := &MintOutput{OutputOwners: secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			addr,
			ids.ShortEmpty,
		},
	}}
	op := &MintOperation{
		MintInput: secp256k1fx.Input{
			SigIndices: []uint32{0},
		},
	}

	utxos := []interface{}{utxo}
	err := fx.VerifyOperation(tx, op, cred, utxos)
	require.ErrorIs(err, secp256k1fx.ErrAddrsNotSortedUnique)
}

func TestFxVerifyMintOperationInvalidGroupID(t *testing.T) {
	require := require.New(t)

	vm := secp256k1fx.TestVM{
		Codec: linearcodec.NewDefault(),
		Log:   logging.NoLog{},
	}
	date := time.Date(2019, time.January, 19, 16, 25, 17, 3, time.UTC)
	vm.Clk.Set(date)

	fx := Fx{}
	require.NoError(fx.Initialize(&vm))
	tx := &secp256k1fx.TestTx{
		UnsignedBytes: txBytes,
	}
	cred := &Credential{Credential: secp256k1fx.Credential{
		Sigs: [][secp256k1.SignatureLen]byte{
			sigBytes,
		},
	}}
	utxo := &MintOutput{OutputOwners: secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			addr,
		},
	}}
	op := &MintOperation{
		MintInput: secp256k1fx.Input{
			SigIndices: []uint32{0},
		},
	}

	utxos := []interface{}{utxo}
	err := fx.VerifyOperation(tx, op, cred, utxos)
	require.ErrorIs(err, errWrongMintOutput)
}

func TestFxVerifyTransferOperation(t *testing.T) {
	require := require.New(t)

	vm := secp256k1fx.TestVM{
		Codec: linearcodec.NewDefault(),
		Log:   logging.NoLog{},
	}
	date := time.Date(2019, time.January, 19, 16, 25, 17, 3, time.UTC)
	vm.Clk.Set(date)

	fx := Fx{}
	require.NoError(fx.Initialize(&vm))
	tx := &secp256k1fx.TestTx{
		UnsignedBytes: txBytes,
	}
	cred := &Credential{Credential: secp256k1fx.Credential{
		Sigs: [][secp256k1.SignatureLen]byte{
			sigBytes,
		},
	}}
	utxo := &OwnedOutput{OutputOwners: secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			addr,
		},
	}}
	op := &BurnOperation{Input: secp256k1fx.Input{
		SigIndices: []uint32{0},
	}}

	utxos := []interface{}{utxo}
	require.NoError(fx.VerifyOperation(tx, op, cred, utxos))
}

func TestFxVerifyTransferOperationWrongUTXO(t *testing.T) {
	require := require.New(t)

	vm := secp256k1fx.TestVM{
		Codec: linearcodec.NewDefault(),
		Log:   logging.NoLog{},
	}
	date := time.Date(2019, time.January, 19, 16, 25, 17, 3, time.UTC)
	vm.Clk.Set(date)

	fx := Fx{}
	require.NoError(fx.Initialize(&vm))
	tx := &secp256k1fx.TestTx{
		UnsignedBytes: txBytes,
	}
	cred := &Credential{Credential: secp256k1fx.Credential{
		Sigs: [][secp256k1.SignatureLen]byte{
			sigBytes,
		},
	}}
	op := &BurnOperation{Input: secp256k1fx.Input{
		SigIndices: []uint32{0},
	}}

	utxos := []interface{}{nil}
	err := fx.VerifyOperation(tx, op, cred, utxos)
	require.ErrorIs(err, errWrongUTXOType)
}

func TestFxVerifyTransferOperationFailedVerify(t *testing.T) {
	require := require.New(t)

	vm := secp256k1fx.TestVM{
		Codec: linearcodec.NewDefault(),
		Log:   logging.NoLog{},
	}
	date := time.Date(2019, time.January, 19, 16, 25, 17, 3, time.UTC)
	vm.Clk.Set(date)

	fx := Fx{}
	require.NoError(fx.Initialize(&vm))
	tx := &secp256k1fx.TestTx{
		UnsignedBytes: txBytes,
	}
	cred := &Credential{Credential: secp256k1fx.Credential{
		Sigs: [][secp256k1.SignatureLen]byte{
			sigBytes,
		},
	}}
	utxo := &OwnedOutput{OutputOwners: secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			addr,
		},
	}}
	op := &BurnOperation{Input: secp256k1fx.Input{
		SigIndices: []uint32{1, 0},
	}}

	utxos := []interface{}{utxo}
	err := fx.VerifyOperation(tx, op, cred, utxos)
	require.ErrorIs(err, secp256k1fx.ErrInputIndicesNotSortedUnique)
}

func TestFxVerifyOperationUnknownOperation(t *testing.T) {
	require := require.New(t)

	vm := secp256k1fx.TestVM{
		Codec: linearcodec.NewDefault(),
		Log:   logging.NoLog{},
	}
	date := time.Date(2019, time.January, 19, 16, 25, 17, 3, time.UTC)
	vm.Clk.Set(date)

	fx := Fx{}
	require.NoError(fx.Initialize(&vm))
	tx := &secp256k1fx.TestTx{
		UnsignedBytes: txBytes,
	}
	cred := &Credential{Credential: secp256k1fx.Credential{
		Sigs: [][secp256k1.SignatureLen]byte{
			sigBytes,
		},
	}}
	utxo := &OwnedOutput{OutputOwners: secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			addr,
		},
	}}

	utxos := []interface{}{utxo}
	err := fx.VerifyOperation(tx, nil, cred, utxos)
	require.ErrorIs(err, errWrongOperationType)
}

func TestFxVerifyTransfer(t *testing.T) {
	require := require.New(t)

	vm := secp256k1fx.TestVM{
		Codec: linearcodec.NewDefault(),
		Log:   logging.NoLog{},
	}
	date := time.Date(2019, time.January, 19, 16, 25, 17, 3, time.UTC)
	vm.Clk.Set(date)

	fx := Fx{}
	require.NoError(fx.Initialize(&vm))
	err := fx.VerifyTransfer(nil, nil, nil, nil)
	require.ErrorIs(err, errCantTransfer)
}
