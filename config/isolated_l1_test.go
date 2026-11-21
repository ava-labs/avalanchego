// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func newTestCreateChainTx() *platform.CreateChainTx {
	return &platform.CreateChainTx{
		SubnetID:    ids.GenerateTestID(),
		ChainName:   "isolated",
		VMID:        ids.GenerateTestID(),
		FxIDs:       []ids.ID{ids.GenerateTestID()},
		GenesisData: []byte("genesis"),
		SubnetAuth:  &secp256k1fx.Input{},
	}
}

// encodeTx signs [unsigned] and returns it in the platform.getTx hex format,
// along with its ID.
func encodeTx(t *testing.T, unsigned platform.UnsignedTx) (string, ids.ID) {
	tx, err := platform.NewSignedTx(unsigned, platform.Codec, nil)
	require.NoError(t, err)
	txHex, err := formatting.Encode(formatting.Hex, tx.Bytes())
	require.NoError(t, err)
	return txHex, tx.ID()
}

func newTestSigner(t *testing.T) *signer.ProofOfPossession {
	sk, err := localsigner.New()
	require.NoError(t, err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(t, err)
	return pop
}

// newTestIsolatedL1Config returns a valid config with two validators.
func newTestIsolatedL1Config(t *testing.T, createChainTx *platform.CreateChainTx) IsolatedL1Config {
	txHex, txID := encodeTx(t, createChainTx)
	return IsolatedL1Config{
		CreateChainTx: txHex,
		ChainID:       txID,
		Validators: []IsolatedL1Validator{
			{NodeID: ids.GenerateTestNodeID(), Weight: 6, Signer: newTestSigner(t)},
			{NodeID: ids.GenerateTestNodeID(), Weight: 4, Signer: newTestSigner(t)},
		},
	}
}

func writeConfigFile(t *testing.T, config IsolatedL1Config) string {
	configBytes, err := json.Marshal(config)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "isolated-l1.json")
	require.NoError(t, os.WriteFile(path, configBytes, 0o600))
	return path
}

func TestGetIsolatedL1NotSet(t *testing.T) {
	l1, err := getIsolatedL1(setupViperFlags())
	require.NoError(t, err)
	require.Nil(t, l1)
}

func TestGetIsolatedL1MissingFile(t *testing.T) {
	v := setupViperFlags()
	v.Set(IsolatedL1ConfigFileKey, filepath.Join(t.TempDir(), "missing.json"))

	_, err := getIsolatedL1(v)
	require.ErrorIs(t, err, errInvalidIsolatedL1Config)
}

func TestGetIsolatedL1(t *testing.T) {
	require := require.New(t)
	createChainTx := newTestCreateChainTx()
	config := newTestIsolatedL1Config(t, createChainTx)
	v := setupViperFlags()
	v.Set(IsolatedL1ConfigFileKey, writeConfigFile(t, config))

	l1, err := getIsolatedL1(v)
	require.NoError(err)

	require.Equal(config.ChainID, l1.Chain.ID)
	require.Equal(createChainTx.SubnetID, l1.Chain.SubnetID)
	require.Equal(createChainTx.VMID, l1.Chain.VMID)
	require.Equal(createChainTx.FxIDs, l1.Chain.FxIDs)
	require.Equal(createChainTx.GenesisData, l1.Chain.GenesisData)
	require.Len(l1.Validators, len(config.Validators))
	for _, vdr := range config.Validators {
		require.Equal(vdr.Weight, l1.Validators[vdr.NodeID].Weight)
		require.Equal(vdr.Signer.Key(), l1.Validators[vdr.NodeID].PublicKey)
	}
}

