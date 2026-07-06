// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package kms

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/aws/aws-sdk-go-v2/aws"
	awskms "github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tools/pgp-bridge/config"
)

const (
	keyID = "2d6d4299-e6da-457c-81f4-2f63c440e6f0"

	testUserID = "Test User <test@avalabs.org>"

	messageToSign = `Cat: Where are you going?
Alice: Which way should I go?
Cat: That depends on where you are going.
Alice: I don't know.
Cat: Then it doesn't matter which way you go.
`
)

// mockKMS is an in-process implementation of KMSAPI. It asserts on the typed
// request inputs the code sends and produces real ECDSA signatures with an
// ephemeral P-256 key so the end-to-end GPG verification still exercises real
// cryptography. Because the AWS SDK owns transport and SigV4, there is no HTTP
// layer to mock here.
type mockKMS struct {
	t              *testing.T
	keyID          string
	privateKey     *ecdsa.PrivateKey
	pubDER         []byte
	creationDate   time.Time // returned by CreateKey and DescribeKey
	capturedPolicy *string   // set by CreateKey from the input's Policy field
}

func newMockKMS(t *testing.T) *mockKMS {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err, "generate test key")
	pubDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	require.NoError(t, err, "marshal public key")

	return &mockKMS{
		t:            t,
		keyID:        keyID,
		privateKey:   privateKey,
		pubDER:       pubDER,
		creationDate: time.Unix(1700000000, 0),
	}
}

func (m *mockKMS) CreateKey(_ context.Context, in *awskms.CreateKeyInput, _ ...func(*awskms.Options)) (*awskms.CreateKeyOutput, error) {
	require.Equal(m.t, types.KeyUsageTypeSignVerify, in.KeyUsage, "CreateKey KeyUsage")
	require.Equal(m.t, types.KeySpecEccNistP256, in.KeySpec, "CreateKey KeySpec")
	require.Equal(m.t, "GPG signing key for "+testUserID, aws.ToString(in.Description), "CreateKey Description")
	tags := tagMap(in.Tags)
	require.Contains(m.t, tags, "pgp-bridge:correlation-id", "CreateKey correlation-id tag")
	require.Equal(m.t, "pgp-bridge", tags["pgp-bridge:managed-by"], "CreateKey managed-by tag")
	m.capturedPolicy = in.Policy
	return &awskms.CreateKeyOutput{
		KeyMetadata: &types.KeyMetadata{
			KeyId:        aws.String(m.keyID),
			CreationDate: aws.Time(m.creationDate),
		},
	}, nil
}

func (m *mockKMS) DescribeKey(_ context.Context, in *awskms.DescribeKeyInput, _ ...func(*awskms.Options)) (*awskms.DescribeKeyOutput, error) {
	require.Equal(m.t, m.keyID, aws.ToString(in.KeyId), "DescribeKey KeyId")
	return &awskms.DescribeKeyOutput{
		KeyMetadata: &types.KeyMetadata{
			KeyId:        aws.String(m.keyID),
			CreationDate: aws.Time(m.creationDate),
		},
	}, nil
}

func tagMap(tags []types.Tag) map[string]string {
	m := make(map[string]string, len(tags))
	for _, tag := range tags {
		m[aws.ToString(tag.TagKey)] = aws.ToString(tag.TagValue)
	}
	return m
}

func (m *mockKMS) CreateAlias(_ context.Context, _ *awskms.CreateAliasInput, _ ...func(*awskms.Options)) (*awskms.CreateAliasOutput, error) {
	return &awskms.CreateAliasOutput{}, nil
}

func (m *mockKMS) GetPublicKey(_ context.Context, in *awskms.GetPublicKeyInput, _ ...func(*awskms.Options)) (*awskms.GetPublicKeyOutput, error) {
	require.Equal(m.t, m.keyID, aws.ToString(in.KeyId), "GetPublicKey KeyId")
	return &awskms.GetPublicKeyOutput{PublicKey: m.pubDER}, nil
}

