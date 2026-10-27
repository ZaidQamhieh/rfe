import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Monitor, Smartphone, Shield, Trash2 } from 'lucide-react';
import { api, type Device } from '@/lib/api';
import { useToast } from '@/lib/toast-context';

function formatAgo(unixSeconds: number): string {
  const s = Date.now() / 1000 - unixSeconds;
  if (s < 60) return 'just now';
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  return `${Math.floor(s / 86400)}d ago`;
}

const ONLINE_WINDOW_SECONDS = 300;
function isOnline(d: Device): boolean {
  return Date.now() / 1000 - d.lastSeen < ONLINE_WINDOW_SECONDS;
}

function DeviceCard({ device }: { device: Device }) {
  const queryClient = useQueryClient();
  const { toast } = useToast();
  const [jailRoot, setJailRoot] = useState(device.jailRoot ?? '');

  const patch = useMutation({
    mutationFn: (body: Parameters<typeof api.patchDevice>[1]) => api.patchDevice(device.id, body),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['devices'] });
    },
    onError: () => toast('Update failed'),
  });

  const online = isOnline(device);
  const hasFileCapabilities =
    device.browse != null && device.download != null && device.upload != null &&
    device.modify != null && device.delete != null && device.share != null;
  const filePermissions = [
    ['Browse files', device.browse ?? false, () => patch.mutate({ browse: !device.browse })],
    ['Download files', device.download ?? false, () => patch.mutate({ download: !device.download })],
    ['Upload files', device.upload ?? false, () => patch.mutate({ upload: !device.upload })],
    ['Modify files', device.modify ?? false, () => patch.mutate({ modify: !device.modify })],
    ['Delete files', device.delete ?? false, () => patch.mutate({ delete: !device.delete })],
    ['Create share links', device.share ?? false, () => patch.mutate({ share: !device.share })],
  ] as const;

  return (
    <div className="device-card">
      <div className="device-card-head">
        <div className="avatar">
          {device.viaLogin ? <Monitor /> : <Smartphone />}
          <span className={`pulse-dot${online ? ' live' : ' off'}`} />
        </div>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontSize: 13.5, fontWeight: 600 }}>{device.label}</div>
          <div style={{ fontSize: 11.5, color: 'var(--text-faint)' }}>{formatAgo(device.lastSeen)}</div>
        </div>
        {device.current && <span className="badge blue">This device</span>}
      </div>

      <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', marginBottom: 12 }}>
        <span className={`badge ${online ? 'green' : 'neutral'}`}>{online ? 'Online' : 'Offline'}</span>
        <span className="badge neutral">{device.viaLogin ? 'Admin' : 'Guest'}</span>
        {device.readOnly && <span className="badge amber">Read-only</span>}
        {device.revoked && <span className="badge red">Revoked</span>}
      </div>

      <div className="field-row" style={{ padding: '8px 0' }}>
        <div className="field-main">
          <div className="field-title">Address</div>
          <div className="field-sub">{device.lastAddress || 'unknown'}</div>
        </div>
      </div>
      <div className="field-row" style={{ padding: '8px 0' }}>
        <div className="field-main">
          <div className="field-title">Quota</div>
          <div className="field-sub">Not tracked</div>
        </div>
      </div>
      <div className="field-row" style={{ padding: '8px 0' }}>
        <div className="field-main">
          <div className="field-title">Read-only</div>
        </div>
        <div
          className={`switch${device.readOnly ? ' on' : ''}`}
          onClick={() => patch.mutate({ readOnly: !device.readOnly })}
        />
      </div>
      <div className="field-row" style={{ padding: '8px 0', gap: 8 }}>
        <input
          type="text"
          value={jailRoot}
          onChange={(e) => setJailRoot(e.target.value)}
          placeholder="Jail path (empty = unrestricted)"
          style={{ flex: 1, minWidth: 0 }}
        />
        <button
          className="btn btn-ghost btn-sm"
          disabled={jailRoot === (device.jailRoot ?? '')}
          onClick={() => patch.mutate({ jailRoot })}
        >
          Save
        </button>
      </div>

      {hasFileCapabilities && (
        <div style={{ borderTop: '1px solid var(--border)', marginTop: 8, paddingTop: 8 }}>
          <div className="field-title" style={{ marginBottom: 4 }}>File permissions</div>
          <div className="field-sub" style={{ marginBottom: 6 }}>
            Upload includes overwrite. Global read-only, configured roots, this device's jail, and global sharing still apply.
          </div>
          {device.viaLogin ? (
            <div className="field-sub">Owner access keeps full file permissions.</div>
          ) : filePermissions.map(([label, enabled, toggle]) => (
            <div className="field-row" style={{ padding: '6px 0' }} key={label}>
              <div className="field-main"><div className="field-title">{label}</div></div>
              <button
                type="button"
                aria-label={`${label}: ${enabled ? 'enabled' : 'disabled'}`}
                aria-pressed={enabled}
                className={`switch${enabled ? ' on' : ''}`}
                disabled={patch.isPending}
                onClick={toggle}
              />
            </div>
          ))}
        </div>
      )}

      <button
        className="btn btn-danger btn-sm"
        style={{ width: '100%', marginTop: 8 }}
        onClick={() => patch.mutate({ revoked: !device.revoked })}
      >
        <Trash2 />
        {device.revoked ? 'Unrevoke' : 'Revoke'}
      </button>
    </div>
  );
}

