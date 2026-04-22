import { useState } from 'react'
import { hasCoreWallet, getPChainAddress } from '../wallet'
import { deriveAddress } from '../api'

export type ConnectMode = 'wallet' | 'key'

interface Props {
  networkID: number
  isTestnet: boolean
  address: string | null
  privateKey: string | null
  onConnect: (address: string, mode: ConnectMode, privateKey?: string) => void
  onDisconnect?: () => void
}

export function WalletConnect({ networkID, isTestnet, address, privateKey, onConnect, onDisconnect }: Props) {
  const [mode, setMode] = useState<ConnectMode>(hasCoreWallet() ? 'wallet' : 'key')
  const [keyInput, setKeyInput] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [connecting, setConnecting] = useState(false)

  if (address) {
    return (
      <div className="wallet-connect connected">
        <span className="badge badge-success">
          {privateKey ? 'Dev Key' : 'Wallet'}
        </span>
        <code>{address}</code>
        {onDisconnect && (
          <button className="btn-small" onClick={onDisconnect}>
            Switch
          </button>
        )}
      </div>
    )
  }

  const connectWallet = async () => {
    setError(null)
    setConnecting(true)
    try {
      const addr = await getPChainAddress(isTestnet)
      onConnect(addr, 'wallet')
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to connect')
    } finally {
      setConnecting(false)
    }
  }

  const connectKey = async () => {
    setError(null)
    if (!keyInput.trim()) {
      setError('Enter a private key')
      return
    }
    setConnecting(true)
    try {
      const { address: addr } = await deriveAddress(keyInput.trim(), networkID)
      onConnect(addr, 'key', keyInput.trim())
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Invalid private key')
    } finally {
      setConnecting(false)
    }
  }

  return (
    <div className="wallet-connect">
      <div className="mode-tabs">
        <button
          className={mode === 'wallet' ? 'tab active' : 'tab'}
          onClick={() => setMode('wallet')}
        >
          Core Wallet
        </button>
        <button
          className={mode === 'key' ? 'tab active' : 'tab'}
          onClick={() => setMode('key')}
        >
          Private Key (Dev)
        </button>
      </div>

      {mode === 'wallet' && (
        <>
          {!hasCoreWallet() ? (
            <p className="error">
              Core Wallet not detected.{' '}
              <a href="https://core.app" target="_blank" rel="noopener noreferrer">
                Install Core
              </a>
            </p>
          ) : (
            <button onClick={connectWallet} disabled={connecting}>
              {connecting ? 'Connecting...' : 'Connect Core Wallet'}
            </button>
          )}
        </>
      )}

      {mode === 'key' && (
        <div className="key-input-row">
          <input
            type="text"
            placeholder="PrivateKey-..."
            value={keyInput}
            onChange={(e) => setKeyInput(e.target.value)}
          />
          <button onClick={connectKey} disabled={connecting}>
            {connecting ? 'Deriving...' : 'Connect'}
          </button>
        </div>
      )}

      {error && <p className="error">{error}</p>}
    </div>
  )
}
