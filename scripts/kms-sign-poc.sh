#!/usr/bin/env bash
#
# kms-sign-poc.sh — Prove that AWS KMS can sign package digests without
#                    exposing key material outside of KMS.
#
# Usage:
#   bash scripts/kms-sign-poc.sh                       # creates a new KMS key
#   KMS_KEY_ID=alias/my-key bash scripts/kms-sign-poc.sh  # uses existing key
#
# Prerequisites: aws-cli v2, openssl, jq, base64
# AWS credentials must be configured (env vars, profile, or instance role).

set -euo pipefail

# ── Colours / helpers ────────────────────────────────────────────────────────

RED='\033[0;31m'; GREEN='\033[0;32m'; CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'

info()  { printf "${CYAN}▶ %s${NC}\n" "$*"; }
ok()    { printf "${GREEN}✔ %s${NC}\n" "$*"; }
fail()  { printf "${RED}✘ %s${NC}\n" "$*"; exit 1; }
header(){ printf "\n${BOLD}═══ %s ═══${NC}\n\n" "$*"; }

WORK_DIR=$(mktemp -d)
trap 'rm -rf "$WORK_DIR"' EXIT

AWS_REGION="${AWS_REGION:-us-east-1}"
SIGNING_ALGO="RSASSA_PKCS1_V1_5_SHA_256"
CREATED_KEY=""  # set if we create a new key (for cleanup prompt)

# ── Section 0: Prerequisites ─────────────────────────────────────────────────

header "Section 0 — Prerequisites"

for cmd in aws openssl jq base64; do
  command -v "$cmd" >/dev/null || fail "Required command not found: $cmd"
  ok "$cmd found"
done

info "Checking AWS credentials..."
CALLER=$(aws sts get-caller-identity --output json 2>&1) \
  || fail "AWS credentials not configured. Run 'aws configure' or export AWS_* env vars."
ACCOUNT=$(echo "$CALLER" | jq -r '.Account')
ok "Authenticated as account $ACCOUNT"

# ── Section 1: Key setup ─────────────────────────────────────────────────────

header "Section 1 — KMS key setup"

if [[ -n "${KMS_KEY_ID:-}" ]]; then
  info "Using existing key: $KMS_KEY_ID"
  # Resolve to full ARN for consistency
  KEY_ARN=$(aws kms describe-key --key-id "$KMS_KEY_ID" \
    --query 'KeyMetadata.Arn' --output text --region "$AWS_REGION")
  ok "Resolved to $KEY_ARN"
else
  info "Creating RSA-4096 asymmetric signing key..."
  CREATE_OUT=$(aws kms create-key \
    --key-spec RSA_4096 \
    --key-usage SIGN_VERIFY \
    --description "Package signing POC — safe to delete" \
    --region "$AWS_REGION" \
    --output json)
  KEY_ARN=$(echo "$CREATE_OUT" | jq -r '.KeyMetadata.Arn')
  KEY_ID=$(echo "$CREATE_OUT" | jq -r '.KeyMetadata.KeyId')
  CREATED_KEY="$KEY_ID"
  ok "Created key $KEY_ARN"

  info "Creating alias alias/pkg-sign-poc..."
  aws kms create-alias \
    --alias-name alias/pkg-sign-poc \
    --target-key-id "$KEY_ID" \
    --region "$AWS_REGION" 2>/dev/null \
    || info "(alias may already exist — continuing)"
  ok "Alias created"
fi

info "Exporting public key to PEM..."
aws kms get-public-key \
  --key-id "$KEY_ARN" \
  --region "$AWS_REGION" \
  --output json \
  | jq -r '.PublicKey' \
  | base64 -d > "$WORK_DIR/pubkey.der"

openssl rsa -pubin -inform DER \
  -in "$WORK_DIR/pubkey.der" \
  -outform PEM \
  -out "$WORK_DIR/pubkey.pem" 2>/dev/null
ok "Public key saved to $WORK_DIR/pubkey.pem"

