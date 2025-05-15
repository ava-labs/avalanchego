package signer

import (
	"context"
	"encoding/base64"
	"net"
	"os"
	"path/filepath"
	"testing"
	"log"

	"google.golang.org/grpc"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/proto/pb/signer"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/rpcsigner"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/spf13/viper"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

type signerServer struct {
	signer.UnimplementedSignerServer
}

func (*signerServer) PublicKey(context.Context, *signer.PublicKeyRequest) (*signer.PublicKeyResponse, error) {
	// for tests to pass, this must be the base64 encoding of a 32 byte public key
	// but it does not need to be associated with any private key
	bytes, err := base64.StdEncoding.DecodeString("j8Ndzc1I6EYWYUWAdhcwpQ1I2xX/i4fdwgJIaxbHlf9yQKMT0jlReiiLYsydgaS1")
	if err != nil {
		return nil, err
	}

	return &signer.PublicKeyResponse{
		PublicKey: bytes,
	}, nil
}

func TestGetStakingSigner(t *testing.T) {
	testKey := "HLimS3vRibTMk9lZD4b+Z+GLuSBShvgbsu0WTLt2Kd4="
	rpcServer := grpc.NewServer()
	defer rpcServer.GracefulStop()

	signer.RegisterSignerServer(rpcServer, &signerServer{})

	listener, err := net.Listen("tcp", "[::1]:0")
	require.NoError(t, err)

	go func() {
		require.NoError(t, rpcServer.Serve(listener))
	}()

	type cfg map[string]any

	tests := []struct {
		name               string
		viperKeys          string
		config             cfg
		expectedSignerType bls.Signer
		expectedErr        error
	}{
		{
			name:               "default-signer",
			expectedSignerType: &localsigner.LocalSigner{},
		},
		{
			name:               "ephemeral-signer",
			config:             cfg{config.StakingEphemeralSignerEnabledKey: true},
			expectedSignerType: &localsigner.LocalSigner{},
		},
		{
			name:               "content-key",
			config:             cfg{config.StakingSignerKeyContentKey: testKey},
			expectedSignerType: &localsigner.LocalSigner{},
		},
		{
			name: "file-key",
			config: cfg{
				config.StakingSignerKeyPathKey: func() string {
					filePath := filepath.Join(t.TempDir(), "signer.key")
					bytes, err := base64.StdEncoding.DecodeString(testKey)
					require.NoError(t, err)
					require.NoError(t, os.WriteFile(filePath, bytes, perms.ReadWrite))
					return filePath
				}(),
			},
			expectedSignerType: &localsigner.LocalSigner{},
		},
		{
			name:               "rpc-signer",
			config:             cfg{config.StakingRPCSignerKey: listener.Addr().String()},
			expectedSignerType: &rpcsigner.Client{},
		},
		{
			name: "multiple-configurations-set",
			config: cfg{
				config.StakingEphemeralSignerEnabledKey: true,
				config.StakingSignerKeyContentKey:       testKey,
			},
			expectedErr: errInvalidSignerConfig,
		},
	}

	// required for proper write permissions for the default signer-key location
	t.Setenv("HOME", t.TempDir())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			v := setupViperFlags()

			for key, value := range tt.config {
				v.Set(key, value)
			}

			config, err := config.GetNodeConfig(v)

			require.ErrorIs(err, tt.expectedErr)
			require.IsType(tt.expectedSignerType, config.StakingSigningKey)
		})
	}
}

func TestDefaultConfigInitializationUsesExistingDefaultKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	require := require.New(t)
	v := setupViperFlags()

	config1, err := config.GetNodeConfig(v)
	require.NoError(err)

	config2, err := config.GetNodeConfig(v)
	require.NoError(err)

	require.Equal(config1.StakingSigningKey.PublicKey(), config2.StakingSigningKey.PublicKey())
}

func setupViperFlags() *viper.Viper {
	v := viper.New()
	fs := config.BuildFlagSet()
	pflag.Parse()
	if err := v.BindPFlags(fs); err != nil {
		log.Fatal(err)
	}
	return v
}