import { useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useAuth } from '@/lib/auth-context';
import { ApiError } from '@/lib/api';

export function Register() {
  const { register } = useAuth();
  const navigate = useNavigate();
  const [pairingCode, setPairingCode] = useState('');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError('');
    if (password !== confirm) {
      setError('Passwords do not match');
      return;
    }
    if (password.length < 8) {
      setError('Password must be at least 8 characters');
      return;
    }
    setBusy(true);
    try {
      await register(pairingCode, username, password);
      navigate('/app/overview');
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Registration failed');
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="authpage">
      <div style={{ width: 340 }}>
        <div style={{ textAlign: 'center', marginBottom: 22 }}>
          <div
            style={{
              width: 48,
              height: 48,
              margin: '0 auto 12px',
              borderRadius: 'var(--r-lg)',
              background: 'var(--surface)',
              border: '1px solid var(--border)',
              display: 'grid',
              placeItems: 'center',
            }}
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="var(--primary)" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={{ width: 22, height: 22 }}>
              <circle cx="12" cy="8" r="4" />
              <path d="M4 21c1.5-4 5-6 8-6s6.5 2 8 6" />
            </svg>
          </div>
          <h2 style={{ margin: 0, fontSize: 17 }}>Create the admin account</h2>
          <p style={{ margin: '4px 0 0', fontSize: 12, color: 'var(--text-faint)' }}>One-time setup for this agent install</p>
        </div>
        <form className="card" style={{ display: 'flex', flexDirection: 'column', gap: 10 }} onSubmit={onSubmit}>
          <label style={{ fontSize: 11, color: 'var(--text-faint)' }}>
            Pairing code
            <input type="text" value={pairingCode} onChange={(e) => setPairingCode(e.target.value)} placeholder="e.g. 5H7RKEW5" style={{ width: '100%', marginTop: 4 }} required />
          </label>
          <label style={{ fontSize: 11, color: 'var(--text-faint)' }}>
            Username
            <input type="text" value={username} onChange={(e) => setUsername(e.target.value)} placeholder="e.g. zaid" style={{ width: '100%', marginTop: 4 }} required />
          </label>
          <label style={{ fontSize: 11, color: 'var(--text-faint)' }}>
            Password
            <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} placeholder="At least 8 characters" style={{ width: '100%', marginTop: 4 }} required />
          </label>
          <label style={{ fontSize: 11, color: 'var(--text-faint)' }}>
            Confirm password
            <input type="password" value={confirm} onChange={(e) => setConfirm(e.target.value)} style={{ width: '100%', marginTop: 4 }} required />
          </label>
          {error && <p style={{ margin: 0, fontSize: 11.5, color: 'var(--red)' }}>{error}</p>}
          <button className="btn btn-primary" style={{ width: '100%', marginTop: 6 }} disabled={busy} type="submit">
            {busy ? 'Creating…' : 'Create account'}
          </button>
        </form>
        <div style={{ textAlign: 'center', marginTop: 16 }}>
          <Link to="/login" style={{ fontSize: 12.5, color: 'var(--text-faint)', textDecoration: 'none' }}>
            Already have an account? Sign in
          </Link>
        </div>
      </div>
    </div>
  );
}