# ── Section 2: Sign a test file ──────────────────────────────────────────────

header "Section 2 — Sign a test file via KMS"

# Create test payload (could be any file — an RPM, a .deb, anything)
echo "This is a test payload simulating package content." > "$WORK_DIR/payload.bin"
info "Test payload: $WORK_DIR/payload.bin"

info "Computing SHA-256 digest..."
openssl dgst -sha256 -binary "$WORK_DIR/payload.bin" > "$WORK_DIR/digest.bin"
DIGEST_HEX=$(openssl dgst -sha256 "$WORK_DIR/payload.bin" | awk '{print $NF}')
ok "SHA-256: $DIGEST_HEX"

info "Calling aws kms sign..."
SIGN_OUT=$(aws kms sign \
  --key-id "$KEY_ARN" \
  --message fileb://"$WORK_DIR/digest.bin" \
  --message-type DIGEST \
  --signing-algorithm "$SIGNING_ALGO" \
  --region "$AWS_REGION" \
  --output json)
echo "$SIGN_OUT" | jq -r '.Signature' | base64 -d > "$WORK_DIR/signature.bin"
SIG_SIZE=$(wc -c < "$WORK_DIR/signature.bin" | tr -d ' ')
ok "Signature received: $SIG_SIZE bytes → $WORK_DIR/signature.bin"

# ── Educational: equivalent raw HTTP request ─────────────────────────────────

DIGEST_B64=$(base64 < "$WORK_DIR/digest.bin" | tr -d '\n')
cat <<CURL_DOC

${BOLD}Equivalent curl request (for reference):${NC}

  POST https://kms.${AWS_REGION}.amazonaws.com/
  Content-Type: application/x-amz-json-1.1
  X-Amz-Target: TrentService.Sign
  Authorization: AWS4-HMAC-SHA256 Credential=<access-key>/${AWS_REGION}/kms/aws4_request, ...

  {
    "KeyId": "$KEY_ARN",
    "Message": "$DIGEST_B64",
    "MessageType": "DIGEST",
    "SigningAlgorithm": "$SIGNING_ALGO"
  }

  Response:
  {
    "KeyId": "$KEY_ARN",
    "Signature": "<base64-encoded PKCS#1 v1.5 signature>",
    "SigningAlgorithm": "$SIGNING_ALGO"
  }

CURL_DOC

# ── Section 3: Verify via KMS ────────────────────────────────────────────────

header "Section 3 — Verify signature via KMS (remote)"

info "Calling aws kms verify..."
VERIFY_OUT=$(aws kms verify \
  --key-id "$KEY_ARN" \
  --message fileb://"$WORK_DIR/digest.bin" \
  --message-type DIGEST \
  --signing-algorithm "$SIGNING_ALGO" \
  --signature fileb://"$WORK_DIR/signature.bin" \
  --region "$AWS_REGION" \
  --output json)

VALID=$(echo "$VERIFY_OUT" | jq -r '.SignatureValid')
if [[ "$VALID" == "true" ]]; then
  ok "KMS verification: SignatureValid = true"
else
  fail "KMS verification failed! Response: $VERIFY_OUT"
fi

# ── Section 4: Verify locally with openssl ────────────────────────────────────

header "Section 4 — Verify signature locally with openssl (offline)"

info "Running: openssl dgst -sha256 -verify pubkey.pem -signature signature.bin payload.bin"
OPENSSL_RESULT=$(openssl dgst -sha256 \
  -verify "$WORK_DIR/pubkey.pem" \
  -signature "$WORK_DIR/signature.bin" \
  "$WORK_DIR/payload.bin" 2>&1)

if [[ "$OPENSSL_RESULT" == *"Verified OK"* ]]; then
  ok "OpenSSL local verification: $OPENSSL_RESULT"
else
  fail "OpenSSL verification failed: $OPENSSL_RESULT"
fi

info "This proves consumers only need the public key — no KMS access required to verify."

# ── Section 5: Real-world payload demo ────────────────────────────────────────

