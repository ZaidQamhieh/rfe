import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { SlidersHorizontal } from 'lucide-react';
import { api, ApiError } from '@/lib/api';
import { useToast } from '@/lib/toast-context';

function formatRate(bytesPerSec: number): string {
  if (bytesPerSec <= 0) return 'Unlimited';
  const mb = bytesPerSec / (1024 * 1024);
  return mb >= 1 ? `${mb.toFixed(1)} MB/s` : `${(bytesPerSec / 1024).toFixed(0)} KB/s`;
}

export function Settings() {
  const { toast } = useToast();
  const qc = useQueryClient();
  const { data: settings, isLoading: settingsLoading } = useQuery({ queryKey: ['settings'], queryFn: api.getSettings });
  const { data: bandwidth, isLoading: bandwidthLoading } = useQuery({ queryKey: ['bandwidth'], queryFn: api.getBandwidth });

  const [newFolder, setNewFolder] = useState('');
  const [photoRoot, setPhotoRoot] = useState('');
  const [uploadInput, setUploadInput] = useState('');
  const [downloadInput, setDownloadInput] = useState('');
  const seeded = useRef(false);

  // Seed local editable state once, when both queries have loaded — avoids
  // clobbering in-progress edits on background refetches.
  useEffect(() => {
    if (settings && bandwidth && !seeded.current) {
      setPhotoRoot(settings.photoBackupRoot);
      setUploadInput(bandwidth.maxUploadBytesPerSec ? String(bandwidth.maxUploadBytesPerSec) : '');
      setDownloadInput(bandwidth.maxDownloadBytesPerSec ? String(bandwidth.maxDownloadBytesPerSec) : '');
      seeded.current = true;
    }
  }, [settings, bandwidth]);

  const patchSettings = useMutation({
    mutationFn: (body: Partial<import('@/lib/api').AgentSettings>) => api.patchSettings(body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settings'] }),
    onError: (err) => toast(err instanceof ApiError ? err.message : 'Failed to update settings'),
  });

  function toggleReadOnly() {
    if (!settings) return;
    patchSettings.mutate({ readOnly: !settings.readOnly }, { onSuccess: () => toast('Settings updated') });
  }

  function removeFolder(path: string) {
    if (!settings) return;
    patchSettings.mutate(
      { roots: settings.roots.filter((r) => r !== path) },
      { onSuccess: () => toast('Folder removed') },
    );
  }

  function addFolder() {
    const path = newFolder.trim();
    if (!path || !settings) return;
    patchSettings.mutate({ roots: [...settings.roots, path] }, { onSuccess: () => toast('Folder added') });
    setNewFolder('');
  }

  async function saveChanges() {
    if (!settings || !bandwidth) return;
    // Send only what this form changed, so saving never overwrites a value another tab or the app set meanwhile.
    const upload = Number(uploadInput) || 0;
    const download = Number(downloadInput) || 0;
    const bandwidthChanged = upload !== bandwidth.maxUploadBytesPerSec || download !== bandwidth.maxDownloadBytesPerSec;
    const rootChanged = photoRoot !== settings.photoBackupRoot;
    try {
      await Promise.all([
        bandwidthChanged ? api.putBandwidth({ maxUploadBytesPerSec: upload, maxDownloadBytesPerSec: download }) : null,
        rootChanged ? api.patchSettings({ photoBackupRoot: photoRoot }) : null,
      ]);
      qc.invalidateQueries({ queryKey: ['bandwidth'] });
      qc.invalidateQueries({ queryKey: ['settings'] });
      toast(bandwidthChanged || rootChanged ? 'Changes saved' : 'Nothing to save');
    } catch (err) {
      toast(err instanceof ApiError ? err.message : 'Failed to save changes');
    }
  }

  if (settingsLoading || bandwidthLoading || !settings || !bandwidth) {
    return <div className="empty"><p>Loading settings…</p></div>;
  }

  return (
    <div>
      <div className="panel-head">
        <div className="icon-badge violet">
          <SlidersHorizontal />
        </div>
        <h3>Settings</h3>
      </div>
      <div className="grid grid-2">
      <div className="card">
        <div className="section-label">Access</div>
        <div className="field-row">
          <div className="field-main">
            <div className="field-title">Read-only mode</div>
            <div className="field-sub">Blocks writes, uploads, deletes and renames for all devices</div>
          </div>
          <div className={`switch${settings.readOnly ? ' on' : ''}`} onClick={toggleReadOnly} role="switch" aria-checked={settings.readOnly} />
        </div>

        <div className="section-label">Allowed folders</div>
        {settings.roots.length === 0 && <div className="field-sub" style={{ padding: '8px 0' }}>No root restrictions — the whole filesystem is reachable.</div>}
        {settings.roots.map((root) => (
          <div className="field-row" key={root}>
            <div className="field-title mono">{root}</div>
            <button className="btn btn-danger btn-sm" onClick={() => removeFolder(root)}>Remove</button>
          </div>
        ))}
        <div className="field-row">
          <input
            type="text"
            placeholder="/path/to/folder"
            value={newFolder}
            onChange={(e) => setNewFolder(e.target.value)}
            style={{ flex: 1 }}
          />
          <button className="btn btn-ghost btn-sm" disabled={!newFolder.trim()} onClick={addFolder}>Add folder</button>
        </div>
      </div>

      <div className="card">
        <div className="section-label">Bandwidth</div>
        <div className="field-row">
          <div className="field-main">
            <div className="field-title">Upload limit</div>
            <div className="field-sub">{formatRate(bandwidth.maxUploadBytesPerSec)}</div>
          </div>
          <input
            type="number"
            min={0}
            placeholder="Unlimited"
            value={uploadInput}
            onChange={(e) => setUploadInput(e.target.value)}
            style={{ width: 140 }}
          />
        </div>
        <div className="field-row">
          <div className="field-main">
            <div className="field-title">Download limit</div>
            <div className="field-sub">{formatRate(bandwidth.maxDownloadBytesPerSec)}</div>
          </div>
          <input
            type="number"
            min={0}
            placeholder="Unlimited"
            value={downloadInput}
            onChange={(e) => setDownloadInput(e.target.value)}
            style={{ width: 140 }}
          />
        </div>

        <div className="section-label">Photo backup</div>
        <div className="field-row">
          <div className="field-title">Destination</div>
          <input
            type="text"
            placeholder="/Photos/Mobile Backup"
            value={photoRoot}
            onChange={(e) => setPhotoRoot(e.target.value)}
            style={{ flex: 1 }}
          />
        </div>
        {!photoRoot.trim() && (
          <div className="card" style={{ background: 'var(--red-tint)', marginTop: 10 }}>
            <div style={{ fontSize: 12, color: 'var(--red)' }}>
              Not set — phone photo backups will fail until a destination is chosen.
            </div>
          </div>
        )}

        <button className="btn btn-primary btn-sm" style={{ marginTop: 16 }} onClick={saveChanges}>
          Save changes
        </button>
      </div>
      </div>
    </div>
  );
}
