import type { StakerConfig } from '../types'

interface Props {
  stakers: StakerConfig[]
  onChange: (stakers: StakerConfig[]) => void
  connectedAddress: string | null
}

const NAVAX = 1_000_000_000 // 1 AVAX = 1e9 nAVAX

export function StakerList({ stakers, onChange, connectedAddress }: Props) {
  const update = (idx: number, patch: Partial<StakerConfig>) => {
    const next = stakers.map((s, i) => (i === idx ? { ...s, ...patch } : s))
    onChange(next)
  }

  const setFeePayer = (idx: number) => {
    const next = stakers.map((s, i) => ({ ...s, feePayer: i === idx }))
    onChange(next)
  }

  const addStaker = () => {
    onChange([...stakers, { address: '', amount: 0, feePayer: false }])
  }

  const removeStaker = (idx: number) => {
    if (stakers.length <= 1) return
    const next = stakers.filter((_, i) => i !== idx)
    // Ensure exactly one fee payer
    if (!next.some((s) => s.feePayer) && next.length > 0) {
      next[0]!.feePayer = true
    }
    onChange(next)
  }

  return (
    <fieldset>
      <legend>Stakers</legend>

      {stakers.map((s, i) => (
        <div key={i} className="staker-row">
          <label>
            Address
            <div className="input-with-button">
              <input
                type="text"
                placeholder="P-fuji1..."
                value={s.address}
                onChange={(e) => update(i, { address: e.target.value })}
              />
              {connectedAddress && (
                <button
                  type="button"
                  className="btn-small"
                  onClick={() => update(i, { address: connectedAddress })}
                >
                  Use my wallet
                </button>
              )}
            </div>
          </label>

          <label>
            Amount (AVAX)
            <input
              type="number"
              min={0}
              step={0.001}
              value={s.amount / NAVAX || ''}
              onChange={(e) =>
                update(i, { amount: Math.round(parseFloat(e.target.value || '0') * NAVAX) })
              }
            />
          </label>

          <label className="radio-label">
            <input
              type="radio"
              name="feePayer"
              checked={s.feePayer}
              onChange={() => setFeePayer(i)}
            />
            Fee Payer
          </label>

          {stakers.length > 1 && (
            <button type="button" className="btn-small btn-danger" onClick={() => removeStaker(i)}>
              Remove
            </button>
          )}
        </div>
      ))}

      <button type="button" onClick={addStaker}>
        + Add Staker
      </button>
    </fieldset>
  )
}
