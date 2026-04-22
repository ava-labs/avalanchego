import { useState } from 'react'
import type { InputConfig, StakerConfig } from '../types'
import { createParty } from '../api'
import { WalletConnect, type ConnectMode } from '../components/WalletConnect'
import { ValidatorForm, toValidatorConfig, type ValidatorFormData } from '../components/ValidatorForm'
import { StakerList } from '../components/StakerList'

const NETWORK_ID = 88888 // tmpnet; change to 5 for Fuji
const IS_TESTNET = true

interface Props {
  onCreated: (partyId: string) => void
}

export function CreateParty({ onCreated }: Props) {
  const [walletAddr, setWalletAddr] = useState<string | null>(null)
  const [privateKey, setPrivateKey] = useState<string | null>(null)
  const [validator, setValidator] = useState<ValidatorFormData>({
    nodeID: '',
    blsPublicKey: '',
    blsProofOfPossession: '',
    durationSeconds: 30,
    delegationFee: 20000, // 2%
  })
  const [stakers, setStakers] = useState<StakerConfig[]>([
    { address: '', amount: 0, feePayer: true },
  ])
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [joinUrl, setJoinUrl] = useState<string | null>(null)

  const handleConnect = (address: string, _mode: ConnectMode, key?: string) => {
    setWalletAddr(address)
    if (key) setPrivateKey(key)
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    setSubmitting(true)

    try {
      // Compute absolute endTime from duration at submission time
      const config: InputConfig = {
        validator: toValidatorConfig(validator),
        stakers,
      }
      const res = await createParty(config)
      const url = `${window.location.origin}/join/${res.partyID}`
      setJoinUrl(url)
      onCreated(res.partyID)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create party')
    } finally {
      setSubmitting(false)
    }
  }

  if (joinUrl) {
    return (
      <div className="container">
        <h1>Party Created</h1>
        <p>Share this link with all stakers:</p>
        <div className="join-url">
          <code>{joinUrl}</code>
          <button onClick={() => navigator.clipboard.writeText(joinUrl)}>Copy</button>
        </div>
      </div>
    )
  }

  return (
    <div className="container">
      <h1>Create Multi-Party Staking Session</h1>

      <WalletConnect
        networkID={NETWORK_ID}
        isTestnet={IS_TESTNET}
        address={walletAddr}
        privateKey={privateKey}
        onConnect={handleConnect}
      />

      <form onSubmit={handleSubmit}>
        <ValidatorForm value={validator} onChange={setValidator} />
        <StakerList stakers={stakers} onChange={setStakers} connectedAddress={walletAddr} />

        {error && <p className="error">{error}</p>}

        <button type="submit" disabled={submitting} className="btn-primary">
          {submitting ? 'Creating...' : 'Create Party'}
        </button>
      </form>
    </div>
  )
}