func (m *mockKMS) Sign(_ context.Context, in *awskms.SignInput, _ ...func(*awskms.Options)) (*awskms.SignOutput, error) {
	require.Equal(m.t, m.keyID, aws.ToString(in.KeyId), "Sign KeyId")
	require.Equal(m.t, types.MessageTypeDigest, in.MessageType, "Sign MessageType")
	require.Equal(m.t, types.SigningAlgorithmSpecEcdsaSha256, in.SigningAlgorithm, "Sign SigningAlgorithm")

	sig, err := ecdsa.SignASN1(rand.Reader, m.privateKey, in.Message)
	require.NoError(m.t, err, "sign digest")
	return &awskms.SignOutput{Signature: sig}, nil
}

func TestLoadAWSConfig_RegionResolution(t *testing.T) {
	ctx := context.Background()
	// Isolate from ambient region config so the test is deterministic.
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("AWS_CONFIG_FILE", os.DevNull)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", os.DevNull)

	// No region from any source: the us-east-1 fallback is resolved DURING config
	// load (so STS-backed credential providers would see it too).
	cfg, err := loadAWSConfig(ctx, config.AWSConfig{})
	require.NoError(t, err)
	require.Equal(t, "us-east-1", cfg.Region, "fallback region should be applied during load")

	// The --aws-region flag overrides the fallback.
	cfg, err = loadAWSConfig(ctx, config.AWSConfig{Region: "eu-west-1"})
	require.NoError(t, err)
	require.Equal(t, "eu-west-1", cfg.Region, "explicit region must win")

	// AWS_DEFAULT_REGION is honored over the fallback.
	t.Setenv("AWS_DEFAULT_REGION", "ap-south-1")
	cfg, err = loadAWSConfig(ctx, config.AWSConfig{})
	require.NoError(t, err)
	require.Equal(t, "ap-south-1", cfg.Region, "AWS_DEFAULT_REGION must win over the fallback")
}

func TestGenerate_PassesThroughKeyPolicy(t *testing.T) {
	ctx := context.Background()
	mock := newMockKMS(t)
	cli := &CLI{client: mock}

	testDir := t.TempDir()
	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(testDir))
	defer os.Chdir(origDir)

	const policy = `{"Version":"2012-10-17","Statement":[{"Sid":"AllowSign","Effect":"Allow","Principal":{"AWS":"arn:aws:iam::123456789012:role/signer"},"Action":"kms:Sign","Resource":"*"}]}`
	require.NoError(t, cli.Generate(ctx, testUserID, policy), "generate with policy")
	require.NotNil(t, mock.capturedPolicy, "CreateKey should receive the policy")
	require.Equal(t, policy, *mock.capturedPolicy)
}

func TestGenerate_OmitsPolicyWhenEmpty(t *testing.T) {
	ctx := context.Background()
	mock := newMockKMS(t)
	cli := &CLI{client: mock}

	testDir := t.TempDir()
	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(testDir))
	defer os.Chdir(origDir)

	require.NoError(t, cli.Generate(ctx, testUserID, ""), "generate without policy")
	require.Nil(t, mock.capturedPolicy, "CreateKey should not receive a policy when none is given")
}

func TestGenerateAndSign(t *testing.T) {
	ctx := context.Background()
	cli := &CLI{client: newMockKMS(t)}

	testDir := t.TempDir()
	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(testDir))
	defer os.Chdir(origDir)

	// 1. Generate a key (no custom policy)
	require.NoError(t, cli.Generate(ctx, testUserID, ""), "generate")

	// Find the generated .asc file
	matches, _ := filepath.Glob("*.asc")
	require.NotEmpty(t, matches, "no .asc file generated")
	pkPath := matches[0]
	keyFromFile := pkPath[:len(pkPath)-4] // strip .asc
	t.Logf("Generated key: %s", keyFromFile)

	// 2. Sign a message
	message := []byte(messageToSign)
	sig, err := cli.Sign(ctx, keyFromFile, message)
	require.NoError(t, err, "sign")

	msgPath := filepath.Join(testDir, "message.txt")
	sigPath := filepath.Join(testDir, "message.txt.asc")
	require.NoError(t, os.WriteFile(msgPath, message, 0644), "write message")
	require.NoError(t, os.WriteFile(sigPath, sig, 0644), "write signature")

	// 3. Verify with GPG
	gpgHome := filepath.Join(testDir, ".gnupg")
	require.NoError(t, os.MkdirAll(gpgHome, 0700), "create gnupg home")

	importCmd := exec.Command("gpg", "--homedir", gpgHome, "--no-autostart", "--import", pkPath)
	importOut, err := importCmd.CombinedOutput()
	require.NoError(t, err, "gpg import failed: %s", importOut)
	t.Logf("GPG import: %s", importOut)

	verifyCmd := exec.Command("gpg", "--homedir", gpgHome, "--no-autostart", "--verify", sigPath, msgPath)
	verifyOut, err := verifyCmd.CombinedOutput()
	require.NoError(t, err, "gpg verify failed: %s", verifyOut)
	require.Contains(t, string(verifyOut), "Good signature")
	t.Logf("GPG verify: %s", verifyOut)
}