func TestParseIsolatedL1Config(t *testing.T) {
	createSubnetTxHex, _ := encodeTx(t, &platform.CreateSubnetTx{
		Owner: &secp256k1fx.OutputOwners{},
	})
	notATxHex, err := formatting.Encode(formatting.Hex, []byte("not a tx"))
	require.NoError(t, err)

	tests := []struct {
		name        string
		modify      func(*IsolatedL1Config)
		expectedErr error
	}{
		{
			name:   "valid",
			modify: func(*IsolatedL1Config) {},
		},
		{
			name: "tx not hex",
			modify: func(c *IsolatedL1Config) {
				c.CreateChainTx = "not hex"
			},
			expectedErr: errInvalidIsolatedL1Tx,
		},
		{
			name: "tx with bad checksum",
			modify: func(c *IsolatedL1Config) {
				txBytes, err := formatting.Decode(formatting.Hex, c.CreateChainTx)
				require.NoError(t, err)
				c.CreateChainTx = "0x" + hex.EncodeToString(txBytes) + "00000000"
			},
			expectedErr: errInvalidIsolatedL1Tx,
		},
		{
			name: "not a tx",
			modify: func(c *IsolatedL1Config) {
				c.CreateChainTx = notATxHex
			},
			expectedErr: errInvalidIsolatedL1Tx,
		},
		{
			name: "not a CreateChainTx",
			modify: func(c *IsolatedL1Config) {
				c.CreateChainTx = createSubnetTxHex
			},
			expectedErr: errInvalidIsolatedL1Tx,
		},
		{
			// Guards against running the chain under an ID that the P-chain
			// never assigned to it.
			name: "chain ID mismatch",
			modify: func(c *IsolatedL1Config) {
				c.ChainID = ids.GenerateTestID()
			},
			expectedErr: errIsolatedL1ChainIDMismatch,
		},
		{
			name: "missing chain ID",
			modify: func(c *IsolatedL1Config) {
				c.ChainID = ids.Empty
			},
			expectedErr: errIsolatedL1ChainIDMismatch,
		},
		{
			name: "no validators",
			modify: func(c *IsolatedL1Config) {
				c.Validators = nil
			},
			expectedErr: errInvalidIsolatedL1Validator,
		},
		{
			name: "zero weight",
			modify: func(c *IsolatedL1Config) {
				c.Validators[0].Weight = 0
			},
			expectedErr: errInvalidIsolatedL1Validator,
		},
		{
			name: "missing signer",
			modify: func(c *IsolatedL1Config) {
				c.Validators[0].Signer = nil
			},
			expectedErr: errInvalidIsolatedL1Validator,
		},
		{
			// A mistyped public key does not match its proof of possession.
			name: "public key does not match proof of possession",
			modify: func(c *IsolatedL1Config) {
				c.Validators[0].Signer.ProofOfPossession = c.Validators[1].Signer.ProofOfPossession
			},
			expectedErr: errInvalidIsolatedL1Validator,
		},
		{
			name: "duplicate node ID",
			modify: func(c *IsolatedL1Config) {
				c.Validators[1].NodeID = c.Validators[0].NodeID
			},
			expectedErr: errInvalidIsolatedL1Validator,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := newTestIsolatedL1Config(t, newTestCreateChainTx())
			test.modify(&config)
			configBytes, err := json.Marshal(config)
			require.NoError(t, err)

			_, err = parseIsolatedL1Config(configBytes)
			require.ErrorIs(t, err, test.expectedErr)
		})
	}
}

func TestParseIsolatedL1ConfigInvalidJSON(t *testing.T) {
	_, err := parseIsolatedL1Config([]byte("{"))
	require.ErrorIs(t, err, errInvalidIsolatedL1Config)
}

func TestIsolatedL1NodeConfig(t *testing.T) {
	createChainTx := newTestCreateChainTx()
	configFile := writeConfigFile(t, newTestIsolatedL1Config(t, createChainTx))

	t.Run("enabled", func(t *testing.T) {
		require := require.New(t)
		v := setupViperFlags()
		v.Set(NetworkNameKey, constants.FujiName)
		v.Set(DataDirKey, t.TempDir())
		v.Set(IsolatedL1ConfigFileKey, configFile)

		config, err := GetNodeConfig(v)
		require.NoError(err)

		require.Contains(config.TrackedSubnets, createChainTx.SubnetID)
		require.NotZero(config.UpgradeConfig.ApricotPhase4MinPChainHeight)
		require.Equal(config.UpgradeConfig.ApricotPhase4MinPChainHeight, config.IsolatedL1.Height)
		// Fuji nodes normally sample public bootstrappers.
		require.Empty(config.Bootstrappers)
	})

	t.Run("sybil protection disabled", func(t *testing.T) {
		v := setupViperFlags()
		v.Set(NetworkNameKey, constants.LocalName)
		v.Set(DataDirKey, t.TempDir())
		v.Set(IsolatedL1ConfigFileKey, configFile)
		v.Set(SybilProtectionEnabledKey, false)

		_, err := GetNodeConfig(v)
		require.ErrorIs(t, err, errIsolatedL1WithoutSybilProtection)
	})
}
