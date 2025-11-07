# Block Database Verifier

Continuously verifies block consistency between two Avalanche C-Chain RPC endpoints by comparing complete JSON responses for blocks, receipts, and transactions.

## Quick Start

```bash
go run verifier.go \
  --endpoint1 "http://3.143.4.79:9650/ext/bc/C/rpc" \
  --endpoint2 "http://3.144.46.123:9650/ext/bc/C/rpc"
```

The verifier starts from the latest block (or a specified height) and continuously monitors for new blocks, logging any discrepancies to an error file.

## Configuration Options

| Flag                      | Default                                     | Description                                    |
| ------------------------- | ------------------------------------------- | ---------------------------------------------- |
| `--endpoint1`             | _required_                                  | First RPC endpoint URL                         |
| `--endpoint2`             | _required_                                  | Second RPC endpoint URL                        |
| `--start-height`          | `-1` (latest)                               | Starting block height                          |
| `--log-file`              | `block_verification_errors_{timestamp}.log` | Error log file path                            |
| `--workers`               | `10`                                        | Number of concurrent workers                   |
| `--check-interval`        | `120`                                       | Seconds between new block checks               |
| `--max-transactions`      | `5`                                         | Max transactions to verify per block (0 = all) |
| `--max-blocks-per-second` | `100`                                       | Max blocks per worker per second (rate limit)  |
| `--finality-buffer`       | `10`                                        | Stay N blocks behind chain tip for finality    |
| `--progress-interval`     | `100`                                       | Log progress every N blocks                    |

**Transaction Verification:** The verifier checks ALL transaction hashes but only fetches detailed data for the first N transactions (default 5) to reduce RPC load. Use `--max-transactions 0` to verify all transaction details.

**Rate Limiting:** Each worker is rate-limited to prevent overwhelming endpoints. Default is 100 blocks/second per worker (1,000 blocks/sec total with 10 workers). This provides an upper bound of ~14,000 requests/second (1,000 blocks × 14 requests per block). The rate limiter only activates if blocks process faster than the limit, so it won't slow down normal operation.

## Running Directly

### Run the Verifier

```bash
go run verifier.go \
  --endpoint1 "http://node1:9650/ext/bc/C/rpc" \
  --endpoint2 "http://node2:9650/ext/bc/C/rpc" \
  --start-height 1000000 \
  --workers 20 \
  --max-blocks-per-second 50
```

### View Logs

**Operational logs** (stdout): Shows progress, status, and what's happening
**Error log file**: Contains only verification errors and discrepancies

```bash
# View error log
tail -f block_verification_errors_*.log

# Search for specific errors
grep "StateRoot" block_verification_errors_*.log
```

### Stop the Verifier

Press `Ctrl+C` to gracefully stop. The verifier will finish processing queued blocks and display a summary.

## Running as a Service

### Install Service

```bash
sudo ./install_service.sh \
  --instance-name "mainnet" \
  --endpoint1 "http://node1:9650/ext/bc/C/rpc" \
  --endpoint2 "http://node2:9650/ext/bc/C/rpc"
```

All verifier flags can be passed to `install_service.sh`. Multiple instances can run with different `--instance-name` values.

### Uninstall Service

```bash
sudo ./install_service.sh --instance-name mainnet --uninstall
```

This stops the service, disables auto-start, and removes the systemd service file. Error logs are preserved.

### Service Commands

```bash
# Start service
sudo systemctl start block-verifier-mainnet

# Stop service
sudo systemctl stop block-verifier-mainnet

# Restart service
sudo systemctl restart block-verifier-mainnet

# Check status
sudo systemctl status block-verifier-mainnet
```

### Update Running Service

To update the verifier after making code changes:

```bash
# 1. Stop the service
sudo systemctl stop block-verifier-mainnet

# 2. Build the updated verifier
cd /home/ubuntu/avalanchego/x/blockdb/verifier
go build -buildvcs=false -o block-verifier

# 3. Install the new binary
sudo cp block-verifier /usr/local/bin/block-verifier

# 4. Clean up build artifact
rm -f block-verifier

# 5. Restart the service
sudo systemctl start block-verifier-mainnet

# 6. Verify it's running
systemctl status block-verifier-mainnet
```

