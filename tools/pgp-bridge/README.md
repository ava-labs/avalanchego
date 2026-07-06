# PGP KMS Bridge

A Go package and CLI for creating OpenPGP signatures using an external ECDSA signer (e.g., AWS KMS). It builds OpenPGP-compatible signature packets and self-signed public keys without requiring a local GPG keyring or private key file. The included CLI tool provides a ready-to-use signing workflow backed by AWS KMS.

## Table of Contents

- [CLI](#cli)
  - [Build](#build)
  - [Configuration](#configuration)
  - [Commands](#commands)
  - [Example: Generate a key, sign a file, and verify](#example-generate-a-key-sign-a-file-and-verify)
  - [Key tags and aliases](#key-tags-and-aliases)
- [KMS Message Type](#kms-message-type)
- [Usage](#usage)
  - [1. Implement the Signer interface](#1-implement-the-signer-interface)
  - [2. Create a Config](#2-create-a-config)
  - [3. Sign a binary](#3-sign-a-binary)
  - [4. Verify with GPG](#4-verify-with-gpg)

## CLI

The `cmd/pgp-bridge/` directory contains a CLI tool that wraps the library with AWS KMS as the signing backend.

### Build

```bash
go build -o pgp-bridge ./cmd/pgp-bridge
```

### Configuration

For non-credential inputs, precedence is **flag > env var > built-in default**.

| Flag           | Env var                       | Default                    | Description                          |
|----------------|-------------------------------|----------------------------|--------------------------------------|
| `--aws-region` | `AWS_REGION` / `AWS_DEFAULT_REGION` | `us-east-1`          | AWS region override                  |
| `--kms-host`   | `KMS_HOST`                    | *(AWS default endpoint)*   | Custom KMS endpoint host override    |
| `--user-id`    | `USER_ID`                     | *(required by `generate`)* | OpenPGP user ID, e.g. `Name <email>` |
| `--key-policy` |                               | *(AWS default key policy)* | Path to a JSON key policy file for `generate` |

**Credentials are never passed as flags.** Secrets on the command line leak
through process listings (`ps`/`/proc`), shell history, and CI logs, so
`pgp-bridge` resolves credentials only through the
[AWS SDK for Go v2](https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/config) default credential
chain: the `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` / `AWS_SESSION_TOKEN`
environment variables, shared config/credentials files, IAM roles, and SSO.
Region is taken from `--aws-region` if given, otherwise `AWS_REGION` /
`AWS_DEFAULT_REGION`, falling back to `us-east-1`. Set `--kms-host` only to target a non-default endpoint
such as a local KMS during development; the SDK selects the correct regional
AWS endpoint by default.

See [Key tags and aliases](#key-tags-and-aliases) for how `generate` tags and
aliases the key and what happens if a step fails.

### Commands

```
Usage:
  pgp-bridge [command]

Available Commands:
  generate             Create a KMS ECDSA P-256 key and export the OpenPGP public key (requires --user-id / USER_ID)
  sign <key-id>        Sign stdin using the given KMS key, write the signature to stdout
```

Run `pgp-bridge --help` or `pgp-bridge <command> --help` for the full flag list.

### Example: Generate a key, sign a file, and verify

```bash
# Generate a new key pair
$ ./pgp-bridge generate --user-id security@avalabs.org
Created KMS key: e0265682-f463-44e2-bf5e-24fb8ee2a3fe
Public key written to: e0265682-f463-44e2-bf5e-24fb8ee2a3fe.asc

# Sign a file (reads from stdin, writes signature to stdout)
$ echo "the little red fox jumps over the lazy dog" > text.txt
$ cat text.txt | ./pgp-bridge sign e0265682-f463-44e2-bf5e-24fb8ee2a3fe > sig.asc

# Verify with standard GPG
$ gpg --import e0265682-f463-44e2-bf5e-24fb8ee2a3fe.asc
gpg: key D3B90080A772C55F: public key "security@avalabs.org" imported

$ gpg --verify sig.asc text.txt
gpg: Signature made Sun Apr  5 22:48:34 2026 CEST
gpg:                using ECDSA key C3C562E518B6D2D59BB2C1FBD3B90080A772C55F
gpg: Good signature from "security@avalabs.org" [unknown]
```

### Key tags and aliases

Each key created by `generate` is stamped with a random correlation ID used for
two pieces of audit metadata:

- **Tags** on the key: `pgp-bridge:correlation-id=<id>` and
  `pgp-bridge:managed-by=pgp-bridge` (the raw user ID is kept in the key
  *description*, since KMS tag values disallow the `<`/`>` common in user IDs).
- An **alias**: `alias/pgp-bridge-<id>`, a human-friendly handle you can pass to
  `aws kms describe-key --key-id alias/pgp-bridge-<id>`.

**Non-idempotency and failure handling.** AWS KMS `CreateKey` has **no
idempotency token**, so key creation cannot be made safely retryable — any
retried or re-issued call produces a brand-new key. SDK retries are therefore
disabled on the `CreateKey` call. Because key generation is a rare,
operator-driven event, `generate` assumes `CreateKey` succeeds and does not
carry automatic-cleanup machinery: if a step *after* the key is created fails
(alias, signing, or file write), the error is returned and the created key is
left in place. Its `pgp-bridge:managed-by` / `pgp-bridge:correlation-id` tags
make such a key easy to find and remove manually.

Required IAM permissions: `kms:CreateKey`, `kms:CreateAlias`,
`kms:GetPublicKey`, and `kms:Sign`.

**Security — signing authorization.** By default, generated keys use the
**default KMS key policy**, which delegates authorization to account IAM. Pass
`--key-policy <file>` to apply a **restrictive key policy** that limits
`kms:Sign` (and ideally `kms:GetPublicKey`) to the intended signing principals —
this replaces the default policy entirely, so the document must at minimum grant
the root principal administrative access or you will lock yourself out. Treat a
release-signing key as sensitive: avoid broad `kms:Sign` on `Resource: "*"`
grants in the account regardless of which approach you use.

## KMS Message Type

The client hashes the message locally and sends only the SHA-256 digest to KMS
with `MessageType=DIGEST`. This is more efficient than sending the pre-image and
avoids transmitting raw data over the wire.

## Usage

### 1. Implement the `Signer` interface

The `Signer` interface abstracts the signing backend. Implement it to wrap AWS KMS, HSM, or any other ECDSA P-256 key source.

```go
type Signer interface {
    // Sign takes a SHA-256 digest and returns a raw DER (ASN.1) ECDSA signature.
    Sign(digest []byte) ([]byte, error)

    // PK returns the raw DER-encoded ECDSA P-256 public key.
    PK() ([]byte, error)
}
```

> **Migration note.** `Sign` and `PK` return **raw DER (ASN.1) bytes**. Earlier
> versions returned *base64-encoded* DER; the encoding was dropped deliberately
> because AWS KMS (via the SDK) already returns raw bytes, so the round-trip was
> pointless. The method signatures are unchanged, so an implementation written
> against the old contract still compiles — update it to stop base64-encoding
> its `Sign`/`PK` outputs and return raw DER.

### 2. Create a `Config`

`GenerateConfig` creates a self-signed OpenPGP public key from your signer and stores it alongside the signer in a `Config`:

```go
config, err := gpg.GenerateConfig(signer, "My Service <service@example.com>")
if err != nil {
    log.Fatal(err)
}
```

The `config.PGPPK` field contains the ASCII-armored OpenPGP public key. Write it to a file to distribute to verifiers:

```go
os.WriteFile("pk.asc", config.PGPPK, 0644)
```

### 3. Sign a binary

Create a `BinarySigner` from the config and sign any byte slice. The output is an ASCII-armored detached OpenPGP signature (`.asc`):

```go
bs := &gpg.BinarySigner{Config: config}

binary, _ := os.ReadFile("myapp")
sig, err := bs.Sign(binary)
if err != nil {
    log.Fatal(err)
}
os.WriteFile("myapp.asc", sig, 0644)
```

### 4. Verify with GPG

Distribute `config.PGPPK` as your public key file. Verifiers import it and verify as usual:

```bash
gpg --import pk.asc
gpg --verify myapp.asc myapp
```
