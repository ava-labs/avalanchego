import type { ValidatorConfig } from '../types'

export interface ValidatorFormData {
  nodeID: string
  blsPublicKey: string
  blsProofOfPossession: string
  durationSeconds: number
  delegationFee: number // raw uint32 value (e.g., 20000 = 2%)
}

interface Props {
  value: ValidatorFormData
  onChange: (v: ValidatorFormData) => void
}

/** Convert form data to ValidatorConfig at submission time */
export function toValidatorConfig(form: ValidatorFormData): ValidatorConfig {
  return {
    nodeID: form.nodeID,
    blsPublicKey: form.blsPublicKey,
    blsProofOfPossession: form.blsProofOfPossession,
    endTime: Math.floor(Date.now() / 1000) + form.durationSeconds,
    delegationFee: form.delegationFee,
  }
}

export function ValidatorForm({ value, onChange }: Props) {
  const update = <K extends keyof ValidatorFormData>(key: K, val: ValidatorFormData[K]) =>
    onChange({ ...value, [key]: val })

  return (
    <fieldset>
      <legend>Validator</legend>

      <label>
        Node ID
        <input
          type="text"
          placeholder="NodeID-..."
          value={value.nodeID}
          onChange={(e) => update('nodeID', e.target.value)}
        />
      </label>

      <label>
        BLS Public Key
        <input
          type="text"
          placeholder="0x..."
          value={value.blsPublicKey}
          onChange={(e) => update('blsPublicKey', e.target.value)}
        />
      </label>

      <label>
        BLS Proof of Possession
        <input
          type="text"
          placeholder="0x..."
          value={value.blsProofOfPossession}
          onChange={(e) => update('blsProofOfPossession', e.target.value)}
        />
      </label>

      <label>
        Validation Duration (seconds)
        <input
          type="number"
          min={1}
          placeholder="e.g., 30 for tmpnet, 1209600 for 2 weeks"
          value={value.durationSeconds || ''}
          onChange={(e) => update('durationSeconds', parseInt(e.target.value) || 0)}
        />
        {value.durationSeconds > 0 && (
          <span className="hint">
            Ends ~{formatDuration(value.durationSeconds)} after party creation
          </span>
        )}
      </label>

      <label>
        Delegation Fee (%)
        <input
          type="number"
          min={0}
          max={100}
          step={0.01}
          value={value.delegationFee / 10000}
          onChange={(e) => update('delegationFee', Math.round(parseFloat(e.target.value) * 10000))}
        />
      </label>
    </fieldset>
  )
}

function formatDuration(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`
  return `${Math.floor(seconds / 86400)}d ${Math.floor((seconds % 86400) / 3600)}h`
}
