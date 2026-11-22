import { useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useAuth } from '@/lib/auth-context';
import { APP_HOME } from '@/lib/routes';

export function PairCode() {
  const { pair } = useAuth();
  const navigate = useNavigate();
  const [code, setCode] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError('');
    setBusy(true);
    try {
      await pair(code.trim());
      navigate(APP_HOME);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Pairing failed');
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="authpage">
      <form style={{ width: 360, textAlign: 'center' }} onSubmit={onSubmit}>
        <div
          className="auth-mark"
          style={{ background: 'var(--surface)', border: '1px solid var(--border)' }}
        >
          <svg viewBox="0 0 24 24" fill="none" stroke="var(--primary)" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={{ width: 22, height: 22 }}>
            <rect x="3" y="3" width="7" height="7" />
            <rect x="14" y="3" width="7" height="7" />
            <rect x="3" y="14" width="7" height="7" />
          </svg>
        </div>
        <h2 style={{ margin: 0, fontSize: 17 }}>Pair this browser</h2>
        <p style={{ margin: '4px 0 20px', fontSize: 12.5, color: 'var(--text-faint)' }}>
          Enter the pairing code from <b style={{ color: 'var(--text-dim)' }}>rfe pair</b> or the phone app's pairing screen
        </p>
        <input
          type="text"
          autoFocus
          value={code}
          onChange={(e) => setCode(e.target.value.toUpperCase())}
          placeholder="e.g. 5H7RKEW5"
          style={{
            width: '100%',
            textAlign: 'center',
            fontFamily: 'var(--font-mono)',
            fontSize: 20,
            fontWeight: 700,
            letterSpacing: '0.1em',
            marginBottom: 20,
          }}
        />
        {error && <p style={{ margin: '0 0 12px', fontSize: 11.5, color: 'var(--red)' }}>{error}</p>}
        <button className="btn btn-primary" style={{ width: '100%' }} disabled={busy || !code.trim()} type="submit">
          {busy ? 'Pairing…' : 'Pair browser'}
        </button>
        <Link to="/login" style={{ display: 'block', marginTop: 16, fontSize: 12.5, color: 'var(--text-faint)', textDecoration: 'none' }}>
          Back to sign in
        </Link>
      </form>
    </div>
  );
}
