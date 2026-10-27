import { File, Paths } from 'expo-file-system';
import { useEffect, useRef, useState } from 'react';

import { statusError } from '../../core/api/agentClient';
import type { Entry } from '../../core/api/models';
import type { Host } from '../../core/models/host';
import { fetchToFileNative } from '../../core/native';
import { clientForHost } from '../../services';
import { humanizeError } from '../pairing/pairingService';
import { hashKey } from '../explorer/thumbnails';
import { FOLLOW_CHUNK_BYTES, followChunk, followStep } from './followLogic';
import { NotTextError } from './textDecode';
import { t } from '../../i18n';

const POLL_MS = 2000;

/**
 * While [enabled], polls the host for [entry]'s size every couple of seconds and fetches only the bytes appended
 * since [baseBytes] (the size of the copy already shown) with a Range request, like `tail -f`. Returns the text
 * appended so far, and an error message once following had to stop (file truncated, not text, host unreachable).
 */
export function useFollowText(host: Host, entry: Entry, enabled: boolean, baseBytes: number | null) {
  // What was appended, and how many host bytes are already shown, belong to one copy of one file: a reload (new
  // [baseBytes]) starts over, while switching Follow off and on again carries on from where it was.
  const key = `${entry.path}@${baseBytes}`;
  const [acc, setAcc] = useState<{ key: string; text: string; stopped: string | null }>({ key, text: '', stopped: null });
  const progress = useRef<{ key: string; known: number }>({ key, known: baseBytes ?? 0 });
  const shown = acc.key === key ? acc : { key, text: '', stopped: null };

  useEffect(() => {
    if (!enabled || baseBytes === null) return;
    if (progress.current.key !== key) progress.current = { key, known: baseBytes };
    let live = true;
    let busy = false;
    const stop = (message: string) => {
      clearInterval(timer);
      setAcc((a) => ({ key, text: a.key === key ? a.text : '', stopped: message }));
    };
    const tick = async () => {
      if (busy) return;
      busy = true;
      try {
        const client = await clientForHost(host);
        const known = progress.current.known;
        const step = followStep(known, (await client.meta(entry.path)).size ?? known);
        if (!live) return;
        if (step.kind === 'truncated') return stop(t('followTruncated'));
        if (step.kind === 'idle') return;
        const spec = client.downloadSpec(entry.path);
        const id = `f${hashKey(entry.path)}${Date.now().toString(36)}`;
        const part = new File(Paths.cache, `follow-${id}.part`);
        try {
          const r = await fetchToFileNative(id, spec.url, { ...spec.headers, Range: step.range }, spec.pin, decodeURIComponent(part.uri.replace('file://', '')), 20_000, FOLLOW_CHUNK_BYTES * 2);
          // 206 is the slice we asked for; a 200 would be the whole file again, which must never be appended.
          if (r.status === 416) return live ? stop(t('followTruncated')) : undefined;
          if (r.status !== 206) throw statusError(r.status);
          const { text, consumed } = followChunk(await part.bytes());
          if (!live) return;
          progress.current.known = known + consumed;
          if (text) setAcc((a) => ({ key, text: (a.key === key ? a.text : '') + text, stopped: null }));
        } finally {
          if (part.exists) part.delete();
        }
      } catch (e) {
        // Not text can never recover; a dropped connection or a busy host is retried on the next poll.
        if (live && e instanceof NotTextError) stop(humanizeError(e));
      } finally {
        busy = false;
      }
    };
    const timer = setInterval(() => void tick(), POLL_MS);
    return () => {
      live = false;
      clearInterval(timer);
    };
  }, [host, entry, enabled, baseBytes, key]);

  return { extra: shown.text, stopped: shown.stopped };
}