// pgpFingerprint extracts the primary key's fingerprint from an ASCII-armored
// OpenPGP public key block, as a hex string.
func pgpFingerprint(t *testing.T, asc []byte) string {
	t.Helper()
	block, err := armor.Decode(bytes.NewReader(asc))
	require.NoError(t, err, "decode armor")
	p, err := packet.NewReader(block.Body).Next()
	require.NoError(t, err, "read packet")
	pk, ok := p.(*packet.PublicKey)
	require.True(t, ok, "first packet is a public key, got %T", p)
	return fmt.Sprintf("%x", pk.Fingerprint)
}

func TestExport(t *testing.T) {
	ctx := context.Background()
	cli := &CLI{client: newMockKMS(t)}

	testDir := t.TempDir()
	t.Chdir(testDir)

	// Export the existing key's public part; no key is created.
	asc, err := cli.Export(ctx, keyID, testUserID)
	require.NoError(t, err, "export")

	// Reproducible: a second export yields the same key identity.
	asc2, err := cli.Export(ctx, keyID, testUserID)
	require.NoError(t, err, "export again")
	require.Equal(t, pgpFingerprint(t, asc), pgpFingerprint(t, asc2), "export must be reproducible")

	// And it matches what Generate produces for the same key + creation date:
	// the fingerprint is pinned to the KMS CreationDate, not time.Now.
	require.NoError(t, cli.Generate(ctx, testUserID, ""), "generate")
	genASC, err := os.ReadFile(keyID + ".asc")
	require.NoError(t, err, "read generated .asc")
	require.Equal(t, pgpFingerprint(t, genASC), pgpFingerprint(t, asc),
		"export must reproduce the fingerprint generate produced for this key")

	// The exported key is well-formed enough for GPG to import.
	ascPath := filepath.Join(testDir, "exported.asc")
	require.NoError(t, os.WriteFile(ascPath, asc, 0644), "write exported key")
	gpgHome := filepath.Join(testDir, ".gnupg")
	require.NoError(t, os.MkdirAll(gpgHome, 0700), "create gnupg home")
	importCmd := exec.Command("gpg", "--homedir", gpgHome, "--no-autostart", "--import", ascPath)
	importOut, err := importCmd.CombinedOutput()
	require.NoError(t, err, "gpg import failed: %s", importOut)
	t.Logf("GPG import: %s", importOut)

	// And it verifies a real KMS-backed signature: the imported public key is
	// not just well-formed but actually usable to check signatures.
	message := []byte(messageToSign)
	sig, err := cli.Sign(ctx, keyID, message)
	require.NoError(t, err, "sign")
	msgPath := filepath.Join(testDir, "message.txt")
	sigPath := filepath.Join(testDir, "message.txt.asc")
	require.NoError(t, os.WriteFile(msgPath, message, 0644), "write message")
	require.NoError(t, os.WriteFile(sigPath, sig, 0644), "write signature")

	verifyCmd := exec.Command("gpg", "--homedir", gpgHome, "--no-autostart", "--verify", sigPath, msgPath)
	verifyOut, err := verifyCmd.CombinedOutput()
	require.NoError(t, err, "gpg verify failed: %s", verifyOut)
	require.Contains(t, string(verifyOut), "Good signature")
	t.Logf("GPG verify: %s", verifyOut)
}
