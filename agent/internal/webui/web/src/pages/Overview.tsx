import { useEffect, useRef, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useMutation, useQuery } from '@tanstack/react-query';
import {
  KeyRound,
  FolderPlus,
  Upload,
  Share2,
  Power,
  RotateCw,
  Monitor,
  ScrollText,
} from 'lucide-react';
import { api, ApiError, type Metrics } from '@/lib/api';
import { canWake } from '@/lib/requests';
import { formatAgo, formatLogTime, rateParts, usedBytesOf, wholeGB } from '@/lib/format';
import { pagePath } from '@/lib/routes';
import { DEVICES_REFRESH_MS, LOGS_REFRESH_MS, METRICS_REFRESH_MS, REFRESH_INTERVAL_MS, isOnline, statusQuery } from '@/lib/queries';
import { useToast } from '@/lib/toast-context';
import { Dialog } from '@/components/Dialog';
import { ListError } from '@/components/ListError';

const isMac = (s: string) => /^([0-9a-f]{2}:){5}[0-9a-f]{2}$/i.test(s.trim());

// Decimal units (1 MB = 1e6 bytes), unlike Settings' binary rate.
function formatRate(bytesPerSec: number | null): { value: string; unit: string } {
  if (bytesPerSec == null) return { value: '--', unit: 'MB/s' };
  return rateParts(bytesPerSec, 1000);
}

// Purely decorative — no data behind the curve, just the mockup's spark motif.
function Spark({ color }: { color: string }) {
  return (
    <svg className="spark" viewBox="0 0 100 30" preserveAspectRatio="none">
      <path className="spark-path" d="M0,22 L15,18 L30,20 L45,10 L60,14 L75,6 L100,9" stroke={color} />
    </svg>
  );
}

