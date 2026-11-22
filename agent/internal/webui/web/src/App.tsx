import { Navigate, Route, Routes } from 'react-router-dom';
import { useAuth } from '@/lib/auth-context';
import { APP_HOME, PAGES, type PageId } from '@/lib/routes';
import { Login } from '@/pages/auth/Login';
import { PairCode } from '@/pages/auth/PairCode';
import { Register } from '@/pages/auth/Register';
import { SessionUnavailable } from '@/pages/auth/SessionUnavailable';
import { AppShell } from '@/components/AppShell';
import { Overview } from '@/pages/Overview';
import { Files } from '@/pages/Files';
import { Transfers } from '@/pages/Transfers';
import { Devices } from '@/pages/Devices';
import { Users } from '@/pages/Users';
import { Logs } from '@/pages/Logs';
import { Settings } from '@/pages/Settings';

const PAGE_ELEMENTS: Record<PageId, React.ReactElement> = {
  overview: <Overview />,
  files: <Files />,
  transfers: <Transfers />,
  devices: <Devices />,
  users: <Users />,
  logs: <Logs />,
  settings: <Settings />,
};

function RequireAuth({ children }: { children: React.ReactNode }) {
  const { authenticated, ready, unavailable, retry } = useAuth();
  if (!ready) return <div role="status" className="auth-checking">Checking session…</div>;
  if (unavailable) return <SessionUnavailable onRetry={retry} />;
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
        {PAGES.map((page) => (
          <Route key={page.id} path={page.id} element={PAGE_ELEMENTS[page.id]} />
        ))}
      </Route>
      <Route path="*" element={<Navigate to={APP_HOME} replace />} />
    </Routes>
  );
}
