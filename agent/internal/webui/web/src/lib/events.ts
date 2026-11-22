// Client of GET /v1/events (protocol/openapi.yaml): change hints that make the page refetch instead of poll.
// An event carries only a kind, never a path or content, so the handler just invalidates cached queries.

export type EventKind = 'hello' | 'transfers' | 'files' | 'settings' | 'devices' | 'audit';

/** The query keys to refetch when an event of this kind arrives. "hello" (connect or reconnect) refreshes everything. */
export function queryKeysFor(kind: string): string[][] {
  switch (kind) {
    case 'hello':
      return [['listing'], ['fsearch'], ['recent'], ['trash'], ['shares'], ['transfers'], ['devices'], ['logs'], ['settings'], ['status']];
    case 'files':
      return [['listing'], ['fsearch'], ['recent'], ['trash'], ['shares'], ['status']];
    case 'transfers':
      return [['transfers']];
    case 'settings':
      return [['settings']];
    case 'devices':
      return [['devices'], ['status']];
    case 'audit':
      return [['logs']];
    default:
      return [];
  }
}

/** Feeds text chunks in and calls onEvent(kind) for each complete event; comment lines (": ping") are ignored. */
export function createSseParser(onEvent: (kind: string) => void): (chunk: string) => void {
  let buf = '';
  let kind = '';
  return (chunk) => {
    buf += chunk;
    for (;;) {
      const nl = buf.indexOf('\n');
      if (nl < 0) return;
      const line = buf.slice(0, nl).replace(/\r$/, '');
      buf = buf.slice(nl + 1);
      if (line === '') {
        if (kind) onEvent(kind);
        kind = '';
      } else if (line.startsWith('event:')) {
        kind = line.slice(6).trim();
      }
    }
  };
}

const RETRY_MIN_MS = 1000;
const RETRY_MAX_MS = 30000;

/** Opens the stream and reconnects with backoff until stop() is called. 401/403/503 also retry, slowly. */
export function connectEvents(onEvent: (kind: string) => void): () => void {
  const ctl = new AbortController();
  let delay = RETRY_MIN_MS;
  const run = async () => {
    while (!ctl.signal.aborted) {
      try {
        const res = await fetch('/v1/events', {
          headers: { 'X-RFE-Web-Session': '1', Accept: 'text/event-stream' },
          credentials: 'same-origin',
          signal: ctl.signal,
        });
        if (res.ok && res.body) {
          delay = RETRY_MIN_MS;
          const parse = createSseParser(onEvent);
          const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
          for (;;) {
            const { value, done } = await reader.read();
            if (done) break;
            parse(value);
          }
        }
      } catch {
        if (ctl.signal.aborted) return;
      }
      await new Promise((r) => setTimeout(r, delay));
      delay = Math.min(delay * 2, RETRY_MAX_MS);
    }
  };
  void run();
  return () => ctl.abort();
}
