import { Navigate, Route, Routes } from 'react-router-dom';
import { useAuth } from '@/lib/auth-context';
import { Login } from '@/pages/auth/Login';
import { PairCode } from '@/pages/auth/PairCode';
import { Register } from '@/pages/auth/Register';
import { AppShell } from '@/components/AppShell';
import { Overview } from '@/pages/Overview';
import { Files } from '@/pages/Files';
import { Transfers } from '@/pages/Transfers';
import { Devices } from '@/pages/Devices';
import { Users } from '@/pages/Users';
import { Logs } from '@/pages/Logs';
import { Settings } from '@/pages/Settings';

function RequireAuth({ children }: { children: React.ReactNode }) {
  const { authenticated, ready } = useAuth();
  if (!ready) return <div role="status" className="auth-checking">Checking session…</div>;
  if (!authenticated) return <Navigate to="/login" replace />;
  return <>{children}</>;
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route path="/pair" element={<PairCode />} />
      <Route path="/register" element={<Register />} />
      <Route
        path="/app"
        element={
          <RequireAuth>
            <AppShell />
          </RequireAuth>
        }
      >
        <Route index element={<Navigate to="overview" replace />} />
        <Route path="overview" element={<Overview />} />
        <Route path="files" element={<Files />} />
        <Route path="transfers" element={<Transfers />} />
        <Route path="devices" element={<Devices />} />
        <Route path="users" element={<Users />} />
        <Route path="logs" element={<Logs />} />
        <Route path="settings" element={<Settings />} />
      </Route>
      <Route path="*" element={<Navigate to="/app/overview" replace />} />
    </Routes>
  );
}
