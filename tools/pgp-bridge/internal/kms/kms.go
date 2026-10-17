// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package kms

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"

	"github.com/ava-labs/avalanchego/tools/pgp-bridge/config"
	"github.com/ava-labs/avalanchego/tools/pgp-bridge/pgp"
)

// httpTimeout bounds every KMS HTTP call so a request can never hang forever.
const httpTimeout = 30 * time.Second

// defaultRegion is used only when no region is set by the --aws-region flag,
// AWS_REGION, or AWS_DEFAULT_REGION, so a credentials-only environment still
// resolves a KMS endpoint.
const defaultRegion = "us-east-1"

// Provenance tags stamped on every key this tool creates, for audit/ownership.
const (
	tagCorrelationID = "pgp-bridge:correlation-id"
	tagManagedBy     = "pgp-bridge:managed-by"
	managedByValue   = "pgp-bridge"
)

// KMSAPI is the subset of the AWS KMS client used by this package. *kms.Client
// satisfies it; tests substitute an in-process implementation.
type KMSAPI interface {
	CreateKey(ctx context.Context, in *kms.CreateKeyInput, optFns ...func(*kms.Options)) (*kms.CreateKeyOutput, error)
	GetPublicKey(ctx context.Context, in *kms.GetPublicKeyInput, optFns ...func(*kms.Options)) (*kms.GetPublicKeyOutput, error)
	Sign(ctx context.Context, in *kms.SignInput, optFns ...func(*kms.Options)) (*kms.SignOutput, error)
	CreateAlias(ctx context.Context, in *kms.CreateAliasInput, optFns ...func(*kms.Options)) (*kms.CreateAliasOutput, error)
	DescribeKey(ctx context.Context, in *kms.DescribeKeyInput, optFns ...func(*kms.Options)) (*kms.DescribeKeyOutput, error)
}

// CLI provides the generate and sign commands backed by AWS KMS.
type CLI struct {
	client KMSAPI
}

// NewCLI builds a CLI backed by a real AWS KMS client. Credentials are resolved
// by the SDK's default credential chain (env vars, shared config, IAM role,
// SSO) — never passed as arguments — so secrets never reach the process's argv.
// A custom Host is applied as the client's base endpoint to support local KMS
// implementations; Region overrides the SDK's resolved region when set.
func NewCLI(ctx context.Context, cfg config.AWSConfig) (*CLI, error) {
	awsCfg, err := loadAWSConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}

	var kmsOpts []func(*kms.Options)
	if cfg.Host != "" {
		kmsOpts = append(kmsOpts, func(o *kms.Options) {
			o.BaseEndpoint = aws.String(endpointURL(cfg.Host))
		})
	}

	return &CLI{client: kms.NewFromConfig(awsCfg, kmsOpts...)}, nil
}

// loadAWSConfig resolves the AWS config used by NewCLI. Region precedence is
// cfg.Region (the --aws-region flag) > AWS_REGION > AWS_DEFAULT_REGION >
// defaultRegion. The default goes through config loading via WithDefaultRegion
// (not a post-load mutation) so STS-backed credential providers
// (AssumeRole/WebIdentity/SSO) constructed during LoadDefaultConfig also resolve
// a region.
func loadAWSConfig(ctx context.Context, cfg config.AWSConfig) (aws.Config, error) {
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithHTTPClient(awshttp.NewBuildableClient().WithTimeout(httpTimeout)),
		awsconfig.WithDefaultRegion(defaultRegion),
	}
	if cfg.Region != "" {
		opts = append(opts, awsconfig.WithRegion(cfg.Region))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return aws.Config{}, fmt.Errorf("load AWS config: %w", err)
	}
	return awsCfg, nil
}

// endpointURL turns a bare host into a base endpoint URL, defaulting to https
// but using http for local hosts (where TLS is typically unavailable).
func endpointURL(host string) string {
	scheme := "https"
	if strings.HasPrefix(host, "localhost") || strings.HasPrefix(host, "127.0.0.1") {
		scheme = "http"
	}
	return scheme + "://" + host
}

