interface Props {
  label: string
  needed: string[]
  received: string[]
}

export function SigningProgress({ label, needed, received }: Props) {
  const receivedSet = new Set(received)

  return (
    <div className="signing-progress">
      <h3>
        {label}: {received.length} of {needed.length} signatures
      </h3>
      <div className="progress-bar">
        <div
          className="progress-fill"
          style={{ width: `${(received.length / Math.max(needed.length, 1)) * 100}%` }}
        />
      </div>
      <ul className="signer-list">
        {needed.map((addr) => (
          <li key={addr}>
            <code>{addr}</code>
            {receivedSet.has(addr) ? (
              <span className="badge badge-success">Signed</span>
            ) : (
              <span className="badge badge-pending">Pending</span>
            )}
          </li>
        ))}
      </ul>
    </div>
  )
}
