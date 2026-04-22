import type { SplitPrepareOutput } from '../types'

const NAVAX = 1_000_000_000

interface Props {
  splitOutput: SplitPrepareOutput
  potentialReward: number
  isTestnet: boolean
}

interface OutputEntry {
  address: string
  amount: number
}

export function SplitView({ splitOutput, potentialReward, isTestnet }: Props) {
  // Parse outputs from the unsigned tx JSON to show the split breakdown
  const unsignedTx = splitOutput.unsignedTx as {
    outs?: Array<{
      output?: { amt?: number; outputOwners?: { addrs?: string[] } }
    }>
  }

  const entries: OutputEntry[] = []
  if (unsignedTx?.outs) {
    for (const out of unsignedTx.outs) {
      const amt = out.output?.amt ?? 0
      const addr = out.output?.outputOwners?.addrs?.[0] ?? 'unknown'
      entries.push({ address: addr, amount: amt })
    }
  }

  const totalReward = potentialReward / NAVAX
  const explorerBase = isTestnet
    ? 'https://subnets-test.avax.network/p-chain/tx'
    : 'https://subnets.avax.network/p-chain/tx'

  return (
    <div className="split-view">
      <h3>Reward Split</h3>
      <p>
        Potential Reward: <strong>{totalReward.toFixed(4)} AVAX</strong>
      </p>
      <p>
        Validator TxID:{' '}
        <a
          href={`${explorerBase}/${splitOutput.validatorTxID}`}
          target="_blank"
          rel="noopener noreferrer"
        >
          {splitOutput.validatorTxID}
        </a>
      </p>

      {entries.length > 0 && (
        <table>
          <thead>
            <tr>
              <th>Address</th>
              <th>Reward (AVAX)</th>
              <th>Share</th>
            </tr>
          </thead>
          <tbody>
            {entries.map((e, i) => (
              <tr key={i}>
                <td>
                  <code>{e.address}</code>
                </td>
                <td>{(e.amount / NAVAX).toFixed(4)}</td>
                <td>{potentialReward > 0 ? ((e.amount / potentialReward) * 100).toFixed(1) : 0}%</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
