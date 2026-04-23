# Reproducing the multi-party staking flow end-to-end

FOR P-CHAIN TXN BUILDING, SEE: cmd/multipartystake/TX_INTERNALS.md

All commands below assume `cwd = /Users/meag.fitz/repos/avalanchego`.

## 1. Build avalanchego + tmpnetctl

```bash
./scripts/build.sh
```

Produces `build/avalanchego` and `build/tmpnetctl`.

## 2. Start a 2-node local tmpnet

```bash
./build/tmpnetctl start-network --avalanchego-path=./build/avalanchego
```

Writes network config under `~/.tmpnet/networks/<timestamp>/` and symlinks `~/.tmpnet/networks/latest`. Defaults include `min-stake-duration=2s` and `uptime-requirement=0`, which lets a short-lived validator earn a real reward.

## 3. Extract node URI + pre-funded keys

```bash
./cmd/multipartystakeserver/tmpnet-info.sh
```

Prints node URI (e.g. `http://127.0.0.1:55654`), the first 3 pre-funded CB58 private keys, and the existing genesis stakers' BLS info.

## 4. Generate a fresh NodeID + BLS for a 3rd validator node

```bash
go run ./cmd/_genvalidatorid/
```

Prints JSON with `nodeID`, `blsPublicKey`, `blsProofOfPossession`. Save them — the validator-staking tx needs them.

## 5. Start a 3rd avalanchego as the validator to register

```bash
mkdir -p /tmp/validator-node
CHAIN_CFG=$(python3 -c "import json; print(json.load(open('$HOME/.tmpnet/networks/latest/NodeID-<one>/flags.json'))['chain-config-content'])")

./build/avalanchego \
  --data-dir=/tmp/validator-node \
  --network-id=88888 \
  --genesis-file=$HOME/.tmpnet/networks/latest/genesis.json \
  --bootstrap-ips=127.0.0.1:<existing-staking-port> \
  --bootstrap-ids=<existing-NodeID> \
  --public-ip=127.0.0.1 \
  --http-port=9660 --staking-port=9661 \
  --min-stake-duration=2s --uptime-requirement=0 \
  --chain-config-content="$CHAIN_CFG" \
  > /tmp/validator-node.log 2>&1 &
```

Confirm healthy: `curl -s http://127.0.0.1:9660/ext/health | jq '.healthy'`.

> The NodeID/BLS reported by `info.getNodeID` on `:9660` must match what you generated in step 4 — the data-dir is fresh so it auto-creates the same staking key material.

## 6. Build + run the stake server

```bash
go build -o /tmp/multipartystakeserver ./cmd/multipartystakeserver/
/tmp/multipartystakeserver --uri http://127.0.0.1:9660 --addr :8080 &
```

## 7. Run the web frontend

```bash
cd ./cmd/multipartystakeserver/web
npm install   # first run only
npm run dev   # http://localhost:5173
```

## 8. Use the UI (create party)

Open `http://localhost:5173`. Pick **Private Key (Dev)** in the wallet row. Fill in:

| Field | Example input |
|---|---|
| Wallet connect | `<key1 from step 3>` |
| NodeID | `<nodeID from step 4>` |
| BLS Public Key | `<blsPublicKey from step 4>` |
| BLS Proof of Possession | `<blsProofOfPossession from step 4>` |
| Validation Duration | `120` (seconds) |
| Delegation Fee | `2` (%) |
| Staker 1 | **Use my wallet**, amount `1000`, **Fee Payer** ✅ |
| Staker 2 | `<addr2>`, amount `300` |
| Staker 3 | `<addr3>`, amount `700` |

Total must be ≥ 2000 AVAX (min validator stake). Click **Create Party**.

> Staker addresses come from deriving each CB58 key via
> `POST /api/dev/derive-address` with `networkID=88888` — the UI does this for you when you paste the key into the "Connect" field.

## 9. Sign on the join page

On the join page, sign-validator with each of the 3 keys (Switch → paste → Connect → **Sign Validator Transaction**), then sign-split with each of the 3 keys. UI advances automatically:

```
awaiting_validator_signatures  (0→3 sigs)
      ↓
awaiting_split_signatures       (0→3 sigs)
      ↓
awaiting_validation_end         (server waits for validator to leave active set)
      ↓
complete                        (split tx committed; two explorer links shown)
```

## 10. Verify on chain

```bash
# validator tx
curl -s -X POST http://127.0.0.1:9660/ext/bc/P -H 'content-type:application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"platform.getTx","params":{"txID":"<VALIDATOR_TX_ID>","encoding":"json"}}' | jq .

# split tx
curl -s -X POST http://127.0.0.1:9660/ext/bc/P -H 'content-type:application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"platform.getTx","params":{"txID":"<SPLIT_TX_ID>","encoding":"json"}}' | jq .

# final balances per staker
curl -s -X POST http://127.0.0.1:9660/ext/bc/P -H 'content-type:application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"platform.getBalance","params":{"addresses":["<staker-addr>"]}}' | jq .
```

## Teardown

```bash
pkill -f /tmp/multipartystakeserver
pkill -f /tmp/validator-node    # the ephemeral 3rd node
./build/tmpnetctl stop-network  # tmpnet
```

## Key properties verified each run

1. **`validationRewardsOwner` = N-of-N multisig** over every party's P-chain address.
2. **`stake[]` outputs are per-owner, threshold=1** — principals never commingle, return automatically at end of validation.
3. **Split tx outputs are strictly proportional** to stake contribution; flow check: `sum(inputs) − sum(outputs) == dynamic fee` (non-zero).
