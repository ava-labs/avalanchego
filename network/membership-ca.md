# Running a Membership CA

How to run the certificate authority that admits a subnet's non-validator
nodes (RPC, archival, stateful), and how to enrol, renew, rotate and revoke
them. Design: [membership.md](membership.md). Configuration keys:
[subnets/config.md](../subnets/config.md). Validity periods, custody and names
below are suggestions.

## Who does what

| Role | Machine | Holds | Online? |
| --- | --- | --- | --- |
| Root | offline laptop or air-gapped VM | `root.key` (secret), `root.crt`, `root.srl`; `root-ca.cnf`, `root-index.txt`, `root-crlnumber` once it revokes | never |
| Issuing | one locked-down host, access logged | `issuing-ca.key` (secret), `issuing-ca.crt`, `issuing-ca.srl`, `root.crt`, `ca.cnf`, `index.txt`, `crlnumber`, `issued/`, the certificate inventory | yes |
| Node | every node of the subnet | `staker.key` (secret, never leaves), `staker.crt` (bundle), `membership-ca.pem`, `membership-crl.pem` once anything is revoked | yes |

Only the three secret keys need protecting ([Custody](#8-custody)). Validators
are not enrolled: their P-Chain entry is their credential, and they only
install the root ([step 3](#3-install-the-root-on-every-node)).

## Before you start

- OpenSSL 3 everywhere (`openssl version`; on macOS, Homebrew's `openssl@3`),
  and bash or zsh: the signing commands pass profiles with `<(...)`.
- Synced clocks: a node whose clock is behind sees a new certificate as not yet
  valid.
- Keys are P-256 ECDSA, as AvalancheGo generates. Every certificate a node
  sends, the intermediate included, must hold a P-256, RSA-2048 or RSA-4096
  key, or peers ignore its chain; P-384, a common CA default, is refused.
  Validity: root 20 years, intermediate 10, leaf 1.
- To run unattended, put `-passin` before `-key` or `-CAkey`; OpenSSL 3.6
  ignores it after them.
- `/path/to/` stands for wherever a node keeps a file.

## 1. Create the root (once per subnet, offline)

We recommend a root per subnet. Nothing in a certificate names a subnet, so a
certificate is a member of every subnet whose `membershipCA` holds a CA in its
chain.

```bash
openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 \
  -aes-256-cbc -out root.key                     # prompts for the passphrase, twice
chmod 400 root.key

openssl req -new -x509 -key root.key -days 7300 \
  -subj "/CN=Member Root CA" \
  -addext "basicConstraints=critical,CA:TRUE,pathlen:1" \
  -addext "keyUsage=critical,keyCertSign,cRLSign" \
  -out root.crt

openssl x509 -in root.crt -noout -fingerprint -sha256
```

Record the fingerprint: it is how you confirm every node trusts this root.

## 2. Create the issuing intermediate (once)

The key is generated on the issuing host and never travels; only the request
and the signed certificate do. On the issuing host:

```bash
openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out issuing-ca.key
chmod 400 issuing-ca.key

openssl req -new -key issuing-ca.key -subj "/CN=Issuing CA" -out issuing-ca.csr
```

On the root machine. `pathlen:0` stops the intermediate issuing further CAs, and
`cRLSign` lets it sign revocation lists:

```bash
openssl x509 -req -in issuing-ca.csr -CA root.crt -CAkey root.key -CAcreateserial \
  -days 3650 -out issuing-ca.crt -extfile <(printf '%s\n' \
    'basicConstraints=critical,CA:TRUE,pathlen:0' \
    'keyUsage=critical,keyCertSign,cRLSign' \
    'subjectKeyIdentifier=hash' \
    'authorityKeyIdentifier=keyid:always')

openssl verify -CAfile root.crt issuing-ca.crt     # issuing-ca.crt: OK
```

Back on the issuing host, set up revocation now, so it is ready the day it is
needed:

```bash
mkdir -p issued
touch index.txt
echo 1000 > crlnumber
cat > ca.cnf <<'EOF'
[ ca ]
default_ca = nodes_ca

[ nodes_ca ]
database         = index.txt
crlnumber        = crlnumber
certificate      = issuing-ca.crt
private_key      = issuing-ca.key
default_md       = sha256
default_crl_days = 3650
EOF
```

Keep a certificate inventory in version control, one row per leaf (`cn`,
`serial`, `not_after`, `issued_to`, `node_id`): renewals and revocations start
from it.

## 3. Install the root on every node

Every node, validators included:

```bash
sudo install -m 0444 root.crt /path/to/membership-ca.pem
```

```json
{
  "validatorOnly": true,
  "membershipCAPath": "/path/to/membership-ca.pem"
}
```

The subnet config is `<SubnetID>.json`, identical on every node, and the subnet
must be tracked. Listing two roots stages a rotation. The node reads the file
like any CA file and skips whatever is not a certificate, so check what it
holds on each node:

```bash
while openssl x509 -noout -subject -fingerprint -sha256 -ext basicConstraints; do :; done < /path/to/membership-ca.pem
```

It prints one subject and fingerprint per root, which must match step 1
everywhere, and each must show `CA:TRUE`. A certificate without it, such as a
leaf pasted in by mistake, loads but verifies nothing, and the node does not
refuse it. `CN=Issuing CA` means the intermediate was installed instead, which
breaks intermediate rotation. After a restart the node logs
`membership CA loaded` for the subnet.

## 4. Enrol a node

On the node, as the user it runs as. Keep `CN` short: the leaf must stay under
2 KiB. The key never leaves the node.

```bash
sudo install -d -m 0750 -o <node-user> -g <node-group> /path/to/staking
cd /path/to/staking

openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out staker.key
chmod 400 staker.key

openssl req -new -key staker.key -subj "/CN=rpc-01" -out rpc-01.csr
```

On the issuing host, sign, bundle and check. The bundle is the leaf first, then
the intermediate. `-days` must not outlast the intermediate: OpenSSL does not
shorten it, and membership ends at the earliest expiry.

```bash
openssl req -in rpc-01.csr -noout -subject -verify     # confirm it is the request you expect

openssl x509 -req -in rpc-01.csr -CA issuing-ca.crt -CAkey issuing-ca.key -CAcreateserial \
  -days 365 -out rpc-01.crt -extfile <(printf '%s\n' \
    'basicConstraints=critical,CA:FALSE' \
    'keyUsage=critical,digitalSignature' \
    'extendedKeyUsage=serverAuth,clientAuth' \
    'subjectKeyIdentifier=hash' \
    'authorityKeyIdentifier=keyid:always')

cat rpc-01.crt issuing-ca.crt > rpc-01.staker.crt       # the bundle: leaf first, then the intermediate

openssl verify -CAfile root.crt -untrusted issuing-ca.crt rpc-01.crt            # rpc-01.crt: OK
test "$(openssl x509 -in rpc-01.crt -outform DER | wc -c | tr -d ' ')" -le 2048 && echo "leaf size OK"
openssl x509 -in rpc-01.crt -noout -serial -subject -enddate

mv rpc-01.crt issued/ && rm rpc-01.csr
```

Add the row to the inventory; [step 7](#7-revoke-a-certificate) revokes from
`issued/`. Return the bundle and install it on the node:

```bash
sudo install -m 0444 -o <node-user> -g <node-group> rpc-01.staker.crt /path/to/staking/staker.crt
grep -c 'BEGIN CERTIFICATE' /path/to/staking/staker.crt     # 2
```

```text
--staking-tls-cert-file=/path/to/staking/staker.crt
--staking-tls-key-file=/path/to/staking/staker.key
```

The `-content` variants take the same files base64-encoded. On restart the
node has a new node ID, a hash of the leaf: record it, and update any
`--bootstrap-ids` that named the old one. It logs `staking certificate chains
to membership CA` with the subnet, `serial` and expiry; a warning there means peers
will not admit it either ([Troubleshooting](#troubleshooting)). The subnet's
chain bootstraps once the validators admit it:

```bash
curl -s -X POST -H 'content-type: application/json' http://<node>:9650/ext/info \
  --data '{"jsonrpc":"2.0","id":1,"method":"info.isBootstrapped","params":{"chain":"<ChainID>"}}'
```

## 5. Renew a certificate

At least 30 days before expiry. The startup warning `staking certificate
membership expires soon` only fires on restart, so go by the inventory or check
on the node; the intermediate, the second block, may expire first:

```bash
cd /path/to/staking
openssl x509 -in staker.crt -noout -enddate                                  # the leaf
awk '/BEGIN CERT/{n++} n==2' staker.crt | openssl x509 -noout -enddate       # the intermediate
openssl x509 -in staker.crt -noout -checkend $((30*86400)) | grep -q 'will not expire' || echo "renew now"
```

`-checkend`'s exit status is wrong in OpenSSL 3.6; test its output. Renewal is
[step 4](#4-enrol-a-node) again, the key may be reused, and the node ID changes.
Never renew a revoked certificate: the revocation names the old serial, not the
key.

## 6. Rotate the CA

**Intermediate**, before it expires: repeat [step 2](#2-create-the-issuing-intermediate-once)
with a new key. Existing bundles keep working until their leaves expire; no
node configuration changes.

**Root**, before it expires or if `root.key` may have leaked (a leaked
`issuing-ca.key` is [revoked](#7c-revoke-the-intermediate) instead):

1. Create a new root (step 1) and append it to `membership-ca.pem` on every node.
2. Create an intermediate from it (step 2) and reissue every non-validator.
3. Remove the old root from `membership-ca.pem`, and the old intermediate's
   list from `membership-crl.pem`.

## 7. Revoke a certificate

A revocation list in the subnet config evicts a member before its certificate
expires: its messages are dropped and, under
`--network-require-validator-to-connect`, its connection is refused. Lists are
read at startup.

### 7a. Revoke a node certificate

On the issuing host, with the serial from the inventory or the node's startup
log:

```bash
for c in issued/*.crt; do openssl x509 -in "$c" -noout -serial | grep -qx "serial=<SERIAL>" && echo "$c"; done
# issued/rpc-01.crt

openssl ca -config ca.cnf -revoke issued/rpc-01.crt -crl_reason keyCompromise   # or cessationOfOperation
openssl ca -config ca.cnf -gencrl -out issuing-ca.crl

openssl verify -crl_check -CAfile root.crt -untrusted issuing-ca.crt -CRLfile issuing-ca.crl issued/rpc-01.crt
# error 23 at 0 depth lookup: certificate revoked      ← the check that the right certificate is on the list

cp issuing-ca.crl membership-crl.pem
```

The node does not check a list's signature, so the `openssl verify` above is
the check that the list is the issuer's. `openssl ca` has no undo. Each
`-gencrl` writes the whole list with a higher number: replace
`membership-crl.pem` with it. A CA that publishes DER, such as AWS Private CA, needs
`openssl crl -inform DER -in crl.der -out crl.pem` first, and must publish
complete lists, not partitions. Mark the row revoked in the inventory.

### 7b. Publish it to every node

```bash
sudo install -m 0444 membership-crl.pem /path/to/membership-crl.pem
```

Point `membershipCRLPath` at it, or put its content in `membershipCRL`, on every node,
validators included, and restart them. Each logs `membership CRL loaded` with the
list's `number`; the same number everywhere means the fleet runs the same list.
A revoked node that restarts logs `staking certificate is revoked by membership
CRL`, and the subnet's chains no longer bootstrap on it.

### 7c. Revoke the intermediate

When `issuing-ca.key` may have leaked, since it can mint certificates with new
serials. On the root machine:

```bash
touch root-index.txt
echo 1000 > root-crlnumber
cat > root-ca.cnf <<'EOF'
[ ca ]
default_ca = root_ca

[ root_ca ]
database         = root-index.txt
crlnumber        = root-crlnumber
certificate      = root.crt
private_key      = root.key
default_md       = sha256
default_crl_days = 3650
EOF

openssl ca -config root-ca.cnf -revoke issuing-ca.crt -crl_reason CACompromise
openssl ca -config root-ca.cnf -gencrl -out root.crl
```

Every leaf the old intermediate issued drops out with it, so first create a new
intermediate and reissue the non-validators, unless cutting the leak off now
matters more. Then publish `root.crl` as in [7b](#7b-publish-it-to-every-node),
in one file with the new intermediate's list if there is one
(`cat root.crl issuing-ca.crl > membership-crl.pem`).

### 7d. Other cases

- **A node you control:** stop it and destroy `staker.key`; revoking it too
  costs one list update.
- **A validator:** remove it from the validator set. Revocation does not apply.
- **A leaked root key:** rotate the root ([step 6](#6-rotate-the-ca)).

## 8. Custody

- `root.key`: passphrase-encrypted, on two offline media in two places, with
  the passphrase split between two custodians. Used only to issue or revoke an
  intermediate.
- `issuing-ca.key`: on the issuing host only, mode 0400, access logged. An HSM or
  KMS later changes nothing on the nodes.
- `staker.key`: generated on the node, excluded from backups. Losing it means
  re-enrolling.
- The inventory, `issued/`, `index.txt` and the CRL counters: in version
  control. Losing `index.txt` drops every revocation from the next list.

## Troubleshooting

| Symptom | Likely cause | Check |
| --- | --- | --- |
| Warning `staking certificate does not chain ...`, `chainLen=1` | The bundle holds the leaf alone | `grep -c 'BEGIN CERTIFICATE' staker.crt` prints 2 |
| Same warning, `chainLen=2` | Wrong root, an expired certificate, clock skew, or an intermediate key other than P-256, RSA-2048 or RSA-4096 | `openssl verify -CAfile membership-ca.pem -untrusted staker.crt <leaf>`; the step 5 dates; `date -u`; `awk '/BEGIN CERT/{n++} n==2' staker.crt \| openssl x509 -noout -text \| grep 'Public-Key\|ASN1 OID'` |
| Warning `staking certificate is revoked by membership CRL` | The node's certificate is on the list | The inventory; reissue it if that was a mistake |
| Warning `staking certificate membership expires soon` | The leaf or intermediate expires within 30 days | Renew ([step 5](#5-renew-a-certificate)) or rotate the intermediate |
| No `membership CA loaded` line | No `membershipCAPath`/`membershipCA`, a misspelt key, or the subnet is not tracked | The subnet config and `--track-subnets` |
| The node refuses to start on `membershipCAPath` | No certificate in the file, or the file was cut short | The command in [step 3](#3-install-the-root-on-every-node); copy the file again |
| The node refuses to start on `membershipCRLPath`/`membershipCRL` | No list in the file, a list that does not parse, a partitioned list, or a file cut short | Rebuild it as in [7a](#7a-revoke-a-node-certificate) |
| The node refuses to start parsing its own certificate | The leaf is over 2 KiB | `openssl x509 -in staker.crt -outform DER \| wc -c` |
| No warning, but the subnet's chain never bootstraps | The validators trust a different root, or their root is not `CA:TRUE` | Compare the output of [step 3](#3-install-the-root-on-every-node) across nodes |
