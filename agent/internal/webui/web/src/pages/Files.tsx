import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import * as DropdownMenu from '@radix-ui/react-dropdown-menu';
import type { ColumnDef } from '@tanstack/react-table';
import {
  Folder,
  FolderPlus,
  Upload,
  Search,
  MoreVertical,
  Trash2,
  RotateCcw,
  Link as LinkIcon,
  CheckSquare,
  Image,
  FileText,
  Archive,
  File,
  Pencil,
  Download,
  Check,
} from 'lucide-react';
import { api, ApiError, contentUrl, type Entry, type ShareLink, type ShareLinkSummary, type TrashEntry } from '@/lib/api';
import { restoreFailure } from '@/lib/requests';
import { useToast } from '@/lib/toast-context';
import { DataTable } from '@/components/DataTable';
import { Dialog } from '@/components/Dialog';

type Tab = 'browse' | 'recent' | 'favorites' | 'trash' | 'shares';
const TABS: { id: Tab; label: string }[] = [
  { id: 'browse', label: 'Browse' },
  { id: 'recent', label: 'Recent' },
  { id: 'favorites', label: 'Favorites' },
  { id: 'trash', label: 'Trash' },
  { id: 'shares', label: 'Shares' },
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

function formatDate(iso?: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (isNaN(d.getTime())) return '—';
  return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' });
}

function joinPath(base: string, name: string): string {
  return base === '/' || base === '' ? `/${name}` : `${base}/${name}`;
}

function crumbsOf(path: string): { label: string; full: string }[] {
  const segs = path.split('/').filter(Boolean);
  const out: { label: string; full: string }[] = [];
  let acc = '';
  for (const s of segs) {
    acc = `${acc}/${s}`;
    out.push({ label: s, full: acc });
  }
  return out;
}

function fileIconFor(entry: Entry): { Icon: typeof File; cls: string } {
  if (entry.isDir) return { Icon: Folder, cls: 'blue' };
  const mime = entry.mimeType ?? '';
  if (mime.includes('image')) return { Icon: Image, cls: 'violet' };
  if (mime.includes('pdf')) return { Icon: FileText, cls: 'amber' };
  if (mime.includes('zip') || mime.includes('archive') || mime.includes('compressed')) return { Icon: Archive, cls: 'red' };
  return { Icon: File, cls: '' };
}

export function Files() {
  const qc = useQueryClient();
  const { toast } = useToast();
  const [activeTab, setActiveTab] = useState<Tab>('browse');

  // ---------- Browse ----------
  const [path, setPath] = useState('/');
  const [entries, setEntries] = useState<Entry[]>([]);
  const [nextCursor, setNextCursor] = useState<string | undefined>(undefined);
  const [browseQuery, setBrowseQuery] = useState('');
  const [selecting, setSelecting] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [newFolderOpen, setNewFolderOpen] = useState(false);
  const [newFolderName, setNewFolderName] = useState('');
  const [renameTarget, setRenameTarget] = useState<Entry | null>(null);
  const [renameValue, setRenameValue] = useState('');
  const [shareResult, setShareResult] = useState<ShareLink | null>(null);

  const listing = useQuery({ queryKey: ['listing', path], queryFn: () => api.listDir(path), enabled: activeTab === 'browse' });
  const searchResult = useQuery({
    queryKey: ['fsearch', path, browseQuery],
    queryFn: () => api.search(browseQuery, path),
    enabled: activeTab === 'browse' && browseQuery.trim().length > 0,
  });

  useEffect(() => {
    if (listing.data) {
      setEntries(listing.data.entries);
      setNextCursor(listing.data.nextCursor ?? undefined);
    }
  }, [listing.data]);

  useEffect(() => {
    setSelected(new Set());
    setSelecting(false);
  }, [path]);

  async function loadMore() {
    if (!nextCursor) return;
    const more = await api.listDir(path, nextCursor);
    setEntries((prev) => [...prev, ...more.entries]);
    setNextCursor(more.nextCursor ?? undefined);
  }

  const mkdirMutation = useMutation({
    mutationFn: (name: string) => api.mkdir(joinPath(path, name)),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['listing', path] });
      toast('Folder created');
      setNewFolderOpen(false);
      setNewFolderName('');
    },
    onError: (err) => toast(err instanceof ApiError ? err.message : 'Failed to create folder'),
  });

  const renameMutation = useMutation({
    mutationFn: ({ p, newName }: { p: string; newName: string }) => api.rename(p, newName),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['listing', path] });
      toast('Renamed');
      setRenameTarget(null);
    },
    onError: (err) => toast(err instanceof ApiError ? err.message : 'Rename failed'),
  });

  const shareMutation = useMutation({
    mutationFn: (p: string) => api.mintShare(p),
    onSuccess: (data) => {
      setShareResult(data);
      qc.invalidateQueries({ queryKey: ['shares'] });
    },
    onError: (err) => toast(err instanceof ApiError ? err.message : 'Failed to create share link'),
  });

  async function downloadFile(entry: Entry) {
    try {
      const response = await fetch(contentUrl(entry.path), {
        headers: { 'X-RFE-Web-Session': '1' },
        credentials: 'same-origin',
      });
      if (!response.ok) {
        const data = await response.json().catch(() => ({}));
        throw new ApiError(response.status, data?.code ?? 'UNKNOWN', data?.message ?? response.statusText);
      }

      // Never navigate the browser to host-controlled file content. Fetch with
      // the device credential, then trigger a save from a download-only blob.
      const objectURL = URL.createObjectURL(await response.blob());
      const link = document.createElement('a');
      link.href = objectURL;
      link.download = entry.name;
      link.hidden = true;
      document.body.appendChild(link);
      link.click();
      link.remove();
      window.setTimeout(() => URL.revokeObjectURL(objectURL), 60_000);
    } catch (err) {
      toast(err instanceof ApiError ? err.message : 'Download failed');
    }
  }

  function toggleSelect(p: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(p)) next.delete(p);
      else next.add(p);
      return next;
    });
  }

  function bulkStub(endpoint: string) {
    toast(`${selected.size} item${selected.size === 1 ? '' : 's'} — wire to ${endpoint}`);
  }

  async function onUploadPicked(files: FileList | null) {
    if (!files || files.length === 0) return;
    for (const file of Array.from(files)) {
      const dest = `${path.replace(/\/$/, '')}/${file.name}`;
      try {
        await api.uploadFile(dest, file);
        toast(`Uploaded ${file.name}`);
      } catch (err) {
        toast(err instanceof ApiError ? err.message : `Upload failed: ${file.name}`);
      }
    }
    qc.invalidateQueries({ queryKey: ['listing'] });
  }

  const rows = browseQuery.trim() ? searchResult.data ?? [] : entries;

  const browseColumns: ColumnDef<Entry, any>[] = [
    ...(selecting
      ? [
          {
            id: 'select',
            header: '',
            cell: ({ row }: any) => (
              <span
                className={`fcheck${selected.has(row.original.path) ? ' checked' : ''}`}
                onClick={(e) => {
                  e.stopPropagation();
                  toggleSelect(row.original.path);
                }}
              >
                {selected.has(row.original.path) && <Check />}
              </span>
            ),
            meta: { width: 36 },
          } as ColumnDef<Entry, any>,
        ]
      : []),
    {
      id: 'name',
      header: 'Name',
      cell: ({ row }) => {
        const entry = row.original;
        const { Icon, cls } = fileIconFor(entry);
        return (
          <div
            className="tcell"
            style={{ cursor: entry.isDir ? 'pointer' : 'default' }}
            onClick={() => entry.isDir && setPath(entry.path)}
          >
            <div className={`trow-icon${cls ? ` ${cls}` : ''}`}>
              <Icon />
            </div>
            {entry.name}
          </div>
        );
      },
    },
    { id: 'size', header: 'Size', cell: ({ row }) => (row.original.isDir ? '—' : formatBytes(row.original.size)) },
    { id: 'modified', header: 'Modified', cell: ({ row }) => formatDate(row.original.modified) },
    {
      id: 'actions',
      header: '',
      meta: { width: 44 },
      cell: ({ row }) => {
        const entry = row.original;
        return (
          <DropdownMenu.Root>
            <DropdownMenu.Trigger asChild>
              <button className="iconbtn" onClick={(e) => e.stopPropagation()}>
                <MoreVertical />
              </button>
            </DropdownMenu.Trigger>
            <DropdownMenu.Portal>
              <DropdownMenu.Content
                align="end"
                sideOffset={4}
                className="card"
                style={{ padding: 6, minWidth: 170, zIndex: 150, boxShadow: 'var(--shadow-2)' }}
              >
                <DropdownMenu.Item asChild>
                  <button
                    className="cmdk-item"
                    style={{ width: '100%' }}
                    onClick={() => {
                      setRenameTarget(entry);
                      setRenameValue(entry.name);
                    }}
                  >
                    <Pencil /> Rename
                  </button>
                </DropdownMenu.Item>
                {!entry.isDir && (
                  <DropdownMenu.Item asChild>
                    <button
                      className="cmdk-item"
                      style={{ width: '100%', textDecoration: 'none' }}
                      onClick={() => void downloadFile(entry)}
                    >
                      <Download /> Download
                    </button>
                  </DropdownMenu.Item>
                )}
                <DropdownMenu.Item asChild>
                  <button className="cmdk-item" style={{ width: '100%' }} onClick={() => shareMutation.mutate(entry.path)}>
                    <LinkIcon /> Share link
                  </button>
                </DropdownMenu.Item>
              </DropdownMenu.Content>
            </DropdownMenu.Portal>
          </DropdownMenu.Root>
        );
      },
    },
  ];

  // ---------- Recent ----------
  const recent = useQuery({ queryKey: ['recent'], queryFn: api.recent, enabled: activeTab === 'recent' });
  const recentColumns: ColumnDef<Entry, any>[] = [
    {
      id: 'name',
      header: 'Name',
      cell: ({ row }) => {
        const { Icon, cls } = fileIconFor(row.original);
        return (
          <div className="tcell">
            <div className={`trow-icon${cls ? ` ${cls}` : ''}`}>
              <Icon />
            </div>
            {row.original.name}
          </div>
        );
      },
    },
    { id: 'modified', header: 'Opened', cell: ({ row }) => formatDate(row.original.modified) },
    { id: 'size', header: 'Size', cell: ({ row }) => (row.original.isDir ? '—' : formatBytes(row.original.size)) },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) =>
        !row.original.isDir ? (
          <button className="btn btn-ghost btn-sm" onClick={() => void downloadFile(row.original)}>
            Download
          </button>
        ) : null,
    },
  ];

  // ---------- Favorites (no favorites endpoint — show configured roots) ----------
  const settings = useQuery({ queryKey: ['settings'], queryFn: api.getSettings, enabled: activeTab === 'favorites' });

  // ---------- Trash ----------
  const trash = useQuery({ queryKey: ['trash'], queryFn: api.listTrash, enabled: activeTab === 'trash' });
  const restoreMutation = useMutation({
    mutationFn: async (id: string) => {
      // The agent answers 200 even when the item could not be restored, so read the per-item result.
      const failure = restoreFailure(await api.restoreTrash([id]));
      if (failure) throw new Error(failure);
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['trash'] });
      toast('Restored');
    },
    onError: (err) => toast(err instanceof Error && err.message ? err.message : 'Restore failed'),
  });
  const deleteForeverMutation = useMutation({
    mutationFn: (id: string) => api.deleteTrashForever([id]),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['trash'] });
      toast('Deleted forever');
    },
    onError: (err) => toast(err instanceof ApiError ? err.message : 'Delete failed'),
  });
  async function emptyTrash() {
    if (!trash.data || trash.data.length === 0) return;
    if (!window.confirm(`Permanently delete all ${trash.data.length} item(s) in trash?`)) return;
    try {
      await api.emptyTrash();
      toast('Trash emptied');
    } catch (err) {
      toast(err instanceof ApiError ? err.message : 'Could not empty the trash');
    }
    qc.invalidateQueries({ queryKey: ['trash'] });
  }
  const trashColumns: ColumnDef<TrashEntry, any>[] = [
    {
      id: 'name',
      header: 'Name',
      cell: ({ row }) => (
        <div className="tcell">
          <div className={`trow-icon${row.original.isDir ? ' blue' : ''}`}>{row.original.isDir ? <Folder /> : <File />}</div>
          {row.original.name}
        </div>
      ),
    },
    { id: 'deleted', header: 'Deleted', cell: ({ row }) => formatDate(row.original.deletedAt) },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) => (
        <div style={{ display: 'flex', gap: 6 }}>
          <button className="iconbtn" title="Restore" onClick={() => restoreMutation.mutate(row.original.id)}>
            <RotateCcw />
          </button>
          <button
            className="iconbtn"
            title="Delete forever"
            onClick={() => {
              if (window.confirm(`Permanently delete "${row.original.name}"?`)) deleteForeverMutation.mutate(row.original.id);
            }}
          >
            <Trash2 />
          </button>
        </div>
      ),
    },
  ];

  // ---------- Shares ----------
  const shares = useQuery({ queryKey: ['shares'], queryFn: api.listShares, enabled: activeTab === 'shares' });
  const revokeMutation = useMutation({
    mutationFn: (tokenHash: string) => api.revokeShare(tokenHash),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['shares'] });
      toast('Share revoked');
    },
    onError: (err) => toast(err instanceof ApiError ? err.message : 'Revoke failed'),
  });
  const shareColumns: ColumnDef<ShareLinkSummary, any>[] = [
    { id: 'path', header: 'File', cell: ({ row }) => row.original.path },
    { id: 'expires', header: 'Expires', cell: ({ row }) => new Date(row.original.expiresAt * 1000).toLocaleString() },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) => (
        <button className="btn btn-ghost btn-sm" onClick={() => revokeMutation.mutate(row.original.tokenHash)}>
          Revoke
        </button>
      ),
    },
  ];

  return (
    <div>
      <div className="panel-head">
        <div className="icon-badge blue">
          <Folder />
        </div>
        <h3>Files</h3>
      </div>
      <div className="segmented" style={{ marginBottom: 16 }}>
        {TABS.map((t) => (
          <button key={t.id} className={activeTab === t.id ? 'active' : ''} onClick={() => setActiveTab(t.id)}>
            {t.label}
          </button>
        ))}
      </div>

      {activeTab === 'browse' && (
        <>
          <div className="crumb">
            <button onClick={() => setPath('/')}>
              <b>root</b>
            </button>
            {crumbsOf(path).map((c, i, arr) => (
              <span key={c.full} style={{ display: 'contents' }}>
                <span>›</span>
                {i === arr.length - 1 ? <b>{c.label}</b> : <button onClick={() => setPath(c.full)}>{c.label}</button>}
              </span>
            ))}
          </div>

          <div className="toolbar">
            <div className="fsearch">
              <Search />
              <input placeholder="Search this folder…" value={browseQuery} onChange={(e) => setBrowseQuery(e.target.value)} />
            </div>
            <div className="grow" />
            <button className="btn btn-ghost btn-sm" onClick={() => setSelecting((s) => !s)}>
              <CheckSquare /> {selecting ? 'Done' : 'Select'}
            </button>
            <button className="btn btn-ghost btn-sm" onClick={() => setNewFolderOpen(true)}>
              <FolderPlus /> New folder
            </button>
            <label className="btn btn-primary btn-sm" style={{ cursor: 'pointer' }}>
              <Upload /> Upload
              <input type="file" multiple style={{ display: 'none' }} onChange={(e) => onUploadPicked(e.target.files)} />
            </label>
          </div>

          {selecting && selected.size > 0 && (
            <div className="bulkbar">
              <b>{selected.size} selected</b>
              <div className="sp" />
              <button className="btn btn-ghost btn-sm" onClick={() => bulkStub('/fs/move')}>
                Move
              </button>
              <button className="btn btn-ghost btn-sm" onClick={() => bulkStub('/fs/copy')}>
                Copy
              </button>
              <button className="btn btn-ghost btn-sm" onClick={() => bulkStub('/fs/compress')}>
                Compress
              </button>
              <button className="btn btn-ghost btn-sm" onClick={() => bulkStub('/fs/checksum')}>
                Checksum
              </button>
              <button className="btn btn-danger btn-sm" onClick={() => bulkStub('/fs/delete')}>
                Delete
              </button>
            </div>
          )}

          {listing.isLoading ? (
            <div className="empty">Loading…</div>
          ) : rows.length === 0 ? (
            <div className="empty">
              <div className="ico">
                <Folder />
              </div>
              <h4>Nothing here</h4>
              <p>{browseQuery.trim() ? 'No matches in this folder.' : 'This folder is empty.'}</p>
            </div>
          ) : (
            <DataTable data={rows} columns={browseColumns} />
          )}

          {!browseQuery.trim() && nextCursor && (
            <button className="btn btn-ghost btn-sm" style={{ marginTop: 12 }} onClick={loadMore}>
              Load more
            </button>
          )}
        </>
      )}

      {activeTab === 'recent' && (
        <>
          {recent.isLoading ? (
            <div className="empty">Loading…</div>
          ) : !recent.data || recent.data.length === 0 ? (
            <div className="empty">
              <h4>No recent files</h4>
              <p>Files you open will show up here.</p>
            </div>
          ) : (
            <DataTable data={recent.data} columns={recentColumns} />
          )}
        </>
      )}

      {activeTab === 'favorites' && (
        <>
          {!settings.data || settings.data.roots.length === 0 ? (
            <div className="empty">
              <h4>No shortcuts configured</h4>
              <p>Favorites aren't backed by the agent yet — showing configured allowed folders instead, and none are set.</p>
            </div>
          ) : (
            <div className="grid grid-3">
              {settings.data.roots.map((root) => (
                <button
                  key={root}
                  className="card"
                  style={{ textAlign: 'left', cursor: 'pointer', display: 'flex', gap: 10, alignItems: 'center' }}
                  onClick={() => {
                    setPath(root);
                    setActiveTab('browse');
                  }}
                >
                  <div className="trow-icon blue">
                    <Folder />
                  </div>
                  <span className="mono" style={{ fontSize: 12.5 }}>
                    {root}
                  </span>
                </button>
              ))}
            </div>
          )}
        </>
      )}

      {activeTab === 'trash' && (
        <>
          <div className="card" style={{ marginBottom: 14, fontSize: 12.5, color: 'var(--text-faint)' }}>
            Deleted items are kept here until permanently removed.
          </div>
          <div className="toolbar">
            <div className="grow" />
            <button className="btn btn-danger btn-sm" onClick={emptyTrash} disabled={!trash.data || trash.data.length === 0}>
              Empty trash
            </button>
          </div>
          {trash.isLoading ? (
            <div className="empty">Loading…</div>
          ) : !trash.data || trash.data.length === 0 ? (
            <div className="empty">
              <h4>Trash is empty</h4>
            </div>
          ) : (
            <DataTable data={trash.data} columns={trashColumns} />
          )}
        </>
      )}

      {activeTab === 'shares' && (
        <>
          {shares.isLoading ? (
            <div className="empty">Loading…</div>
          ) : !shares.data || shares.data.length === 0 ? (
            <div className="empty">
              <h4>No active share links</h4>
              <p>Mint a share link from a file's menu in Browse.</p>
            </div>
          ) : (
            <DataTable data={shares.data} columns={shareColumns} />
          )}
        </>
      )}

      <Dialog open={newFolderOpen} onOpenChange={setNewFolderOpen} title="New folder">
        <form
          style={{ display: 'flex', flexDirection: 'column', gap: 10 }}
          onSubmit={(e) => {
            e.preventDefault();
            if (newFolderName.trim()) mkdirMutation.mutate(newFolderName.trim());
          }}
        >
          <input
            type="text"
            autoFocus
            placeholder="Folder name"
            value={newFolderName}
            onChange={(e) => setNewFolderName(e.target.value)}
            style={{ width: '100%' }}
          />
          <button className="btn btn-primary" type="submit" disabled={mkdirMutation.isPending}>
            Create
          </button>
        </form>
      </Dialog>

      <Dialog open={!!renameTarget} onOpenChange={(o) => !o && setRenameTarget(null)} title="Rename">
        <form
          style={{ display: 'flex', flexDirection: 'column', gap: 10 }}
          onSubmit={(e) => {
            e.preventDefault();
            if (renameTarget && renameValue.trim()) renameMutation.mutate({ p: renameTarget.path, newName: renameValue.trim() });
          }}
        >
          <input type="text" autoFocus value={renameValue} onChange={(e) => setRenameValue(e.target.value)} style={{ width: '100%' }} />
          <button className="btn btn-primary" type="submit" disabled={renameMutation.isPending}>
            Rename
          </button>
        </form>
      </Dialog>

      <Dialog open={!!shareResult} onOpenChange={(o) => !o && setShareResult(null)} title="Share link created">
        {shareResult && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
            <p style={{ margin: 0, fontSize: 12, color: 'var(--text-faint)' }}>
              This link is shown once — copy it now, it can't be retrieved again.
            </p>
            <input type="text" readOnly value={shareResult.url} onFocus={(e) => e.currentTarget.select()} style={{ width: '100%' }} />
            <button
              className="btn btn-ghost"
              onClick={() => {
                navigator.clipboard.writeText(shareResult.url);
                toast('Link copied');
              }}
            >
              Copy link
            </button>
          </div>
        )}
      </Dialog>
    </div>
  );
}
