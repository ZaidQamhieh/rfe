import { AgentApiError } from '../../core/api/agentClient';
import { HLS_TRANSCODE_FEATURE, STREAM_QUALITIES, type StreamQuality } from '../../core/api/hlsStream';
import { t } from '../../i18n';

/** A video source choice: the file itself (direct playback), or a transcode at one quality. */
export type VideoQualityChoice = 'original' | StreamQuality;

/**
 * The choices to list in the quality menu. Quality options appear only when the agent's health reading
 * advertises hlsTranscode; with no reading (offline, not loaded yet) only Original is offered.
 */
export function qualityChoices(health: { features: string[] } | null | undefined): VideoQualityChoice[] {
  const hasHls = Array.isArray(health?.features) && health.features.includes(HLS_TRANSCODE_FEATURE);
  return hasHls ? ['original', ...STREAM_QUALITIES] : ['original'];
}

/** Agent code or HTTP status to message, for the three ways GET /stream can refuse or fail. */
const BY_CODE: Record<string, () => string> = {
  TRANSCODE_UNAVAILABLE: () => t('transcodeUnavailable'),
  TRANSCODE_BUSY: () => t('transcodeBusy'),
  TRANSCODE_FAILED: () => t('transcodeFailed'),
};
const BY_STATUS: Record<number, () => string> = {
  501: BY_CODE.TRANSCODE_UNAVAILABLE,
  429: BY_CODE.TRANSCODE_BUSY,
  502: BY_CODE.TRANSCODE_FAILED,
};

/**
 * A plain message for a failed transcoded playback. An agent error is mapped by its code, then its status; any other
 * error (a player reports only text such as "Response code: 429") by a whole-number status in its message.
 * Anything unrecognised reads as the generic "could not convert" message.
 */
export function transcodeErrorMessage(e: unknown): string {
  if (e instanceof AgentApiError) return (BY_CODE[e.code] ?? BY_STATUS[e.statusCode] ?? BY_CODE.TRANSCODE_FAILED)();
  const text = typeof e === 'string' ? e : typeof e === 'object' && e !== null && 'message' in e ? String((e as { message: unknown }).message) : '';
  const status = /\b(501|429|502)\b/.exec(text);
  return (status ? BY_STATUS[Number(status[1])] : BY_CODE.TRANSCODE_FAILED)();
}

/** Where to restart after a source switch, in seconds: the old position when it is a usable number, else the start. */
export function resumePosition(seconds: number | undefined): number {
  return typeof seconds === 'number' && Number.isFinite(seconds) && seconds > 0 ? seconds : 0;
}
