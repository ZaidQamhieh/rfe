import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { RefreshCw } from 'lucide-react';
import { api } from '@/lib/api';
import { formatLogTime } from '@/lib/format';
import { REFRESH_INTERVAL_MS } from '@/lib/queries';
import { useToast } from '@/lib/toast-context';
import { ListError } from '@/components/ListError';

type Level = 'info' | 'warn' | 'error';
const FILTERS = ['all', 'info', 'warn', 'error'] as const;
type Filter = (typeof FILTERS)[number];

// ponytail: the agent's /logs endpoint is a plain journald tail with no real
// severity field (see splitLogLine's doc comment in webdata_handlers.go) —
// this is a best-effort substring heuristic for row coloring only, not a
// backend-reported level.
function classify(message: string): Level {
  const m = message.toLowerCase();
  if (m.includes('error') || m.includes('fail')) return 'error';
  if (m.includes('warn')) return 'warn';
  return 'info';
}

export function Logs() {
  const { toast } = useToast();
  const [filter, setFilter] = useState<Filter>('all');
  const { data, isLoading, isError, error, refetch } = useQuery({
    queryKey: ['logs'],
    queryFn: api.listLogs,
    refetchInterval: REFRESH_INTERVAL_MS,
  });

  async function handleRefresh() {
    const result = await refetch();
    toast(result.isError ? 'Refresh failed' : 'Refreshed');
  }

  const logs = data ?? [];
  const filtered = filter === 'all' ? logs : logs.filter((l) => classify(l.message) === filter);

  return (
    <div>
      <div className="toolbar">
        <div className="chip-row">
          {FILTERS.map((f) => (
            <span key={f} className={`chip${filter === f ? ' active' : ''}`} onClick={() => setFilter(f)}>
              {f === 'all' ? 'All' : f[0].toUpperCase() + f.slice(1)}
            </span>
          ))}
        </div>
        <div className="grow" />
        <button className="btn btn-ghost btn-sm" onClick={handleRefresh}>
          <RefreshCw /> Refresh
        </button>
      </div>

      {isLoading ? (
        <div className="empty"><p>Loading logs…</p></div>
      ) : isError && !data ? (
        <ListError title="Could not load logs" error={error} />
      ) : filtered.length === 0 ? (
        <div className="empty">
          <h4>No log lines</h4>
          <p>Nothing to show for this filter.</p>
        </div>
      ) : (
        <div className="logview">
          {filtered.map((l, i) => {
            const level = classify(l.message);
            const time = formatLogTime(l.ts);
            return (
              <div className={`log-row${level === 'info' ? '' : ` ${level}`}`} key={`${l.ts}-${i}`}>
                <span className="t">{time}</span>
                <span className="lvl">{level.toUpperCase()}</span>
                <span>{l.message}</span>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
