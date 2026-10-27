import { useEffect, useState } from 'react';
import { Command } from 'cmdk';
import { useNavigate } from 'react-router-dom';
import {
  LayoutGrid,
  Folder,
  Activity,
  Monitor,
  Users,
  ScrollText,
  Settings,
  QrCode,
  Upload,
  Link as LinkIcon,
} from 'lucide-react';
import { useToast } from '@/lib/toast-context';

const NAV_ITEMS = [
  { label: 'Overview', to: '/app/overview', icon: LayoutGrid },
  { label: 'Files', to: '/app/files', icon: Folder },
  { label: 'Transfers', to: '/app/transfers', icon: Activity },
  { label: 'Devices', to: '/app/devices', icon: Monitor },
  { label: 'Users', to: '/app/users', icon: Users },
  { label: 'Logs', to: '/app/logs', icon: ScrollText },
  { label: 'Settings', to: '/app/settings', icon: Settings },
];

const ACTION_ITEMS = [
  { label: 'Pair a new device', to: '/app/devices', icon: QrCode },
  { label: 'Upload a file', to: '/app/files', icon: Upload },
  { label: 'Generate a share link', to: '/app/files', icon: LinkIcon },
];

export function CommandPalette() {
  const [open, setOpen] = useState(false);
  const navigate = useNavigate();
  const { toast } = useToast();

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        setOpen((v) => !v);
      }
      if (e.key === 'Escape') setOpen(false);
    };
    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, []);

  const go = (to: string, label: string) => {
    setOpen(false);
    navigate(to);
    toast(label);
  };

  return (
    <>
      <div className={`cmdk-backdrop${open ? ' active' : ''}`} onClick={() => setOpen(false)} />
      <Command
        className={`cmdk${open ? ' active' : ''}`}
        shouldFilter
        loop
        label="Command palette"
      >
        <div className="cmdk-input">
          <svg viewBox="0 0 24 24" fill="none" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <circle cx="11" cy="11" r="7" />
            <path d="m21 21-4.3-4.3" />
          </svg>
          <Command.Input autoFocus placeholder="Jump to a page or run a command…" />
        </div>
        <Command.List className="cmdk-list">
          <Command.Empty style={{ padding: '14px', fontSize: '12.5px', color: 'var(--text-faint)' }}>
            No results.
          </Command.Empty>
          <Command.Group heading="Go to">
            {NAV_ITEMS.map(({ label, to, icon: Icon }) => (
              <Command.Item key={to} className="cmdk-item" onSelect={() => go(to, label)}>
                <Icon className="icon" />
                {label}
                <span className="grp">Go to</span>
              </Command.Item>
            ))}
          </Command.Group>
          <Command.Group heading="Action">
            {ACTION_ITEMS.map(({ label, to, icon: Icon }) => (
              <Command.Item key={label} className="cmdk-item" onSelect={() => go(to, label)}>
                <Icon className="icon" />
                {label}
                <span className="grp">Action</span>
              </Command.Item>
            ))}
          </Command.Group>
        </Command.List>
      </Command>
    </>
  );
}
