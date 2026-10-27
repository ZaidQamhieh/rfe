import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ColumnDef } from '@tanstack/react-table';
import { Search, Trash2, File as FileIcon, ArrowLeftRight } from 'lucide-react';
import { api, ApiError, type TransferRow } from '@/lib/api';
import { useToast } from '@/lib/toast-context';
import { DataTable } from '@/components/DataTable';

type StatusFilter = 'all' | 'active' | 'completed' | 'failed';
const FILTERS: { id: StatusFilter; label: string }[] = [
  { id: 'all', label: 'All' },
  { id: 'active', label: 'Active' },
  { id: 'completed', label: 'Completed' },
  { id: 'failed', label: 'Failed' },
];

function formatBytes(n: number): string {
  if (!n) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${i === 0 ? v : v.toFixed(1)} ${units[i]}`;
}

function timeAgo(unixSeconds: number): string {
  if (!unixSeconds) return 'Never';
  const diff = Date.now() / 1000 - unixSeconds;
  if (diff < 60) return `${Math.max(0, Math.floor(diff))}s ago`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  return `${Math.floor(diff / 86400)}d ago`;
}

function statusBadge(status: TransferRow['status']): { cls: string; label: string } {
  if (status === 'open') return { cls: 'blue', label: 'Active' };
  if (status === 'completed') return { cls: 'green', label: 'Completed' };
  return { cls: 'red', label: 'Failed' };
}

export function Transfers() {
  const qc = useQueryClient();
  const { toast } = useToast();
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all');
  const [deviceFilter, setDeviceFilter] = useState<string | undefined>(undefined);
  const [query, setQuery] = useState('');

  const transfers = useQuery({
    queryKey: ['transfers', deviceFilter],
    queryFn: () => api.listTransfers(deviceFilter ? { device: deviceFilter } : undefined),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.deleteTransfer(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['transfers'] });
      toast('Session cleared');
    },
    onError: (err) => toast(err instanceof ApiError ? err.message : 'Failed to clear session'),
  });

  const data = transfers.data;
  const deviceLabel = (id: string) => data?.devices.find((d) => d.id === id)?.label ?? id;

  const filtered = (data?.transfers ?? []).filter((t) => {
    if (statusFilter === 'active' && t.status !== 'open') return false;
    if (statusFilter === 'completed' && t.status !== 'completed') return false;
    if (statusFilter === 'failed' && t.status !== 'failed') return false;
    if (query.trim() && !t.name.toLowerCase().includes(query.trim().toLowerCase())) return false;
    return true;
  });

  const columns: ColumnDef<TransferRow, any>[] = [
    {
      id: 'name',
      header: 'File',
      cell: ({ row }) => (
        <div className="tcell">
          <div className="trow-icon">
            <FileIcon />
          </div>
          <div>
            {row.original.name}
            <div style={{ fontSize: 11, color: 'var(--text-faint)' }}>{formatBytes(row.original.totalSize)}</div>
          </div>
        </div>
      ),
    },
    { id: 'device', header: 'Device', cell: ({ row }) => deviceLabel(row.original.deviceId) },
    {
      id: 'progress',
      header: 'Progress',
      cell: ({ row }) => {
        const barColor =
          row.original.status === 'completed' ? 'var(--green)' : row.original.status === 'failed' ? 'var(--red)' : undefined;
        return (
          <div className="progress" style={{ width: 120 }}>
            <span style={{ width: `${row.original.progress}%`, background: barColor }} />
          </div>
        );
      },
    },
    {
      id: 'status',
      header: 'Status',
      cell: ({ row }) => {
        const b = statusBadge(row.original.status);
        return <span className={`badge ${b.cls}`}>{row.original.status === 'open' ? `${row.original.progress}%` : b.label}</span>;
      },
    },
    { id: 'time', header: 'Time', cell: ({ row }) => <span className="mono">{timeAgo(row.original.updatedAt)}</span> },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) =>
        row.original.status !== 'open' ? (
          <button
            className="iconbtn"
            title="Clear session"
            onClick={() => {
              if (window.confirm(`Clear transfer session for "${row.original.name}"?`)) deleteMutation.mutate(row.original.id);
            }}
          >
            <Trash2 />
          </button>
        ) : null,
    },
  ];

  return (
    <div>
      <div className="panel-head">
        <div className="icon-badge green">
          <ArrowLeftRight />
        </div>
        <h3>Transfers</h3>
      </div>
      <div className="grid grid-4" style={{ marginBottom: 18 }}>
        <div className="card stat-card">
          <div className="label">Active now</div>
          <div className="value" style={{ color: 'var(--primary)', gap: 8 }}>
            <span className="live-dot" /> {data?.activeNow ?? 0}
          </div>
        </div>
        <div className="card stat-card">
          <div className="label">Completed today</div>
          <div className="value" style={{ color: 'var(--green)' }}>{data?.counts['completed'] ?? 0}</div>
        </div>
        <div className="card stat-card">
          <div className="label">Failed</div>
          <div className="value" style={{ color: 'var(--red)' }}>{data?.counts['failed'] ?? 0}</div>
        </div>
        <div className="card stat-card">
          <div className="label">Total sessions</div>
          <div className="value">{data?.total ?? 0}</div>
        </div>
      </div>

      <div className="toolbar">
        <div className="segmented">
          {FILTERS.map((f) => (
            <button key={f.id} className={statusFilter === f.id ? 'active' : ''} onClick={() => setStatusFilter(f.id)}>
              {f.label}
              {f.id === 'failed' && (data?.counts['failed'] ?? 0) > 0 && (
                <span className="badge red" style={{ padding: '0 5px', marginLeft: 5 }}>{data?.counts['failed']}</span>
              )}
            </button>
          ))}
        </div>
        <div className="grow" />
        <div className="fsearch">
          <Search />
          <input placeholder="Search transfers…" value={query} onChange={(e) => setQuery(e.target.value)} />
        </div>
      </div>

      <div className="chip-row" style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginBottom: 16 }}>
        <span className={`chip${deviceFilter === undefined ? ' active' : ''}`} onClick={() => setDeviceFilter(undefined)}>
          All devices
        </span>
        {(data?.devices ?? []).map((d) => (
          <span key={d.id} className={`chip${deviceFilter === d.id ? ' active' : ''}`} onClick={() => setDeviceFilter(d.id)}>
            {d.label}
          </span>
        ))}
      </div>

      {transfers.isLoading ? (
        <div className="empty">Loading…</div>
      ) : filtered.length === 0 ? (
        <div className="empty">
          <h4>No transfers</h4>
          <p>Nothing matches this filter yet.</p>
        </div>
      ) : (
        <DataTable data={filtered} columns={columns} />
      )}
    </div>
  );
}
