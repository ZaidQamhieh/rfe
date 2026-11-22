/** Shown when the session check failed for a reason other than the agent refusing the session. Keeps the session. */
export function SessionUnavailable({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="authpage">
      <div className="card" style={{ width: 340, textAlign: 'center', display: 'flex', flexDirection: 'column', gap: 10 }}>
        <h2 style={{ margin: 0, fontSize: 17 }}>Can't reach this PC</h2>
        <p style={{ margin: 0, fontSize: 12, color: 'var(--text-faint)' }}>Check that the agent is running and the network is up.</p>
        <button className="btn btn-primary" style={{ width: '100%', marginTop: 6 }} onClick={onRetry}>
          Retry
        </button>
      </div>
    </div>
  );
}
