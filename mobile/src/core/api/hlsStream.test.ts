import { AgentClient, type Transport } from './agentClient';
import { HLS_TRANSCODE_FEATURE, STREAM_QUALITIES, streamUrl } from './hlsStream';
import type { Host } from '../models/host';

const PIN = 'ab'.repeat(32);
const host: Host = { id: 'h1', label: 'PC', address: '192.168.1.2:8765' };
const transport: Transport = { async request() { throw new Error('no network in this test'); } };

describe('HLS stream URL', () => {
  it('lists the five qualities the agent accepts, lowest first', () => {
    expect(STREAM_QUALITIES).toEqual(['480p', '720p', '1080p', '1440p', '2160p']);
    expect(HLS_TRANSCODE_FEATURE).toBe('hlsTranscode');
  });

  it('builds the playlist URL with the path and quality escaped', () => {
    expect(streamUrl('192.168.1.2:8765', '/Movies/a b&c.mkv', '1080p')).toBe(
      'https://192.168.1.2:8765/v1/stream?path=%2FMovies%2Fa%20b%26c.mkv&quality=1080p',
    );
  });

  it('adds neither init nor seg: those are the playlist\'s own relative URIs', () => {
    expect(streamUrl('h:1', '/a.mp4', '480p')).not.toMatch(/init|seg/);
  });
});

describe('AgentClient.streamSpec', () => {
  it('carries the pinned bearer headers and the pin, like downloadSpec', () => {
    const c = new AgentClient(host, { transport, deviceToken: 'tok', pinnedFingerprint: PIN });
    const spec = c.streamSpec('/v/x.mp4', '720p');
    expect(spec.url).toBe('https://192.168.1.2:8765/v1/stream?path=%2Fv%2Fx.mp4&quality=720p');
    expect(spec.headers.Authorization).toBe('Bearer tok');
    expect(spec.pin).toBe(PIN);
    expect(spec.headers).toEqual(c.downloadSpec('/v/x.mp4').headers);
  });
});