export function Overview() {
  const navigate = useNavigate();
  const { toast } = useToast();

  const metrics = useQuery({ queryKey: ['metrics'], queryFn: api.metrics, refetchInterval: METRICS_REFRESH_MS });
  const status = useQuery(statusQuery(REFRESH_INTERVAL_MS));
  const devices = useQuery({ queryKey: ['devices'], queryFn: api.listDevices, refetchInterval: DEVICES_REFRESH_MS });
  const logs = useQuery({ queryKey: ['logs'], queryFn: api.listLogs, refetchInterval: LOGS_REFRESH_MS });

  const prevMetrics = useRef<Metrics | null>(null);
  const [netRate, setNetRate] = useState<number | null>(null);
  useEffect(() => {
    if (!metrics.data) return;
    const prev = prevMetrics.current;
    if (prev && metrics.data.tsMs > prev.tsMs) {
      const dtSec = (metrics.data.tsMs - prev.tsMs) / 1000;
      const dBytes = metrics.data.rxBytes - prev.rxBytes + (metrics.data.txBytes - prev.txBytes);
      if (dtSec > 0 && dBytes >= 0) setNetRate(dBytes / dtSec);
    }
    prevMetrics.current = metrics.data;
  }, [metrics.data]);

  const [wakeOpen, setWakeOpen] = useState(false);
  const [wakeMac, setWakeMac] = useState('');
  const wake = useMutation({
    mutationFn: api.wake,
    onSuccess: () => {
      toast('Wake packet sent');
      setWakeOpen(false);
    },
    onError: (err) =>
      toast(err instanceof ApiError && err.code === 'CAPABILITY_DENIED'
        ? 'This device is not allowed to wake computers. Allow it under Devices on the PC.'
        : 'Could not send the wake packet'),
  });

  const restart = useMutation({
    mutationFn: api.restartAgent,
    onSuccess: () => toast('Agent restart requested'),
    onError: () => toast('Restart failed'),
  });

  const rate = formatRate(netRate);
  const usedBytes = status.data ? usedBytesOf(status.data) : undefined;

  const quickActions = [
    { label: 'Pair device', sub: 'POST /v1/pairing/generate', icon: KeyRound, primary: true, onClick: () => navigate(pagePath('devices')) },
    { label: 'New folder', sub: 'POST /v1/fs/folder', icon: FolderPlus, onClick: () => navigate(pagePath('files')) },
    { label: 'Upload file', sub: 'POST /v1/transfers', icon: Upload, onClick: () => navigate(pagePath('files')) },
    { label: 'Share link', sub: 'POST /v1/share/mint', icon: Share2, onClick: () => navigate(pagePath('files')) },
    ...(canWake(status.data)
      ? [{
          label: 'Wake device',
          sub: 'POST /v1/wol',
          icon: Power,
          onClick: () => setWakeOpen(true),
        }]
      : []),
    {
      label: 'Restart agent',
      sub: 'POST /v1/agent/restart',
      icon: RotateCw,
      onClick: () => restart.mutate(),
    },
  ];

  const topDevices = (devices.data ?? []).slice(0, 3);
  const recentLogs = (logs.data ?? []).slice(-5).reverse();
  // A failed query with nothing cached is an error to show, not an empty list.
  const statsFailed = (metrics.isError && !metrics.data) || (status.isError && !status.data);
  const statsError = metrics.isError && !metrics.data ? metrics.error : status.error;
  const devicesFailed = devices.isError && !devices.data;
  const logsFailed = logs.isError && !logs.data;

  return (
    <div>
      {statsFailed && <ListError title="Could not load system stats" error={statsError} />}
      <div className="grid grid-4">
        <div className="card stat-card">
          <div className="label">CPU</div>
          <div className="value">
            {metrics.data ? metrics.data.cpuPercent.toFixed(0) : '--'}
            <span className="unit">%</span>
          </div>
          <Spark color="var(--primary)" />
        </div>
        <div className="card stat-card">
          <div className="label">Memory</div>
          <div className="value">
            {metrics.data ? metrics.data.ramPercent.toFixed(0) : '--'}
            <span className="unit">%</span>
          </div>
          <Spark color="var(--violet)" />
        </div>
        <div className="card stat-card">
          <div className="label">Network I/O</div>
          <div className="value">
            {rate.value}
            <span className="unit">{rate.unit}</span>
          </div>
          <Spark color="var(--green)" />
        </div>
        <div className="card stat-card">
          <div className="label">Disk</div>
          <div className="value">
            {usedBytes != null ? wholeGB(usedBytes) : '--'}
            <span className="unit">{status.data ? `/ ${wholeGB(status.data.totalBytes)} GB` : 'GB'}</span>
          </div>
          <Spark color="var(--amber)" />
        </div>
      </div>

      <div className="section-label">Quick actions</div>
      <div className="qa-grid">
        {quickActions.map(({ label, sub, icon: Icon, primary, onClick }) => (
          <button key={label} className={`qa-tile${primary ? ' qa-primary' : ''}`} onClick={onClick}>
            <div className="icon-badge">
              <Icon />
            </div>
            <div className="qa-label">{label}</div>
            <div className="qa-sub">{sub}</div>
          </button>
        ))}
      </div>

      <div className="grid grid-2" style={{ marginTop: 22 }}>
        <div className="card">
          <div className="panel-head">
            <div className="icon-badge blue">
              <Monitor />
            </div>
            <h3>Connected Devices</h3>
            <div className="panel-head-actions">
              <Link to={pagePath('devices')}>View all →</Link>
            </div>
          </div>
          {devicesFailed ? (
            <ListError title="Could not load devices" error={devices.error} />
          ) : topDevices.length === 0 ? (
            <div style={{ fontSize: 12.5, color: 'var(--text-faint)' }}>No devices paired yet.</div>
          ) : (
            topDevices.map((d) => {
              const online = isOnline(d);
              return (
                <div className="dev-row" key={d.id}>
                  <div className="avatar">
                    {d.label.slice(0, 1).toUpperCase()}
                    <span className={`pulse-dot${online ? ' live' : ' off'}`} />
                  </div>
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <div style={{ fontSize: 13, fontWeight: 600 }}>{d.label}</div>
                    <div style={{ fontSize: 11.5, color: 'var(--text-faint)' }}>{formatAgo(d.lastSeen)}</div>
                  </div>
                  <span className={`badge ${online ? 'green' : 'neutral'}`}>{online ? 'Online' : 'Offline'}</span>
                </div>
              );
            })
          )}
        </div>

        <div className="card">
          <div className="panel-head">
            <div className="icon-badge violet">
              <ScrollText />
            </div>
            <h3>Recent Activity</h3>
            <div className="panel-head-actions">
              <Link to={pagePath('logs')}>Open logs →</Link>
            </div>
          </div>
          {logsFailed ? (
            <ListError title="Could not load logs" error={logs.error} />
          ) : recentLogs.length === 0 ? (
            <div style={{ fontSize: 12.5, color: 'var(--text-faint)' }}>No recent log lines.</div>
          ) : (
            recentLogs.map((line, i) => {
              const time = formatLogTime(line.ts);
              return (
                <div className="actlog-row" key={i}>
                  <span className="t">{time}</span>
                  <span>{line.message}</span>
                </div>
              );
            })
          )}
        </div>
      </div>

      <Dialog open={wakeOpen} onOpenChange={setWakeOpen} title="Wake a computer">
        <form
          style={{ display: 'flex', flexDirection: 'column', gap: 10 }}
          onSubmit={(e) => {
            e.preventDefault();
            if (isMac(wakeMac)) wake.mutate(wakeMac.trim());
          }}
        >
          <input type="text" autoFocus placeholder="MAC address, aa:bb:cc:dd:ee:ff" value={wakeMac} onChange={(e) => setWakeMac(e.target.value)} style={{ width: '100%' }} />
          <button className="btn btn-primary" type="submit" disabled={wake.isPending || !isMac(wakeMac)}>
            Send wake packet
          </button>
        </form>
      </Dialog>
    </div>
  );
}
