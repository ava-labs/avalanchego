import { useState, useEffect, useCallback, useRef } from 'react'
import type { GetPartyResponse } from '../types'
import {
  getParty,
  signValidatorWithWallet,
  signValidatorWithKey,
  signSplitWithWallet,
  signSplitWithKey,
} from '../api'
import { signTransaction } from '../wallet'
import { WalletConnect, type ConnectMode } from '../components/WalletConnect'
import { SigningProgress } from '../components/SigningProgress'
import { SplitView } from '../components/SplitView'

const NETWORK_ID = 88888 // tmpnet; change to 5 for Fuji
const IS_TESTNET = true
const POLL_INTERVAL = 5000

interface Props {
  partyId: string
}

export function JoinParty({ partyId }: Props) {
  const [walletAddr, setWalletAddr] = useState<string | null>(null)
  const [connectMode, setConnectMode] = useState<ConnectMode>('wallet')
  const [privateKey, setPrivateKey] = useState<string | null>(null)
  const [party, setParty] = useState<GetPartyResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [signing, setSigning] = useState(false)
  const [signingLabel, setSigningLabel] = useState('')
  const pollRef = useRef<ReturnType<typeof setInterval>>(undefined)

  const handleConnect = (address: string, mode: ConnectMode, key?: string) => {
    setWalletAddr(address)
    setConnectMode(mode)
    if (key) setPrivateKey(key)
  }

  const handleDisconnect = () => {
    setWalletAddr(null)
    setPrivateKey(null)
    setConnectMode('wallet')
  }

  // Fetch party state
  const fetchParty = useCallback(async () => {
    try {
      const data = await getParty(partyId)
      setParty(data)
      setError(null)
      return data
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to fetch party')
      return null
    } finally {
      setLoading(false)
    }
  }, [partyId])

  // Initial fetch
  useEffect(() => {
    fetchParty()
  }, [fetchParty])

  // Polling: poll when waiting for other signers
  useEffect(() => {
    if (!party || !walletAddr) return

    const shouldPoll = (() => {
      if (party.state === 'complete') return false
      if (party.state === 'awaiting_validation_end') return true
      if (party.state === 'awaiting_validator_signatures') {
        return party.signaturesReceived.includes(walletAddr)
      }
      if (party.state === 'awaiting_split_signatures') {
        return party.splitSignaturesReceived.includes(walletAddr)
      }
      return false
    })()

    if (shouldPoll) {
      pollRef.current = setInterval(fetchParty, POLL_INTERVAL)
    }
    return () => clearInterval(pollRef.current)
  }, [party, walletAddr, fetchParty])

  const isParticipant = party?.signaturesNeeded.includes(walletAddr ?? '')
  const hasSignedValidator = party?.signaturesReceived.includes(walletAddr ?? '')
  const hasSignedSplit = party?.splitSignaturesReceived.includes(walletAddr ?? '')

  const handleSignValidator = async () => {
    if (!party || !walletAddr) return
    setError(null)
    setSigning(true)

    try {
      if (connectMode === 'key' && privateKey) {
        setSigningLabel('Signing with private key...')
        await signValidatorWithKey(partyId, privateKey)
      } else {
        setSigningLabel('Signing with Core Wallet...')
        const signedHex = await signTransaction(party.prepareOutput.unsignedTxHex, IS_TESTNET)
        setSigningLabel('Submitting signature...')
        await signValidatorWithWallet(partyId, walletAddr, signedHex)
      }
      await fetchParty()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Signing failed')
    } finally {
      setSigning(false)
      setSigningLabel('')
    }
  }

  const handleSignSplit = async () => {
    if (!party?.splitOutput || !walletAddr) return
    setError(null)
    setSigning(true)

    try {
      if (connectMode === 'key' && privateKey) {
        setSigningLabel('Signing with private key...')
        await signSplitWithKey(partyId, privateKey)
      } else {
        setSigningLabel('Signing with Core Wallet...')
        const signedHex = await signTransaction(party.splitOutput.unsignedTxHex, IS_TESTNET)
        setSigningLabel('Submitting signature...')
        await signSplitWithWallet(partyId, walletAddr, signedHex)
      }
      await fetchParty()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Signing failed')
    } finally {
      setSigning(false)
      setSigningLabel('')
    }
  }

  if (loading) {
    return (
      <div className="container">
        <p>Loading party...</p>
      </div>
    )
  }

  if (!party) {
    return (
      <div className="container">
        <h1>Party Not Found</h1>
        {error && <p className="error">{error}</p>}
      </div>
    )
  }

  const explorerBase = IS_TESTNET
    ? 'https://subnets-test.avax.network/p-chain/tx'
    : 'https://subnets.avax.network/p-chain/tx'

  return (
    <div className="container">
      <h1>Multi-Party Staking</h1>
      <p className="party-id">
        Party: <code>{partyId}</code>
      </p>

      <WalletConnect
        networkID={NETWORK_ID}
        isTestnet={IS_TESTNET}
        address={walletAddr}
        privateKey={privateKey}
        onConnect={handleConnect}
        onDisconnect={handleDisconnect}
      />

      {walletAddr && !isParticipant && (
        <p className="error">Your wallet is not a participant in this party.</p>
      )}

      {error && <p className="error">{error}</p>}

      {/* Phase 1: Validator Signing */}
      {party.state === 'awaiting_validator_signatures' && (
        <section>
          <SigningProgress
            label="Validator Transaction"
            needed={party.signaturesNeeded}
            received={party.signaturesReceived}
          />

          {walletAddr && isParticipant && !hasSignedValidator && (
            <div className="sign-action">
              <button onClick={handleSignValidator} disabled={signing} className="btn-primary">
                {signing ? signingLabel : 'Sign Validator Transaction'}
              </button>
            </div>
          )}

          {hasSignedValidator && (
            <p className="status-msg">
              Your signature is submitted. Waiting for{' '}
              {party.signaturesNeeded.length - party.signaturesReceived.length} more...
            </p>
          )}
        </section>
      )}

      {/* Phase 2: Split Signing */}
      {party.state === 'awaiting_split_signatures' && party.splitOutput && (
        <section>
          <SplitView
            splitOutput={party.splitOutput}
            potentialReward={party.potentialReward}
            isTestnet={IS_TESTNET}
          />

          <SigningProgress
            label="Reward Split Transaction"
            needed={party.signaturesNeeded}
            received={party.splitSignaturesReceived}
          />

          {walletAddr && isParticipant && !hasSignedSplit && (
            <div className="sign-action">
              <button onClick={handleSignSplit} disabled={signing} className="btn-primary">
                {signing ? signingLabel : 'Sign Reward Split'}
              </button>
            </div>
          )}

          {hasSignedSplit && (
            <p className="status-msg">
              Your signature is submitted. Waiting for{' '}
              {party.signaturesNeeded.length - party.splitSignaturesReceived.length} more...
            </p>
          )}
        </section>
      )}

      {/* Phase 2.5: Waiting for validation to end */}
      {party.state === 'awaiting_validation_end' && (
        <section>
          {party.splitOutput && (
            <SplitView
              splitOutput={party.splitOutput}
              potentialReward={party.potentialReward}
              isTestnet={IS_TESTNET}
            />
          )}
          <div className="status-msg">
            <h3>Waiting for Validation Period to End</h3>
            <p>
              All signatures collected. The reward split transaction will be
              submitted automatically once the validator's staking period ends
              {party.validatorEndTime ? (
                <> at {new Date(party.validatorEndTime * 1000).toLocaleString()}</>
              ) : null}.
            </p>
            <p style={{ marginTop: '0.5rem', fontSize: '0.85rem', color: '#888' }}>
              Polling every {POLL_INTERVAL / 1000}s...
            </p>
          </div>
        </section>
      )}

      {/* Phase 3: Complete */}
      {party.state === 'complete' && (
        <section className="complete-section">
          <h2>All Done!</h2>

          {party.validatorTxID && (
            <p>
              Validator Tx:{' '}
              <a
                href={`${explorerBase}/${party.validatorTxID}`}
                target="_blank"
                rel="noopener noreferrer"
              >
                {party.validatorTxID}
              </a>
            </p>
          )}

          {party.splitTxID && (
            <p>
              Split Tx:{' '}
              <a
                href={`${explorerBase}/${party.splitTxID}`}
                target="_blank"
                rel="noopener noreferrer"
              >
                {party.splitTxID}
              </a>
            </p>
          )}

          {party.splitOutput && (
            <SplitView
              splitOutput={party.splitOutput}
              potentialReward={party.potentialReward}
              isTestnet={IS_TESTNET}
            />
          )}
        </section>
      )}
    </div>
  )
}