header "Section 5 — Sign real-world payloads (RPM header / .deb archive)"

# For this demo we sign arbitrary files the same way — the KMS API doesn't care
# what the payload is.  In production:
#   - RPM: rpmsign extracts the header+payload digest and signs it via GPG
#   - DEB: dpkg-sig hashes the .deb ar members and signs the digest via GPG
# Both delegate to GPG, which can be backed by KMS via aws-kms-pkcs11.

sign_and_verify() {
  local label="$1" file="$2"

  info "Signing $label: $(basename "$file") ($(wc -c < "$file" | tr -d ' ') bytes)"

  openssl dgst -sha256 -binary "$file" > "$WORK_DIR/rw_digest.bin"
  local hex
  hex=$(openssl dgst -sha256 "$file" | awk '{print $NF}')

  local sig_out
  sig_out=$(aws kms sign \
    --key-id "$KEY_ARN" \
    --message fileb://"$WORK_DIR/rw_digest.bin" \
    --message-type DIGEST \
    --signing-algorithm "$SIGNING_ALGO" \
    --region "$AWS_REGION" \
    --output json)
  echo "$sig_out" | jq -r '.Signature' | base64 -d > "$WORK_DIR/rw_sig.bin"

  local result
  result=$(openssl dgst -sha256 \
    -verify "$WORK_DIR/pubkey.pem" \
    -signature "$WORK_DIR/rw_sig.bin" \
    "$file" 2>&1)

  if [[ "$result" == *"Verified OK"* ]]; then
    ok "$label — SHA-256=$hex — $result"
  else
    fail "$label verification failed: $result"
  fi
}

# Simulate an RPM header (in practice, rpmsign extracts this)
dd if=/dev/urandom bs=1 count=4096 of="$WORK_DIR/fake-rpm-header.bin" 2>/dev/null
sign_and_verify "RPM header digest" "$WORK_DIR/fake-rpm-header.bin"

# Simulate a .deb archive (in practice, dpkg-sig hashes the ar members)
dd if=/dev/urandom bs=1 count=8192 of="$WORK_DIR/fake-package.deb" 2>/dev/null
sign_and_verify ".deb archive digest" "$WORK_DIR/fake-package.deb"

# ── Section 6: Cleanup ───────────────────────────────────────────────────────

header "Section 6 — Cleanup"

if [[ -n "$CREATED_KEY" ]]; then
  info "A new KMS key was created: $KEY_ARN"
  info "To delete it (7-day waiting period):"
  echo "  aws kms schedule-key-deletion --key-id $CREATED_KEY --pending-window-in-days 7 --region $AWS_REGION"
  echo "  aws kms delete-alias --alias-name alias/pkg-sign-poc --region $AWS_REGION"
else
  info "Used existing key — no cleanup needed."
fi

# ── Summary ───────────────────────────────────────────────────────────────────

header "Summary"

cat <<'SUMMARY'
What was demonstrated:

  1. Created (or reused) an RSA-4096 asymmetric signing key in AWS KMS
  2. Signed a SHA-256 digest via the KMS Sign API
     - Private key NEVER left KMS
     - Only a 32-byte digest was sent to KMS
  3. Verified the signature via KMS Verify API (remote)
  4. Verified the signature locally with openssl (offline, public key only)
  5. Signed RPM-header-sized and .deb-archive-sized payloads (same API call)

Next steps for production package signing:

  aws-kms-pkcs11  →  gnupg-pkcs11-scd  →  gpg-agent  →  rpmsign / dpkg-sig
  (PKCS#11 bridge)   (GPG smartcard daemon)              (standard tools)

  - aws-kms-pkcs11: github.com/JackOfMostTrades/aws-kms-pkcs11
  - Pre-built .so available from GitHub releases (works on Ubuntu 22.04/24.04)
  - gnupg-pkcs11-scd: available via apt on Ubuntu LTS
  - GitHub Actions: use OIDC federation (no stored AWS secrets)
SUMMARY

ok "POC complete."
