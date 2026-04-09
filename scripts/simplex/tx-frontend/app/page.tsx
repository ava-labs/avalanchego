"use client";

import { useState, useCallback, useEffect, useRef } from "react";
import {
  JsonRpcProvider,
  Wallet,
  parseEther,
  formatEther,
  getAddress,
  sha256,
  toUtf8Bytes,
  type TransactionResponse,
} from "ethers";

const EWOQ_PRIVATE_KEY =
  "56289e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8027";
const POLL_INTERVAL = 2000;

function nodePrivateKey(index: number): string {
  return sha256(toUtf8Bytes(`simplex-node-${index}`));
}

interface NodeInfo {
  uri: string;
  address: string | null;
  nodeID: string | null;
  healthy: boolean;
  blockHeight: number | null;
  balance: string | null;
}

interface TxRecord {
  hash: string;
  from: string;
  to: string;
  amount: string;
  status: "pending" | "confirmed" | "failed";
  blockNumber?: number;
}

interface ChainInfo {
  name: string;
  chainId: string;
  rpcPath: string;
  subnetId?: string;
  vm: string;
  consensus?: string;
  conversionTx?: string;
  conversionId?: string;
  validatorCount?: number;
}

async function rpcCall(url: string, method: string, params?: Record<string, unknown>) {
  const res = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ jsonrpc: "2.0", id: 1, method, params: params ?? {} }),
  });
  return res.json();
}

interface PChainTx {
  type: string;
  txID: string;
  blockHeight: number;
  timestamp?: string;
}

function inferPChainTxType(unsignedTx: Record<string, unknown>): string {
  if ("validators" in unsignedTx && "chainID" in unsignedTx && "address" in unsignedTx) return "ConvertSubnetToL1";
  if ("chainName" in unsignedTx) return "CreateChain";
  if ("owner" in unsignedTx && !("validator" in unsignedTx)) return "CreateSubnet";
  if ("validator" in unsignedTx && "subnetID" in unsignedTx) return "AddSubnetValidator";
  if ("message" in unsignedTx && "balance" in unsignedTx) return "RegisterL1Validator";
  if ("message" in unsignedTx) return "SetL1ValidatorWeight";
  if ("validationID" in unsignedTx && "balance" in unsignedTx) return "IncreaseL1ValidatorBalance";
  if ("validationID" in unsignedTx) return "DisableL1Validator";
  if ("stake" in unsignedTx) return "AddValidator";
  return "Unknown";
}

async function fetchPChainBlocks(
  baseUri: string,
  fromHeight: number,
  toHeight: number
): Promise<PChainTx[]> {
  const txs: PChainTx[] = [];
  for (let h = fromHeight; h <= toHeight; h++) {
    try {
      const res = await rpcCall(`${baseUri}/ext/bc/P`, "platform.getBlockByHeight", {
        height: String(h),
        encoding: "json",
      });
      const block = res?.result?.block;
      if (!block) continue;

      const blockTxs = block.txs ?? [];
      for (const tx of blockTxs) {
        const txID = tx?.id ?? "";
        const unsignedTx = (tx?.unsignedTx ?? {}) as Record<string, unknown>;
        txs.push({
          type: inferPChainTxType(unsignedTx),
          txID,
          blockHeight: h,
          timestamp: block.time ? String(block.time) : undefined,
        });
      }
    } catch {
      // skip block
    }
  }
  return txs;
}

async function fetchNodeInfo(
  uri: string,
  address: string | null
): Promise<NodeInfo> {
  const base: NodeInfo = {
    uri,
    address,
    nodeID: null,
    healthy: false,
    blockHeight: null,
    balance: null,
  };
  try {
    const [idRes, healthRes] = await Promise.all([
      rpcCall(`${uri}/ext/info`, "info.getNodeID"),
      rpcCall(`${uri}/ext/health`, "health.health"),
    ]);
    base.nodeID = idRes?.result?.nodeID ?? null;
    base.healthy = healthRes?.result?.healthy ?? false;

    const provider = new JsonRpcProvider(`${uri}/ext/bc/C/rpc`);
    base.blockHeight = await provider.getBlockNumber();

    if (address) {
      const bal = await provider.getBalance(address);
      base.balance = formatEther(bal);
    }
  } catch {
    // node unreachable
  }
  return base;
}

