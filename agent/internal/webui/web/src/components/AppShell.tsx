import { NavLink, Outlet, useNavigate } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import {
  LayoutGrid,
  Folder,
  Activity,
  Monitor,
  Users,
  ScrollText,
  Settings,
  Search,
  Bell,
  RotateCw,
  LogOut,
} from 'lucide-react';
import { api } from '@/lib/api';
import { useAuth } from '@/lib/auth-context';
import { useToast } from '@/lib/toast-context';
import { CommandPalette } from './CommandPalette';

const NAV = [
  { to: 'overview', label: 'Overview', icon: LayoutGrid },
  { to: 'files', label: 'Files', icon: Folder },
  { to: 'transfers', label: 'Transfers', icon: Activity },
  { to: 'devices', label: 'Devices', icon: Monitor },
  { to: 'users', label: 'Users', icon: Users },
  { to: 'logs', label: 'Logs', icon: ScrollText },
  { to: 'settings', label: 'Settings', icon: Settings },
];

const TITLES: Record<string, [string, string]> = {
  overview: ['Overview', 'Live host health, quick actions, and connected devices at a glance.'],
  files: ['Files', 'Full file management from a browser — Browse, Recent, Favorites, Trash, Shares.'],
  transfers: ['Transfers', 'Every upload/download session, live and historical, across every paired device.'],
  devices: ['Devices', 'Paired phones and browsers — pairing, quotas, jail paths, revocation.'],
  users: ['Users', 'Login accounts for the web companion — separate from paired devices.'],
  logs: ['Logs', 'Live tail of the agent service log.'],
  settings: ['Settings', 'Access, allowed folders, bandwidth, and photo-backup destination.'],
};

function formatUptime(seconds: number): string {
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  return d > 0 ? `up ${d}d ${h}h` : `up ${h}h`;
}

export function AppShell() {
  const navigate = useNavigate();
  const { logout } = useAuth();
  const { toast } = useToast();
  const health = useQuery({ queryKey: ['health'], queryFn: api.health, refetchInterval: 15000 });
  const status = useQuery({ queryKey: ['status'], queryFn: api.status, refetchInterval: 15000 });

  const active = NAV.find((n) => location.pathname.includes(`/${n.to}`))?.to ?? 'overview';
  const title = TITLES[active];

  return (
    <div className="dashboard-shell active">
      <nav className="dsidebar">
        <div className="dsidebar-brand">
          <span className="dot" />
          <div>
            <div className="t">{health.data?.name || 'this agent'}</div>
            <div className="s">{health.data?.address || health.data?.tailscaleAddress || 'connecting…'}</div>
          </div>
        </div>
        <div className="dnav">
          {NAV.map(({ to, label, icon: Icon }) => (
            <NavLink key={to} to={to} className={({ isActive }) => `dnav-item${isActive ? ' active' : ''}`}>
              <Icon />
              {label}
            </NavLink>
          ))}
        </div>
        <div className="dsidebar-foot">
          <div className="who" onClick={() => { logout(); navigate('/login'); }}>
            <div className="who-avatar">{(health.data?.name || 'A')[0].toUpperCase()}</div>
            <div>
              <div className="who-name">Signed in</div>
              <div className="who-role">Admin</div>
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
            <h1>{title?.[0] ?? 'Overview'}</h1>
            <div className="crumbsub">
              {health.data?.name || 'main-pc'}
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
              onClick={() => {
                api.restartAgent().catch(() => {});
                toast('Restart requested');
              }}
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
