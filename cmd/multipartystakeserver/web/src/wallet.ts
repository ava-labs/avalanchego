import { secp256k1 } from '@noble/curves/secp256k1'
import { sha256 } from '@noble/hashes/sha256'
import { ripemd160 } from '@noble/hashes/ripemd160'
import { bech32 } from 'bech32'

// Extend Window to include Core Wallet's injected provider
declare global {
  interface Window {
    avalanche?: {
      request: <T = unknown>(args: { method: string; params?: unknown }) => Promise<T>
    }
  }
}

export function hasCoreWallet(): boolean {
  return typeof window !== 'undefined' && !!window.avalanche
}

/**
 * Derive the P-Chain address from Core Wallet's XP public key.
 * Steps: get uncompressed pubkey → compress → SHA-256 → RIPEMD-160 → bech32
 */
export async function getPChainAddress(isTestnet: boolean): Promise<string> {
  if (!window.avalanche) throw new Error('Core Wallet not found')

  const { xp } = await window.avalanche.request<{ xp: string; evm: string }>({
    method: 'avalanche_getAccountPubKey',
    params: [],
  })

  // Strip 0x prefix if present
  const pubKeyHex = xp.startsWith('0x') ? xp.slice(2) : xp

  // Compress the uncompressed secp256k1 public key (65 bytes → 33 bytes)
  const point = secp256k1.ProjectivePoint.fromHex(pubKeyHex)
  const compressed = point.toRawBytes(true)

  // SHA-256 → RIPEMD-160 = 20-byte address
  const hash = ripemd160(sha256(compressed))

  // Bech32 encode with HRP
  const hrp = isTestnet ? 'fuji' : 'avax'
  const words = bech32.toWords(hash)
  const encoded = bech32.encode(hrp, words)

  return `P-${encoded}`
}

/**
 * Ensure Core Wallet is in the correct network mode (testnet/mainnet).
 * Returns the previous chainId hex if a switch was needed, null otherwise.
 */
export async function ensureCoreNetworkMode(isTestnet: boolean): Promise<string | null> {
  if (!window.avalanche) throw new Error('Core Wallet not found')

  const chain = await window.avalanche.request<{
    chainId: string
    isTestnet: boolean
  }>({
    method: 'wallet_getEthereumChain',
    params: [],
  })

  if (chain.isTestnet === isTestnet) return null

  // Switch to C-Chain to toggle Core's mainnet/testnet mode
  const targetChainId = isTestnet ? '0xa869' : '0xa86a' // 43113 / 43114
  await window.avalanche.request({
    method: 'wallet_switchEthereumChain',
    params: [{ chainId: targetChainId }],
  })

  return chain.chainId
}

/**
 * Sign an unsigned P-Chain tx with Core Wallet. Returns the signed tx hex.
 * The frontend never submits the tx — only the server does.
 */
export async function signTransaction(unsignedTxHex: string, isTestnet: boolean): Promise<string> {
  if (!window.avalanche) throw new Error('Core Wallet not found')

  await ensureCoreNetworkMode(isTestnet)

  const hex = unsignedTxHex.startsWith('0x') ? unsignedTxHex : `0x${unsignedTxHex}`

  const result = await window.avalanche.request<{ signedTransactionHex: string }>({
    method: 'avalanche_signTransaction',
    params: { transactionHex: hex, chainAlias: 'P' },
  })

  return result.signedTransactionHex
}
