// Pure pieces of the agent's transcoded-video endpoint (GET /v1/stream, HLS fMP4); contract: protocol/openapi.yaml.

/** The /health `features` entry that says the agent has ffmpeg and ffprobe and serves GET /stream. */
export const HLS_TRANSCODE_FEATURE = 'hlsTranscode';

/** Target heights the agent accepts for `quality`, lowest first; a smaller source is never upscaled. */
export const STREAM_QUALITIES = ['480p', '720p', '1080p', '1440p', '2160p'] as const;
export type StreamQuality = (typeof STREAM_QUALITIES)[number];

/**
 * The HLS playlist URL for one file at one quality. Only path and quality go here: the playlist the agent returns
 * names its own init and segment URLs relative to this one.
 */
export function streamUrl(address: string, remotePath: string, quality: StreamQuality): string {
  return `https://${address}/v1/stream?path=${encodeURIComponent(remotePath)}&quality=${quality}`;
}
