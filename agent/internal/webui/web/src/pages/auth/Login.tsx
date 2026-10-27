import { useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useAuth } from '@/lib/auth-context';
import { ApiError } from '@/lib/api';

export function Login() {
  const { login } = useAuth();
  const navigate = useNavigate();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError('');
    setBusy(true);
    try {
      await login(username, password);
      navigate('/app/overview');
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Sign in failed');
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
              background: 'linear-gradient(135deg,var(--primary),var(--violet))',
              display: 'grid',
              placeItems: 'center',
            }}
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="#fff" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={{ width: 22, height: 22 }}>
              <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z" />
            </svg>
          </div>
          <h2 style={{ margin: 0, fontSize: 17 }}>Sign in</h2>
          <p style={{ margin: '4px 0 0', fontSize: 12, color: 'var(--text-faint)' }}>Use the admin account created on this agent</p>
        </div>
        <form className="card" style={{ display: 'flex', flexDirection: 'column', gap: 10 }} onSubmit={onSubmit}>
          <label style={{ fontSize: 11, color: 'var(--text-faint)' }}>
            Username
            <input type="text" value={username} onChange={(e) => setUsername(e.target.value)} style={{ width: '100%', marginTop: 4 }} required />
          </label>
          <label style={{ fontSize: 11, color: 'var(--text-faint)' }}>
            Password
            <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} style={{ width: '100%', marginTop: 4 }} required />
          </label>
          {error && <p style={{ margin: 0, fontSize: 11.5, color: 'var(--red)' }}>{error}</p>}
          <button className="btn btn-primary" style={{ width: '100%', marginTop: 6 }} disabled={busy} type="submit">
            {busy ? 'Signing in…' : 'Sign in'}
          </button>
        </form>
        <div style={{ textAlign: 'center', marginTop: 16, display: 'flex', flexDirection: 'column', gap: 8 }}>
          <Link to="/pair" style={{ fontSize: 12.5, color: 'var(--primary)', textDecoration: 'none' }}>
            Pair this browser with a code instead
          </Link>
          <Link to="/register" style={{ fontSize: 12.5, color: 'var(--text-faint)', textDecoration: 'none' }}>
            First time? Create the admin account
          </Link>
        </div>
      </div>
    </div>
  );
}
