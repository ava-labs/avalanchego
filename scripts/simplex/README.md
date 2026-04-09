# Simplex Local Network

Scripts for running a local 5-node AvalancheGo network with an L1 chain, plus a web dashboard for interacting with it.

## Prerequisites

- Go toolchain
- Node.js / npm
- AvalancheGo source (this repo)

## Quick Start

### 1. Start the network

```bash
./scripts/simplex/start_network.sh
```

Starts a 5-node local network using `tmpnetctl`. Nodes get dynamic ports assigned automatically.

### 2. Set environment and sync node info

```bash
source ~/.tmpnet/networks/latest/network.env
./scripts/simplex/sync_nodes.sh
```

Writes node URIs to `tx-frontend/public/nodes.json` so the frontend can discover them.

### 3. Fund node accounts

```bash
./scripts/simplex/fund_nodes.sh
```

Generates a deterministic C-Chain address per node and funds each with 100 AVAX from the pre-funded ewoq key. Updates `nodes.json` with addresses.

### 4. Create an L1

```bash
./scripts/simplex/create_l1.sh
```

This script:
1. Builds the subnet-evm plugin and installs it to all nodes
2. Creates a subnet on the P-Chain
3. Creates a chain using subnet-evm with the genesis in `genesis.json`
4. Converts the subnet to an L1 with all 5 nodes as validators
5. Updates node configs to track the new subnet
6. Restarts the network
7. Writes chain info to `tx-frontend/public/chains.json`

### 5. Re-sync and fund the L1

After restart, ports change and the L1 chain needs funding:

```bash
source ~/.tmpnet/networks/latest/network.env
./scripts/simplex/sync_nodes.sh
./scripts/simplex/fund_nodes.sh
```

This funds node addresses on both the C-Chain and the L1 chain.

### 6. Start the frontend

```bash
cd scripts/simplex/tx-frontend
npm install  # first time only
npm run dev
```

Open `http://localhost:3000`.

### 7. Stop the network

```bash
source ~/.tmpnet/networks/latest/network.env
./scripts/simplex/stop_network.sh
```

## Frontend Features

- **Node cards** — shows all 5 nodes with health status, block height, balance, and NodeID. Click to switch which node handles API calls.
- **Chain pills** — switch between P-Chain, C-Chain, and L1 chains. Each shows its own transactions.
- **P-Chain view** — displays P-Chain transactions (CreateSubnet, CreateChain, ConvertSubnetToL1, etc.) with type badges.
- **Send transactions** — pick a From and To node, enter an amount, and send AVAX between node accounts on any EVM chain.
- **Transaction history** — live-updating sent and on-chain transactions, sorted by block number.

## File Structure

```
scripts/simplex/
  start_network.sh       # Start 5-node network
  stop_network.sh        # Stop network
  check_network.sh       # Health check all nodes
  sync_nodes.sh          # Write node URIs to nodes.json
  fund_nodes.sh          # Fund node accounts on all EVM chains
  create_l1.sh           # Create L1 (subnet + chain + conversion)
  create_l1/main.go      # Go tool for L1 creation
  subnet_config.json     # Consensus parameters (snowball, swappable to simplex)
  genesis.json           # Subnet-EVM chain genesis
  tx-frontend/           # Next.js dashboard app
    public/
      nodes.json         # Auto-generated node URIs + addresses
      chains.json        # Auto-generated chain info
    app/
      page.tsx           # Main dashboard page
```

## Configuration

- **`subnet_config.json`** — Snowball consensus parameters. Will be swapped for Simplex parameters later.
- **`genesis.json`** — Subnet-EVM genesis. Pre-funds the ewoq address (`0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC`).
- Node accounts use deterministic keys derived from `sha256("simplex-node-{i}")`.