export function Devices() {
  const { toast } = useToast();
  const devices = useQuery({ queryKey: ['devices'], queryFn: api.listDevices, refetchInterval: 15000 });
  const status = useQuery({ queryKey: ['status'], queryFn: api.status, refetchInterval: 15000 });

  const [code, setCode] = useState<{ code: string; expiresAt: number; qrPngBase64?: string } | null>(null);
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);

  const generate = useMutation({
    mutationFn: api.generatePairingCode,
    onSuccess: (data) => {
      setCode(data);
      toast('New pairing code generated');
    },
    onError: () => toast('Failed to generate pairing code'),
  });

  const list = devices.data ?? [];
  const total = list.length;
  const online = list.filter(isOnline).length;
  const guests = list.filter((d) => !d.viaLogin).length;
  const admins = list.filter((d) => d.viaLogin).length;
  const readOnlyCount = list.filter((d) => d.readOnly).length;
  const revokedCount = list.filter((d) => d.revoked).length;
  const usedBytes = status.data ? status.data.totalBytes - status.data.freeBytes : undefined;

  const remaining = code ? Math.max(0, Math.floor(code.expiresAt - now / 1000)) : 0;
  const mm = String(Math.floor(remaining / 60)).padStart(2, '0');
  const ss = String(remaining % 60).padStart(2, '0');
  const codeDigits = code && remaining > 0 ? code.code.split('') : [];

  return (
    <div>
      <div className="stat-row-7">
        <div className="mini-stat"><div className="label">Total</div><div className="value">{total}</div></div>
        <div className="mini-stat"><div className="label">Online</div><div className="value" style={{ color: 'var(--green)' }}>{online}</div></div>
        <div className="mini-stat"><div className="label">Guests</div><div className="value" style={{ color: 'var(--amber)' }}>{guests}</div></div>
        <div className="mini-stat"><div className="label">Admins</div><div className="value" style={{ color: 'var(--primary)' }}>{admins}</div></div>
        <div className="mini-stat"><div className="label">Read-only</div><div className="value" style={{ color: 'var(--amber)' }}>{readOnlyCount}</div></div>
        <div className="mini-stat"><div className="label">Revoked</div><div className="value" style={{ color: 'var(--red)' }}>{revokedCount}</div></div>
        <div className="mini-stat">
          <div className="label">Storage used</div>
          <div className="value">{usedBytes != null ? `${(usedBytes / 1e9).toFixed(0)}GB` : '--'}</div>
        </div>
      </div>

      <div className="grid grid-2">
        <div className="card" style={{ textAlign: 'center' }}>
          <h3 style={{ margin: '0 0 4px' }}>Pair a new device</h3>
          <div style={{ fontSize: 11.5, color: 'var(--text-faint)' }}>POST /v1/pairing/generate</div>
          {codeDigits.length > 0 ? (
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 24, margin: '10px 0' }}>
              {code?.qrPngBase64 && (
                <img
                  src={`data:image/png;base64,${code.qrPngBase64}`}
                  alt="Scan to pair"
                  style={{ width: 140, height: 140, flex: 'none', borderRadius: 'var(--r-md)', background: '#fff', padding: 8 }}
                />
              )}
              <div>
                <div className="otp-tiles" style={{ justifyContent: 'flex-start' }}>
                  {codeDigits.map((c, i) => (
                    <span key={i}>{c}</span>
                  ))}
                </div>
                <div style={{ fontSize: 11.5, color: 'var(--text-faint)', marginTop: 8 }}>
                  Expires in {mm}:{ss} · scan or type the code
                </div>
              </div>
            </div>
          ) : (
            <div style={{ margin: '14px 0', fontSize: 12.5, color: 'var(--text-faint)' }}>
              No active code. Generate one for a new device to redeem.
            </div>
          )}
          <button className="btn btn-primary" disabled={generate.isPending} onClick={() => generate.mutate()}>
            Generate new code
          </button>
        </div>

        <div className="card">
          <div className="panel-head">
            <div className="icon-badge blue">
              <Shield />
            </div>
            <h3>New pairings</h3>
          </div>
          <p style={{ fontSize: 12.5, color: 'var(--text-dim)', lineHeight: 1.6, margin: 0 }}>
            There's no global default jail path — a newly paired device starts unrestricted.
            Set its jail path and read-only flag per device below, right after it pairs.
          </p>
        </div>
      </div>

      <div className="section-label">Paired devices</div>
      {list.length === 0 ? (
        <div className="empty">
          <div className="ico">
            <Monitor />
          </div>
          <h4>No devices paired yet</h4>
          <p>Generate a pairing code above to connect a phone or browser.</p>
        </div>
      ) : (
        <div className="grid grid-3">
          {list.map((d) => (
            <DeviceCard key={d.id} device={d} />
          ))}
        </div>
      )}
    </div>
  );
}
