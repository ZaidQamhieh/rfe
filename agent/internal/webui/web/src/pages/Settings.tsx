import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { SlidersHorizontal } from 'lucide-react';
import { api, apiMessage, type AgentSettings } from '@/lib/api';
import { parseBandwidthLimit } from '@/lib/bandwidthInput';
import { rateParts } from '@/lib/format';
import { applyRootChange, bandwidthBody, type RootChange } from '@/lib/settingsEdit';
import { useToast } from '@/lib/toast-context';
import { ListError } from '@/components/ListError';

// Binary units (1 MB = 1024 * 1024 bytes), unlike Overview's decimal rate.
function formatRate(bytesPerSec: number): string {
  if (bytesPerSec <= 0) return 'Unlimited';
  const { value, unit } = rateParts(bytesPerSec, 1024);
  return `${value} ${unit}`;
}

export function Settings() {
  const { toast } = useToast();
  const qc = useQueryClient();
  const {
    data: settings,
    isLoading: settingsLoading,
    isError: settingsIsError,
    error: settingsError,
  } = useQuery({ queryKey: ['settings'], queryFn: api.getSettings });
  const {
    data: bandwidth,
    isLoading: bandwidthLoading,
    isError: bandwidthIsError,
    error: bandwidthError,
  } = useQuery({ queryKey: ['bandwidth'], queryFn: api.getBandwidth });

  const [newFolder, setNewFolder] = useState('');
  const [photoRoot, setPhotoRoot] = useState('');
  const [uploadInput, setUploadInput] = useState('');
  const [downloadInput, setDownloadInput] = useState('');
  // Which fields the user has typed in. A field that is not edited follows the agent's value; an edited one is kept
  // through refetches, and only an edited one is sent on save.
  const edited = useRef({ photoRoot: false, upload: false, download: false });
  // Allowed-folder changes run one at a time, each on the folder list read just before it is sent.
  const rootsQueue = useRef<Promise<void>>(Promise.resolve());

  useEffect(() => {
    if (settings && !edited.current.photoRoot) setPhotoRoot(settings.photoBackupRoot);
  }, [settings]);
  useEffect(() => {
    if (!bandwidth) return;
    if (!edited.current.upload) setUploadInput(bandwidth.maxUploadBytesPerSec ? String(bandwidth.maxUploadBytesPerSec) : '');
    if (!edited.current.download) setDownloadInput(bandwidth.maxDownloadBytesPerSec ? String(bandwidth.maxDownloadBytesPerSec) : '');
  }, [bandwidth]);

  const patchSettings = useMutation({
    mutationFn: (body: Partial<AgentSettings>) => api.patchSettings(body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settings'] }),
    onError: (err) => toast(apiMessage(err, 'Failed to update settings')),
  });

  function toggleReadOnly() {
    if (!settings) return;
    patchSettings.mutate({ readOnly: !settings.readOnly }, { onSuccess: () => toast('Settings updated') });
  }

  /** Applies [change] to the agent's folder list as it is now, after any earlier change has finished, so two quick clicks both stick. */
  function changeRoots(change: RootChange, done: string) {
    rootsQueue.current = rootsQueue.current.then(async () => {
      try {
        const latest = await qc.fetchQuery({ queryKey: ['settings'], queryFn: api.getSettings, staleTime: 0 });
        await api.patchSettings({ roots: applyRootChange(latest.roots, change) });
        toast(done);
      } catch (err) {
        toast(apiMessage(err, 'Failed to update settings'));
      }
      await qc.invalidateQueries({ queryKey: ['settings'] });
    });
  }

  function removeFolder(path: string) {
    changeRoots({ remove: path }, 'Folder removed');
  }

  function addFolder() {
    const path = newFolder.trim();
    if (!path) return;
    changeRoots({ add: path }, 'Folder added');
    setNewFolder('');
  }

  async function saveChanges() {
    if (!settings || !bandwidth) return;
    // Send only what the user edited, so saving never overwrites a value another tab or the app set meanwhile.
    const sent = { ...edited.current };
    const upload = sent.upload ? parseBandwidthLimit(uploadInput) : undefined;
    const download = sent.download ? parseBandwidthLimit(downloadInput) : undefined;
    if (upload === null || download === null) {
      toast('Limits must be whole numbers of bytes per second, or empty for unlimited');
      return;
    }
    const rootChanged = sent.photoRoot && photoRoot !== settings.photoBackupRoot;
    try {
      // The PUT replaces both limits, so the one the user left alone is read from the agent right now.
      const latest = sent.upload || sent.download ? await api.getBandwidth() : bandwidth;
      const body = bandwidthBody(latest, { upload, download });
      const bandwidthChanged = body !== null;
      await Promise.all([
        body ? api.putBandwidth(body) : null,
        rootChanged ? api.patchSettings({ photoBackupRoot: photoRoot }) : null,
      ]);
      edited.current = { photoRoot: false, upload: false, download: false };
      qc.invalidateQueries({ queryKey: ['bandwidth'] });
      qc.invalidateQueries({ queryKey: ['settings'] });
      toast(bandwidthChanged || rootChanged ? 'Changes saved' : 'Nothing to save');
    } catch (err) {
      toast(apiMessage(err, 'Failed to save changes'));
    }
  }

  const loadFailed = (settingsIsError && !settings) || (bandwidthIsError && !bandwidth);
  if (loadFailed) {
    return <ListError title="Could not load settings" error={settingsIsError && !settings ? settingsError : bandwidthError} />;
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
              onChange={(e) => {
                edited.current.upload = true;
                setUploadInput(e.target.value);
              }}
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
              onChange={(e) => {
                edited.current.download = true;
                setDownloadInput(e.target.value);
              }}
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
              onChange={(e) => {
                edited.current.photoRoot = true;
                setPhotoRoot(e.target.value);
              }}
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