export default function Home() {
  const [chains, setChains] = useState<ChainInfo[]>([]);
  const [selectedChain, setSelectedChain] = useState<ChainInfo | null>(null);
  const [nodeEntries, setNodeEntries] = useState<
    { uri: string; address: string | null }[]
  >([]);
  const [nodes, setNodes] = useState<NodeInfo[]>([]);
  const [connected, setConnected] = useState(false);
  const [activeNode, setActiveNode] = useState("");
  const [shouldAutoConnect, setShouldAutoConnect] = useState(false);
  const [fromNodeIndex, setFromNodeIndex] = useState<number | null>(null);
  const [to, setTo] = useState("");
  const [amount, setAmount] = useState("1");
  const [sentTxs, setSentTxs] = useState<TxRecord[]>([]);
  const [chainTxs, setChainTxs] = useState<TxRecord[]>([]);
  const [pChainTxs, setPChainTxs] = useState<PChainTx[]>([]);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState("");
  const [balance, setBalance] = useState<string | null>(null);
  const [senderAddress, setSenderAddress] = useState<string | null>(null);
  const [chainBlockHeight, setChainBlockHeight] = useState<number | null>(null);
  const pollingRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const lastScannedBlock = useRef<number>(0);
  const lastPChainBlock = useRef<number>(0);

  // Auto-load node entries and chains
  useEffect(() => {
    fetch("/nodes.json")
      .then((res) => res.json())
      .then((entries: ({ uri: string; address: string } | string)[]) => {
        if (entries.length > 0) {
          const parsed = entries.map((e) =>
            typeof e === "string" ? { uri: e, address: null } : e
          );
          setNodeEntries(parsed);
          setShouldAutoConnect(true);
        }
      })
      .catch(() => {});

    fetch("/chains.json")
      .then((res) => res.json())
      .then((data: ChainInfo[]) => {
        setChains(data);
        // Default to C-Chain
        const cChain = data.find((c) => c.chainId === "C");
        if (cChain) setSelectedChain(cChain);
      })
      .catch(() => {});
  }, []);

  // Reset tx history and block scanner when chain changes
  useEffect(() => {
    setChainTxs([]);
    setSentTxs([]);
    setPChainTxs([]);
    setChainBlockHeight(null);
    lastScannedBlock.current = 0;
    lastPChainBlock.current = 0;
  }, [selectedChain]);

  const getChainRpcUrl = useCallback(() => {
    if (!activeNode || !selectedChain) return null;
    return `${activeNode}${selectedChain.rpcPath}`;
  }, [activeNode, selectedChain]);

  const connect = useCallback(async () => {
    if (nodeEntries.length === 0) return;

    setError("");
    try {
      const infos = await Promise.all(
        nodeEntries.map((e) => fetchNodeInfo(e.uri, e.address))
      );
      setNodes(infos);

      const firstHealthy = infos.find((n) => n.healthy);
      const primary = firstHealthy?.uri ?? nodeEntries[0].uri;
      setActiveNode(primary);

      const provider = new JsonRpcProvider(`${primary}/ext/bc/C/rpc`);
      const wallet = new Wallet(EWOQ_PRIVATE_KEY, provider);
      setSenderAddress(wallet.address);
      const bal = await provider.getBalance(wallet.address);
      setBalance(formatEther(bal));
      setConnected(true);
    } catch (e: unknown) {
      setError(`Failed to connect: ${e instanceof Error ? e.message : e}`);
      setConnected(false);
    }
  }, [nodeEntries]);

  useEffect(() => {
    if (shouldAutoConnect && !connected) {
      connect();
      setShouldAutoConnect(false);
    }
  }, [shouldAutoConnect, connected, connect]);

  const isEvmChain =
    selectedChain && selectedChain.vm !== "platformvm";

  const pollNetwork = useCallback(async () => {
    if (!activeNode) return;
    try {
      // Refresh node statuses
      const infos = await Promise.all(
        nodeEntries.map((e) => fetchNodeInfo(e.uri, e.address))
      );
      setNodes(infos);

      // Sender balance (always from C-chain)
      const cProvider = new JsonRpcProvider(`${activeNode}/ext/bc/C/rpc`);
      const wallet = new Wallet(EWOQ_PRIVATE_KEY, cProvider);
      const bal = await cProvider.getBalance(wallet.address);
      setBalance(formatEther(bal));

      // Poll selected chain for txs (only EVM chains)
      const rpcUrl = getChainRpcUrl();
      if (rpcUrl && isEvmChain) {
        const provider = new JsonRpcProvider(rpcUrl);
        const height = await provider.getBlockNumber();
        setChainBlockHeight(height);

        const startBlock = Math.max(
          lastScannedBlock.current + 1,
          Math.max(0, height - 20)
        );
        const newTxs: TxRecord[] = [];
        for (let i = startBlock; i <= height; i++) {
          const block = await provider.getBlock(i, true);
          if (!block) continue;
          const txs: TransactionResponse[] =
            block.prefetchedTransactions ?? [];
          for (const tx of txs) {
            newTxs.push({
              hash: tx.hash,
              from: tx.from,
              to: tx.to ?? "Contract Creation",
              amount: formatEther(tx.value),
              status: "confirmed",
              blockNumber: i,
            });
          }
        }
        if (newTxs.length > 0) {
          setChainTxs((prev) => [...newTxs, ...prev].slice(0, 50));
        }
        lastScannedBlock.current = height;
      }

      // Poll P-Chain if selected
      if (selectedChain?.vm === "platformvm") {
        const heightRes = await rpcCall(`${activeNode}/ext/bc/P`, "platform.getHeight");
        const pHeight = Number(heightRes?.result?.height ?? 0);
        setChainBlockHeight(pHeight);

        const startH = Math.max(
          lastPChainBlock.current + 1,
          Math.max(0, pHeight - 20)
        );
        if (startH <= pHeight) {
          const newPTxs = await fetchPChainBlocks(activeNode, startH, pHeight);
          if (newPTxs.length > 0) {
            setPChainTxs((prev) => [...newPTxs, ...prev].slice(0, 50));
          }
          lastPChainBlock.current = pHeight;
        }
      }
    } catch {
      // silently fail polls
    }
  }, [activeNode, nodeEntries, getChainRpcUrl, isEvmChain, selectedChain]);

  useEffect(() => {
    if (!connected) return;
    pollingRef.current = setInterval(pollNetwork, POLL_INTERVAL);
    return () => {
      if (pollingRef.current) clearInterval(pollingRef.current);
    };
  }, [connected, pollNetwork]);

  const sendTx = async () => {
    const rpcUrl = getChainRpcUrl();
    if (!rpcUrl || !to || !amount || fromNodeIndex === null) return;
    setSending(true);
    setError("");
    try {
      const provider = new JsonRpcProvider(rpcUrl);
      const privKey = nodePrivateKey(fromNodeIndex);
      const wallet = new Wallet(privKey, provider);
      const tx = await wallet.sendTransaction({
        to: getAddress(to),
        value: parseEther(amount),
      });
      const record: TxRecord = {
        hash: tx.hash,
        from: wallet.address,
        to,
        amount,
        status: "pending",
      };
      setSentTxs((prev) => [record, ...prev]);
      const receipt = await tx.wait();
      setSentTxs((prev) =>
        prev.map((t) =>
          t.hash === tx.hash
            ? {
                ...t,
                status: receipt?.status === 1 ? "confirmed" : "failed",
                blockNumber: receipt?.blockNumber,
              }
            : t
        )
      );
    } catch (e: unknown) {
      setError(`TX failed: ${e instanceof Error ? e.message : e}`);
    } finally {
      setSending(false);
    }
  };

  const mono = "font-[family-name:var(--font-geist-mono)]";
  const card =
    "p-4 bg-zinc-100 dark:bg-zinc-900 rounded-lg border border-zinc-200 dark:border-zinc-800";
  const inputCls =
    "w-full bg-white dark:bg-zinc-800 border border-zinc-300 dark:border-zinc-700 rounded px-3 py-2 text-sm";

  return (
    <div className="max-w-5xl mx-auto p-8 font-[family-name:var(--font-geist-sans)]">
      {/* Nodes Overview */}
      {nodes.length > 0 && (
        <div className="mb-6">
          <div className="flex gap-3 overflow-x-auto">
            {nodes.map((node, i) => (
              <div
                key={node.uri}
                onClick={() => node.healthy && setActiveNode(node.uri)}
                className={`flex-shrink-0 w-48 p-4 rounded-lg border cursor-pointer transition-colors ${
                  node.uri === activeNode
                    ? "bg-blue-50 dark:bg-blue-950 border-blue-300 dark:border-blue-800"
                    : "bg-zinc-100 dark:bg-zinc-900 border-zinc-200 dark:border-zinc-800 hover:border-zinc-400 dark:hover:border-zinc-600"
                }`}
              >
                <div className="flex items-center justify-between mb-2">
                  <span className="text-sm font-semibold">Node {i + 1}</span>
                  <span
                    className={`w-2.5 h-2.5 rounded-full ${
                      node.healthy ? "bg-green-500" : "bg-red-500"
                    }`}
                  />
                </div>
                <div
                  className={`${mono} text-xs text-zinc-500 dark:text-zinc-400 truncate mb-1`}
                >
                  {node.nodeID ? `${node.nodeID.slice(0, 18)}...` : "unknown"}
                </div>
                <div className="text-xs text-zinc-500 dark:text-zinc-400">
                  {node.uri.replace("http://127.0.0.1:", ":")}
                </div>
                {node.address && (
                  <div
                    className={`${mono} text-xs text-zinc-500 dark:text-zinc-400 truncate mb-1`}
                  >
                    {node.address.slice(0, 8)}...{node.address.slice(-6)}
                  </div>
                )}
                {node.blockHeight !== null && (
                  <div className="text-xs text-zinc-500 dark:text-zinc-400">
                    Block{" "}
                    <span
                      className={`${mono} font-medium text-foreground`}
                    >
                      {node.blockHeight}
                    </span>
                  </div>
                )}
                {node.balance !== null && (
                  <div className="text-xs text-zinc-500 dark:text-zinc-400">
                    Balance{" "}
                    <span
                      className={`${mono} font-medium text-foreground`}
                    >
                      {parseFloat(node.balance).toFixed(2)} AVAX
                    </span>
                  </div>
                )}
                {node.uri === activeNode && (
                  <div className="mt-2 text-xs font-medium text-blue-600 dark:text-blue-400">
                    active
                  </div>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Chain pills */}
      {chains.length > 0 && (
        <div className="flex items-center gap-2 mb-6">
          <span className="text-sm text-zinc-500 dark:text-zinc-400 mr-1">
            Chains:
          </span>
          {chains.map((c) => (
            <button
              key={c.chainId}
              onClick={() => setSelectedChain(c)}
              className={`px-3 py-1.5 text-sm rounded-full border cursor-pointer transition-colors ${
                selectedChain?.chainId === c.chainId
                  ? "bg-blue-600 text-white border-blue-600"
                  : "border-zinc-300 dark:border-zinc-700 hover:bg-zinc-200 dark:hover:bg-zinc-800"
              }`}
            >
              {c.name}
            {c.consensus && (
              <span className={`ml-1.5 text-xs px-1.5 py-0.5 rounded ${
                c.consensus === "simplex"
                  ? "bg-purple-100 text-purple-700 dark:bg-purple-900 dark:text-purple-300"
                  : "bg-zinc-200 text-zinc-600 dark:bg-zinc-700 dark:text-zinc-300"
              }`}>
                {c.consensus}
              </span>
            )}
            </button>
          ))}
        </div>
      )}

      {/* Selected chain info */}
      {selectedChain && (selectedChain.subnetId || selectedChain.conversionTx) && (
        <div className={`mb-6 ${card}`}>
          <div className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
            {selectedChain.subnetId && (
              <>
                <span className="text-zinc-500 dark:text-zinc-400">Subnet ID</span>
                <span className={`${mono} text-xs`}>{selectedChain.subnetId}</span>
              </>
            )}
            <span className="text-zinc-500 dark:text-zinc-400">Chain ID</span>
            <span className={`${mono} text-xs`}>{selectedChain.chainId}</span>
            {selectedChain.conversionTx && (
              <>
                <span className="text-zinc-500 dark:text-zinc-400">Conversion TX</span>
                <span className={`${mono} text-xs`}>{selectedChain.conversionTx}</span>
              </>
            )}
            {selectedChain.conversionId && (
              <>
                <span className="text-zinc-500 dark:text-zinc-400">Conversion ID</span>
                <span className={`${mono} text-xs`}>{selectedChain.conversionId}</span>
              </>
            )}
            {selectedChain.validatorCount && (
              <>
                <span className="text-zinc-500 dark:text-zinc-400">Validators</span>
                <span>{selectedChain.validatorCount} nodes</span>
              </>
            )}
            <span className="text-zinc-500 dark:text-zinc-400">VM</span>
            <span>{selectedChain.vm}</span>
            {selectedChain.consensus && (
              <>
                <span className="text-zinc-500 dark:text-zinc-400">Consensus</span>
                <span className={`font-medium ${
                  selectedChain.consensus === "simplex"
                    ? "text-purple-600 dark:text-purple-400"
                    : ""
                }`}>{selectedChain.consensus}</span>
              </>
            )}
            {chainBlockHeight !== null && (
              <>
                <span className="text-zinc-500 dark:text-zinc-400">Block Height</span>
                <span className={`${mono} font-medium`}>{chainBlockHeight}</span>
              </>
            )}
          </div>
        </div>
      )}

      {/* Send TX - only for EVM chains */}
      {isEvmChain && (
        <div className={`mb-6 ${card}`}>
          <h2 className="text-lg font-semibold mb-3">Send Transaction</h2>
          <div className="space-y-3">
            <div>
              <label className="block text-sm text-zinc-500 dark:text-zinc-400 mb-1">
                From
              </label>
              <div className="flex flex-wrap gap-2">
                {nodes
                  .filter((n) => n.address)
                  .map((_, i) => (
                    <button
                      key={i}
                      onClick={() => setFromNodeIndex(i)}
                      className={`px-3 py-1 text-xs rounded border cursor-pointer transition-colors ${
                        fromNodeIndex === i
                          ? "bg-green-600 text-white border-green-600"
                          : "bg-white dark:bg-zinc-800 border-zinc-300 dark:border-zinc-700 hover:border-green-400"
                      }`}
                    >
                      Node {i + 1}
                    </button>
                  ))}
              </div>
            </div>
            <div>
              <label className="block text-sm text-zinc-500 dark:text-zinc-400 mb-1">
                To
              </label>
              <div className="flex flex-wrap gap-2 mb-2">
                {nodes
                  .filter((n) => n.address)
                  .map((node, i) => (
                    <button
                      key={i}
                      onClick={() => setTo(node.address!)}
                      className={`px-3 py-1 text-xs rounded border cursor-pointer transition-colors ${
                        to === node.address
                          ? "bg-blue-600 text-white border-blue-600"
                          : "bg-white dark:bg-zinc-800 border-zinc-300 dark:border-zinc-700 hover:border-blue-400"
                      }`}
                    >
                      Node {i + 1}
                    </button>
                  ))}
              </div>
              <input
                className={`${inputCls} ${mono}`}
                placeholder="0x... or select a node above"
                value={to}
                onChange={(e) => setTo(e.target.value)}
              />
            </div>
            <div>
              <label className="block text-sm text-zinc-500 dark:text-zinc-400 mb-1">
                Amount (AVAX)
              </label>
              <input
                className={inputCls}
                type="number"
                step="0.01"
                value={amount}
                onChange={(e) => setAmount(e.target.value)}
              />
            </div>
            <button
              onClick={sendTx}
              disabled={
                sending || !to || !amount || !connected || fromNodeIndex === null
              }
              className="w-full py-2 bg-green-600 hover:bg-green-500 disabled:bg-zinc-300 dark:disabled:bg-zinc-700 disabled:text-zinc-500 text-white rounded font-medium cursor-pointer"
            >
              {sending ? "Sending..." : "Send TX"}
            </button>
          </div>
        </div>
      )}

      {error && (
        <div className="mb-4 p-3 bg-red-100 dark:bg-red-900/50 border border-red-300 dark:border-red-800 rounded text-red-700 dark:text-red-300 text-sm">
          {error}
        </div>
      )}

      {/* Sent TX History */}
      {sentTxs.length > 0 && (
        <div className={`mb-6 ${card}`}>
          <h2 className="text-lg font-semibold mb-3">Sent Transactions</h2>
          <div className="space-y-2">
            {sentTxs.map((tx) => (
              <TxRow key={tx.hash} tx={tx} mono={mono} />
            ))}
          </div>
        </div>
      )}

      {/* On-Chain TX History (EVM chains) */}
      {chainTxs.length > 0 && isEvmChain && (
        <div className={card}>
          <h2 className="text-lg font-semibold mb-3">
            On-Chain Transactions
            {selectedChain && (
              <span className="text-sm font-normal text-zinc-500 dark:text-zinc-400 ml-2">
                ({selectedChain.name})
              </span>
            )}
          </h2>
          <div className="space-y-2">
            {[...chainTxs]
              .sort((a, b) => (b.blockNumber ?? 0) - (a.blockNumber ?? 0))
              .map((tx) => (
                <TxRow key={tx.hash} tx={tx} mono={mono} />
              ))}
          </div>
        </div>
      )}

      {/* P-Chain TX History */}
      {selectedChain?.vm === "platformvm" && pChainTxs.length > 0 && (
        <div className={card}>
          <h2 className="text-lg font-semibold mb-3">
            P-Chain Transactions
          </h2>
          <div className="space-y-2">
            {[...pChainTxs]
              .sort((a, b) => b.blockHeight - a.blockHeight)
              .map((tx, i) => (
                <div
                  key={tx.txID || i}
                  className="p-3 bg-white dark:bg-zinc-800 rounded border border-zinc-200 dark:border-zinc-700"
                >
                  <div className="flex justify-between items-center mb-1">
                    <span className={`${mono} text-xs text-zinc-500 dark:text-zinc-400`}>
                      {tx.txID ? `${tx.txID.slice(0, 18)}...${tx.txID.slice(-8)}` : "—"}
                    </span>
                    <span className="text-xs font-medium px-2 py-0.5 rounded bg-purple-100 text-purple-700 dark:bg-purple-900 dark:text-purple-300">
                      {tx.type}
                    </span>
                  </div>
                  <div className="text-xs text-zinc-500">
                    Block #{tx.blockHeight}
                  </div>
                </div>
              ))}
          </div>
        </div>
      )}
    </div>
  );
}

function TxRow({ tx, mono }: { tx: TxRecord; mono: string }) {
  return (
    <div className="p-3 bg-white dark:bg-zinc-800 rounded border border-zinc-200 dark:border-zinc-700">
      <div className="flex justify-between items-center mb-1">
        <span className={`${mono} text-xs text-zinc-500 dark:text-zinc-400`}>
          {tx.hash.slice(0, 18)}...{tx.hash.slice(-8)}
        </span>
        <span
          className={`text-xs font-medium px-2 py-0.5 rounded ${
            tx.status === "confirmed"
              ? "bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300"
              : tx.status === "failed"
                ? "bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300"
                : "bg-yellow-100 text-yellow-700 dark:bg-yellow-900 dark:text-yellow-300"
          }`}
        >
          {tx.status}
        </span>
      </div>
      <div className="text-sm">
        <span className={`${mono} text-xs`}>{tx.from.slice(0, 10)}...</span>
        {" → "}
        <span className={`${mono} text-xs`}>
          {tx.to.length > 20 ? `${tx.to.slice(0, 10)}...` : tx.to}
        </span>
        <span className="ml-2 font-medium">
          {parseFloat(tx.amount).toFixed(2)} AVAX
        </span>
      </div>
      {tx.blockNumber && (
        <div className="text-xs text-zinc-500">Block #{tx.blockNumber}</div>
      )}
    </div>
  );
}
