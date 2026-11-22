import { NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom';
import { useMutation, useQuery } from '@tanstack/react-query';
import {
  Search,
  Bell,
  RotateCw,
  LogOut,
} from 'lucide-react';
import { api, apiMessage } from '@/lib/api';
import { useAuth } from '@/lib/auth-context';
import { PAGES } from '@/lib/routes';
import { statusQuery } from '@/lib/queries';
import { useAgentEvents } from '@/lib/useAgentEvents';
import { useToast } from '@/lib/toast-context';
import { CommandPalette } from './CommandPalette';

function formatUptime(seconds: number): string {
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  return d > 0 ? `up ${d}d ${h}h` : `up ${h}h`;
}

/** How often the shell re-polls health and status for the sidebar and top bar. */
const SHELL_POLL_INTERVAL_MS = 15000;

export function AppShell() {
  const navigate = useNavigate();
  const location = useLocation();
  const { logout } = useAuth();
  const { toast } = useToast();
  useAgentEvents();
  const health = useQuery({ queryKey: ['health'], queryFn: api.health, refetchInterval: SHELL_POLL_INTERVAL_MS });
  const status = useQuery(statusQuery(SHELL_POLL_INTERVAL_MS));
  const restart = useMutation({
    mutationFn: api.restartAgent,
    onSuccess: () => toast('Restart requested'),
    onError: () => toast('Restart failed'),
  });

  const title = PAGES.find((p) => location.pathname.includes(`/${p.id}`)) ?? PAGES.find((p) => p.id === 'overview');

  return (
    <div className="dashboard-shell">
      <nav className="dsidebar">
        <div className="dsidebar-brand">
          <span className="dot" />
          <div>
            <div className="t">{health.data?.name || 'this agent'}</div>
            <div className="s">{health.data?.address || health.data?.tailscaleAddress || 'connecting…'}</div>
          </div>
        </div>
        <div className="dnav">
          {PAGES.map(({ id, title: label, icon: Icon }) => (
            <NavLink key={id} to={id} className={({ isActive }) => `dnav-item${isActive ? ' active' : ''}`}>
              <Icon />
              {label}
            </NavLink>
          ))}
        </div>
        <div className="dsidebar-foot">
          <div className="who" onClick={() => { logout().then(() => navigate('/login'), (err) => toast(apiMessage(err, 'Sign out failed'))); }}>
            <div className="who-avatar">{(health.data?.name || 'A')[0].toUpperCase()}</div>
            <div>
              <div className="who-name">Signed in</div>
            </div>
            <button className="iconbtn" style={{ marginLeft: 'auto', width: 26, height: 26 }} title="Sign out">
              <LogOut style={{ width: 14, height: 14 }} />
            </button>
          </div>
        </div>
      </nav>

      <div className="dmain">
        <div className="dtopbar">
          <div>
            <h1>{title?.title ?? 'Overview'}</h1>
            <div className="crumbsub">
              {health.data?.name || 'This PC'}
              {health.data?.version ? ` · v${health.data.version}` : ''}
              {status.data ? ` · ${formatUptime(status.data.uptimeSeconds)}` : ''}
            </div>
          </div>
          <div className="dsearch" onClick={() => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', metaKey: true }))}>
            <Search />
            Search or jump to…
            <kbd>⌘K</kbd>
          </div>
          <div className="dtopbar-actions">
            <button className="iconbtn" title="Notifications">
              <Bell />
            </button>
            <button
              className="iconbtn"
              title="Restart agent"
              onClick={() => restart.mutate()}
            >
              <RotateCw />
            </button>
          </div>
        </div>
        <div className="dcontent">
          <Outlet />
        </div>
      </div>
      <CommandPalette />
    </div>
  );
}
