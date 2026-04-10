# Simplex Local Network

Run a local 5-node AvalancheGo network with a Simplex-consensus L1 chain and a web dashboard for sending transactions.

## Prerequisites

- Go toolchain
- Node.js / npm
- Python 3

## Quick Start

### 1. Clean up any previous network

```bash
./scripts/simplex/clean.sh
```

Kills leftover AvalancheGo processes, removes tmpnet data, and deletes generated files.

### 2. Build AvalancheGo

```bash
./scripts/build.sh
```

### 3. Build the subnet-evm plugin

```bash
mkdir -p ~/.avalanchego/plugins
go build -o ~/.avalanchego/plugins/srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy ./graft/subnet-evm/plugin/
```

Compiles the subnet-evm binary and installs it as a plugin. The plugin ID (`srEX...`) is the VM ID that the L1 chain will reference.

Note: If running `start-network` you see `failed to register VM ... RPCChainVM protocol version mismatch between AvalancheGo and Virtual Machine plugin`, delete the old plugins with `rm -rf ~/.avalanchego/plugins` and re-run this step.

### 4. Start the network

```bash
./scripts/run_tmpnetctl.sh start-network --node-count=5 --avalanchego-path=./build/avalanchego
```

Starts a 5-node local network. Nodes get dynamic ports assigned automatically.

### 5. Create the L1

```bash
./scripts/simplex/create_l1.sh
```

Issues three P-Chain transactions (CreateSubnet, CreateChain, ConvertSubnetToL1), injects BLS keys into the Simplex config, restarts the network, and writes chain metadata to `tx-frontend/public/chains.json`.

To use Snowball consensus instead of Simplex:

```bash
./scripts/simplex/create_l1.sh ./scripts/simplex/config/subnet_config.json
```

### 6. Fund accounts

```bash
./scripts/simplex/fund_nodes.sh
```

Syncs node URIs from the tmpnet directory, generates a deterministic address per node, and funds each with 100 AVAX from the pre-funded ewoq key on both the C-Chain and L1 chain. Writes node info to `tx-frontend/public/nodes.json`.

### 7. Start the dashboard

```bash
cd scripts/simplex/tx-frontend
npm install  # first time only
npm run dev
```

Open `http://localhost:3000`.

### 8. Stop or clean

```bash
# Stop the network (keeps data):
./scripts/simplex/stop_network.sh

# Full cleanup (kills processes, removes tmpnet data, deletes generated files):
./scripts/simplex/clean.sh
```

## Verifying

Check node health:

```bash
./scripts/simplex/check_network.sh
```

## Sending a test transaction

`send_tx` is a Go tool that sends 1 AVAX on the L1 chain using the pre-funded ewoq key. It connects to the first node's RPC endpoint, sends the transaction, and waits up to 30 seconds for it to be mined.

```bash
go run ./scripts/simplex/send_tx/ \
  --nodes=scripts/simplex/tx-frontend/public/nodes.json \
  --chains=scripts/simplex/tx-frontend/public/chains.json
```

It will print the chain ID, ewoq balance, current block number, and the transaction hash once confirmed.

## Dashboard Features

- **Node cards** -- health status, block height, balance, and NodeID for each node. Click a card to route API calls through that node.
- **Chain selector** -- switch between P-Chain, C-Chain, and L1 chains.
- **P-Chain view** -- shows P-Chain blocks with transaction type badges (CreateSubnet, CreateChain, ConvertSubnetToL1, etc.).
- **Send transactions** -- pick a sender and receiver node, enter an amount, and send AVAX on any EVM chain.
- **Live updates** -- balances, block heights, and transaction history refresh automatically.

## File Structure

```
scripts/simplex/
  clean.sh                       # Kill processes, remove tmpnet data
  stop_network.sh                # Stop the network
  check_network.sh               # Health-check all nodes
  fund_nodes.sh                  # Sync node URIs + fund accounts
  create_l1.sh                   # Create L1 (subnet + chain + convert)
  create_l1/main.go              # Go tool for L1 creation
  send_tx/main.go                # Go tool for test transactions
  config/
    genesis.json                 # Subnet-EVM chain genesis
    subnet_config_simplex.json   # Simplex consensus parameters (default)
    subnet_config.json           # Snowball consensus parameters (alternative)
  tx-frontend/                   # Next.js dashboard
    public/
      nodes.json                 # Generated: node URIs + addresses
      chains.json                # Generated: chain metadata
    app/
      page.tsx                   # Dashboard UI
```

## Configuration

- **`config/subnet_config_simplex.json`** -- Simplex consensus parameters (used by default).
- **`config/subnet_config.json`** -- Snowball consensus parameters (pass as argument to `create_l1.sh` to use).
- **`config/genesis.json`** -- Subnet-EVM genesis. Pre-funds the ewoq address (`0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC`).
- Node accounts use deterministic keys derived from `sha256("simplex-node-{i}")`.