You can also check the logs to confirm the new version is working:

```bash
# View recent startup logs
sudo journalctl -u block-verifier-mainnet -n 20 --no-pager
```

### View Service Logs

**Operational logs** (journalctl):

```bash
# Live logs
sudo journalctl -u block-verifier-mainnet -f

# Last hour
sudo journalctl -u block-verifier-mainnet --since "1 hour ago"

# Search logs
sudo journalctl -u block-verifier-mainnet | grep "Block 12345"
```

**Error log file**:

```bash
# Default location: /var/log/block_verifier_{instance}_errors.log
tail -f /var/log/block_verifier_mainnet_errors.log
```

Note: Systemd journal retention is set to 60 days by the installer.

## Log Output

### Operational Logs (stdout/journalctl)

Shows verifier operations and progress:

```
[2025-11-05 11:09:43] === VERIFIER STARTED ===
Endpoint 1: http://node1:9650/ext/bc/C/rpc
Endpoint 2: http://node2:9650/ext/bc/C/rpc
Start height: 71467805 (latest)
Starting 10 worker threads for concurrent verification
Verification loop started (check interval: 120 seconds)
Queueing 150 new blocks (71467806 to 71467955)
Progress: 100 blocks verified, 0 discrepancies found (100.00% success rate)
Chain head at block 71467955, checking again in 120 seconds
```

### Error Log File

Contains only verification errors with block height and mismatch type:

```
[2025-11-05 11:32:15] Block 71467900: Verification failed: block data mismatch
[2025-11-05 11:35:22] Block 71468000: Verification failed: receipts mismatch
[2025-11-05 11:40:10] Block 71468500: Verification failed: transaction 0xabcd1234... mismatch

[2025-11-05 12:00:00] === VERIFICATION SUMMARY ===
Endpoint 1: http://node1:9650/ext/bc/C/rpc
Endpoint 2: http://node2:9650/ext/bc/C/rpc
Start Height: 71,467,805
Current Height: 71,469,000
Blocks Processed: 1,195
Discrepancies Found: 3
Session Duration: 28m15s
Blocks per Minute: 42.48
Success Rate: 99.75%
Log File: /var/log/block_verifier_mainnet_errors.log
============================
```

## Troubleshooting

### Service Won't Start

```bash
# Check status and recent errors
sudo systemctl status block-verifier-mainnet
sudo journalctl -u block-verifier-mainnet --since "5 minutes ago"

# Common causes:
# - Invalid endpoint URLs
# - Network connectivity issues
# - Permission errors on log file
```

### Connection Errors

```bash
# Test endpoint connectivity
curl -X POST -H "Content-Type: application/json" \
  --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
  http://node1:9650/ext/bc/C/rpc

# Check connection logs
sudo journalctl -u block-verifier-mainnet | grep "CONNECTION"
grep "CONNECTION" /path/to/error.log
```

### High Memory Usage

Reduce workers or increase check interval:

```bash
sudo ./install_service.sh --instance-name mainnet \
  --endpoint1 "..." --endpoint2 "..." \
  --workers 5 --check-interval 300
```

## What Gets Verified

The verifier compares **complete JSON responses** (normalized for consistent formatting) between both endpoints to ensure:

- **Block Data**: All fields including hash, parent hash, state root, receipts root, transactions root, nonce, miner, gas limit/used, timestamp, difficulty, size, extra data, logs bloom, uncles, and any other fields present in the response
- **Block Receipts**: Complete receipts data including status, gas used, contract address, logs bloom, all log entries with topics and data
- **Transactions**: All transaction fields including hash, type, from/to addresses, value, gas, gas price, nonce, input data, signatures (v, r, s), and any EIP-specific fields
- **Transaction Order**: Ensures transactions appear in identical order and hash values match

**Comprehensive Coverage**: By comparing normalized JSON responses, the verifier catches ALL fields returned by the RPC endpoints, not just explicitly defined fields. This ensures no discrepancies are missed, even if new fields are added to the RPC response format.

Any mismatch is logged with the block height and type of mismatch (block data, receipts, or specific transaction).
