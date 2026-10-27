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
import { api, type Metrics } from '@/lib/api';
import { useToast } from '@/lib/toast-context';

function formatAgo(unixSeconds: number): string {
  const s = Date.now() / 1000 - unixSeconds;
  if (s < 60) return 'just now';
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  return `${Math.floor(s / 86400)}d ago`;
}

function formatRate(bytesPerSec: number | null): { value: string; unit: string } {
  if (bytesPerSec == null) return { value: '--', unit: 'MB/s' };
  if (bytesPerSec >= 1e6) return { value: (bytesPerSec / 1e6).toFixed(1), unit: 'MB/s' };
  return { value: (bytesPerSec / 1e3).toFixed(0), unit: 'KB/s' };
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

  const metrics = useQuery({ queryKey: ['metrics'], queryFn: api.metrics, refetchInterval: 5000 });
  const status = useQuery({ queryKey: ['status'], queryFn: api.status, refetchInterval: 10000 });
  const devices = useQuery({ queryKey: ['devices'], queryFn: api.listDevices, refetchInterval: 15000 });
  const logs = useQuery({ queryKey: ['logs'], queryFn: api.listLogs, refetchInterval: 8000 });

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

  const restart = useMutation({
    mutationFn: api.restartAgent,
    onSuccess: () => toast('Agent restart requested'),
    onError: () => toast('Restart failed'),
  });

  const rate = formatRate(netRate);
  const usedBytes = status.data ? status.data.totalBytes - status.data.freeBytes : undefined;

  const quickActions = [
    { label: 'Pair device', sub: 'POST /v1/pairing/generate', icon: KeyRound, primary: true, onClick: () => navigate('/app/devices') },
    { label: 'New folder', sub: 'POST /v1/fs/folder', icon: FolderPlus, onClick: () => navigate('/app/files') },
    { label: 'Upload file', sub: 'POST /v1/transfers', icon: Upload, onClick: () => navigate('/app/files') },
    { label: 'Share link', sub: 'POST /v1/share/mint', icon: Share2, onClick: () => navigate('/app/files') },
    {
      label: 'Wake device',
      sub: 'POST /v1/wol',
      icon: Power,
      onClick: () => {
        toast('Pick a device on the Devices page to wake it');
        navigate('/app/devices');
      },
    },
    {
      label: 'Restart agent',
      sub: 'POST /v1/agent/restart',
      icon: RotateCw,
      onClick: () => restart.mutate(),
    },
  ];

  const topDevices = (devices.data ?? []).slice(0, 3);
  const recentLogs = (logs.data ?? []).slice(-5).reverse();

  return (
    <div>
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
            {usedBytes != null ? (usedBytes / 1e9).toFixed(0) : '--'}
            <span className="unit">{status.data ? `/ ${(status.data.totalBytes / 1e9).toFixed(0)} GB` : 'GB'}</span>
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
              <Link to="/app/devices">View all →</Link>
            </div>
          </div>
          {topDevices.length === 0 ? (
            <div style={{ fontSize: 12.5, color: 'var(--text-faint)' }}>No devices paired yet.</div>
          ) : (
            topDevices.map((d) => {
              const online = Date.now() / 1000 - d.lastSeen < 300;
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
              <Link to="/app/logs">Open logs →</Link>
            </div>
          </div>
          {recentLogs.length === 0 ? (
            <div style={{ fontSize: 12.5, color: 'var(--text-faint)' }}>No recent log lines.</div>
          ) : (
            recentLogs.map((line, i) => {
              const t = new Date(line.ts);
              const time = Number.isNaN(t.getTime()) ? line.ts : t.toLocaleTimeString();
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
    </div>
  );
}