// Generate creates a new ECDSA P-256 key in AWS KMS, produces a self-signed
// OpenPGP public key, and writes it to <key-id>.asc, tagging the key and giving
// it an alias for reference. When keyPolicy is non-empty it is attached as the
// key's resource policy (a JSON document); otherwise the default KMS key policy
// applies (authorization delegated to account IAM).
//
// Key generation is a rare, operator-driven event. NOTE: AWS KMS CreateKey has
// no idempotency token, so key creation cannot be made safely retryable — any
// retried or re-issued call produces a brand-new key. SDK retries are therefore
// disabled on the CreateKey call below, and Generate otherwise assumes CreateKey
// succeeds: an error in any later step is returned as-is, leaving the created
// (tagged, aliased) key in place for manual handling. Given how rarely keys are
// generated, that is preferred over carrying automatic-cleanup machinery.
func (c *CLI) Generate(ctx context.Context, userID, keyPolicy string) error {
	// Validate before creating the key so a bad input does not leave an
	// orphaned KMS key behind.
	if err := pgp.ValidateUserID(userID); err != nil {
		return err
	}

	correlationID, err := newCorrelationID()
	if err != nil {
		return err
	}
	alias := aliasForCorrelationID(correlationID)

	// CreateKey has no idempotency token (see the note above), so disable SDK
	// retries: a retry after a transient timeout would create a duplicate key
	// rather than return the original.
	input := &kms.CreateKeyInput{
		KeyUsage:    types.KeyUsageTypeSignVerify,
		KeySpec:     types.KeySpecEccNistP256,
		Description: aws.String(fmt.Sprintf("GPG signing key for %s", userID)),
		Tags: []types.Tag{
			{TagKey: aws.String(tagCorrelationID), TagValue: aws.String(correlationID)},
			{TagKey: aws.String(tagManagedBy), TagValue: aws.String(managedByValue)},
		},
	}
	if keyPolicy != "" {
		input.Policy = aws.String(keyPolicy)
	}
	out, err := c.client.CreateKey(ctx, input, func(o *kms.Options) {
		o.Retryer = aws.NopRetryer{}
	})
	if err != nil {
		return fmt.Errorf("create key: %w", err)
	}
	if out.KeyMetadata == nil || out.KeyMetadata.KeyId == nil || *out.KeyMetadata.KeyId == "" {
		return fmt.Errorf("create key: empty key ID in response")
	}
	if out.KeyMetadata.CreationDate == nil {
		return fmt.Errorf("create key: missing creation date in response")
	}
	keyID := *out.KeyMetadata.KeyId
	fmt.Fprintf(os.Stderr, "Created KMS key: %s (alias %s)\n", keyID, alias)

	if _, err := c.client.CreateAlias(ctx, &kms.CreateAliasInput{
		AliasName:   aws.String(alias),
		TargetKeyId: aws.String(keyID),
	}); err != nil {
		return fmt.Errorf("create alias %s: %w", alias, err)
	}

	// Pin the PGP creation timestamp to the KMS key's CreationDate (not
	// time.Now) so the derived public key's fingerprint is stable and can be
	// reproduced later by `export` for this same key.
	signer := &Signer{ctx: ctx, client: c.client, keyID: keyID}
	cfg, err := pgp.GenerateConfig(signer, userID, *out.KeyMetadata.CreationDate)
	if err != nil {
		return fmt.Errorf("generate config: %w", err)
	}

	filename := keyID + ".asc"
	if err := WriteFileAtomic(filename, cfg.PGPPK, 0644); err != nil {
		return fmt.Errorf("write public key: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Public key written to: %s\n", filename)
	return nil
}

// aliasForCorrelationID is the KMS alias name a correlation ID maps to.
func aliasForCorrelationID(id string) string { return "alias/pgp-bridge-" + id }

// newCorrelationID returns a random RFC 4122 v4 UUID, used to name the key's
// alias and tag.
func newCorrelationID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate correlation ID: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// WriteFileAtomic writes data to a temp file and renames it into place so a
// failure never leaves a partially written public-key file behind.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// Sign reads a binary message and returns a detached ASCII-armored OpenPGP signature.
// The public key file (<keyID>.asc) must have been created by a prior call to Generate.
func (c *CLI) Sign(ctx context.Context, keyID string, message []byte) ([]byte, error) {
	pgpPK, err := os.ReadFile(keyID + ".asc")
	if err != nil {
		return nil, fmt.Errorf("read public key file %s.asc: %w", keyID, err)
	}

	signer := &Signer{ctx: ctx, client: c.client, keyID: keyID}
	bs := &pgp.BinarySigner{Config: pgp.Config{
		Signer: signer,
		PGPPK:  pgpPK,
	}}
	return bs.Sign(message)
}

// Export derives an ASCII-armored OpenPGP public key from an existing KMS key,
// without creating anything. keyID may be a key ID, ARN, or alias. The PGP
// creation timestamp is pinned to the key's KMS CreationDate, so the resulting
// key's fingerprint is stable — repeated exports (and the original Generate)
// yield the same key identity. The returned bytes are the .asc block; the caller
// decides where to write them.
func (c *CLI) Export(ctx context.Context, keyID, userID string) ([]byte, error) {
	if err := pgp.ValidateUserID(userID); err != nil {
		return nil, err
	}

	out, err := c.client.DescribeKey(ctx, &kms.DescribeKeyInput{KeyId: aws.String(keyID)})
	if err != nil {
		return nil, fmt.Errorf("describe key: %w", err)
	}
	if out.KeyMetadata == nil || out.KeyMetadata.CreationDate == nil {
		return nil, fmt.Errorf("describe key: missing creation date in response")
	}

	signer := &Signer{ctx: ctx, client: c.client, keyID: keyID}
	config, err := pgp.GenerateConfig(signer, userID, *out.KeyMetadata.CreationDate)
	if err != nil {
		return nil, fmt.Errorf("generate config: %w", err)
	}
	return config.PGPPK, nil
}

// Signer implements pgp.Signer using AWS KMS. The context is stored on the
// struct because pgp.Signer's methods take only a digest; it is set per command
// invocation so KMS calls honor cancellation and the HTTP timeout.
type Signer struct {
	ctx    context.Context
	client KMSAPI
	keyID  string
}

// Sign returns the raw DER-encoded ECDSA signature over the given SHA-256 digest.
func (s *Signer) Sign(digest []byte) ([]byte, error) {
	out, err := s.client.Sign(s.ctx, &kms.SignInput{
		KeyId:            aws.String(s.keyID),
		Message:          digest,
		MessageType:      types.MessageTypeDigest,
		SigningAlgorithm: types.SigningAlgorithmSpecEcdsaSha256,
	})
	if err != nil {
		return nil, fmt.Errorf("KMS Sign: %w", err)
	}
	return out.Signature, nil
}

// PK returns the raw DER-encoded public key for the configured KMS key.
func (s *Signer) PK() ([]byte, error) {
	out, err := s.client.GetPublicKey(s.ctx, &kms.GetPublicKeyInput{
		KeyId: aws.String(s.keyID),
	})
	if err != nil {
		return nil, fmt.Errorf("KMS GetPublicKey: %w", err)
	}
	return out.PublicKey, nil
}
