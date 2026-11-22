import { AgentApiError } from '../../core/api/agentClient';
import { t } from '../../i18n';
import { qualityChoices, resumePosition, transcodeErrorMessage } from './videoQuality';

describe('qualityChoices', () => {
  it('offers only Original when the agent does not advertise hlsTranscode', () => {
    expect(qualityChoices({ features: ['pairRequest'] })).toEqual(['original']);
  });

  it('offers only Original with no health reading (never guess a transcode exists)', () => {
    expect(qualityChoices(null)).toEqual(['original']);
    expect(qualityChoices(undefined)).toEqual(['original']);
  });

  it('offers Original then the five qualities with hlsTranscode', () => {
    expect(qualityChoices({ features: ['pairRequest', 'hlsTranscode'] })).toEqual(['original', '480p', '720p', '1080p', '1440p', '2160p']);
  });
});

describe('transcodeErrorMessage', () => {
  const api = (status: number, code: string) => new AgentApiError(status, code, 'raw agent text');

  it('maps the agent codes to plain strings', () => {
    expect(transcodeErrorMessage(api(501, 'TRANSCODE_UNAVAILABLE'))).toBe(t('transcodeUnavailable'));
    expect(transcodeErrorMessage(api(429, 'TRANSCODE_BUSY'))).toBe(t('transcodeBusy'));
    expect(transcodeErrorMessage(api(502, 'TRANSCODE_FAILED'))).toBe(t('transcodeFailed'));
  });

  it('maps by status when only the status is known (a player reports the HTTP code in its message)', () => {
    expect(transcodeErrorMessage(new Error('Response code: 501'))).toBe(t('transcodeUnavailable'));
    expect(transcodeErrorMessage(new Error('Response code: 429'))).toBe(t('transcodeBusy'));
    expect(transcodeErrorMessage(new Error('Response code: 502'))).toBe(t('transcodeFailed'));
    expect(transcodeErrorMessage({ message: 'Source error. HTTP 429' })).toBe(t('transcodeBusy'));
  });

  it('does not mistake a number inside another number or an unrelated error for a transcode status', () => {
    expect(transcodeErrorMessage(new Error('took 15012 ms'))).toBe(t('transcodeFailed'));
    expect(transcodeErrorMessage(undefined)).toBe(t('transcodeFailed'));
    expect(transcodeErrorMessage(api(404, 'NOT_FOUND'))).toBe(t('transcodeFailed'));
  });
});

describe('resumePosition', () => {
  it('keeps a valid position', () => {
    expect(resumePosition(83.5)).toBe(83.5);
  });
  it('falls back to the start for a missing or invalid one', () => {
    expect(resumePosition(NaN)).toBe(0);
    expect(resumePosition(-3)).toBe(0);
    expect(resumePosition(Infinity)).toBe(0);
    expect(resumePosition(undefined)).toBe(0);
  });
});
