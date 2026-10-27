import { BANDWIDTH_PRESETS, bandwidthOptions, nextAppCapabilities, nextFileGrants, patchDevice } from './hostSettingsLogic';
import type { Device } from '../../core/api/models';

describe('nextAppCapabilities', () => {
  it('turning the catalog off also turns launching off', () => {
    expect(nextAppCapabilities({ viewApps: true, launchApps: true }, { viewApps: false })).toEqual({ viewApps: false, launchApps: false });
  });
  it('launching cannot be turned on without the catalog', () => {
    expect(nextAppCapabilities({ viewApps: false, launchApps: false }, { launchApps: true })).toEqual({ viewApps: false, launchApps: false });
    expect(nextAppCapabilities({ viewApps: true, launchApps: false }, { launchApps: true })).toEqual({ viewApps: true, launchApps: true });
  });
  it('turning the catalog on keeps launching off unless it was already granted', () => {
    expect(nextAppCapabilities({ viewApps: false, launchApps: true }, { viewApps: true })).toEqual({ viewApps: true, launchApps: false });
    expect(nextAppCapabilities({ viewApps: true, launchApps: true }, { viewApps: true })).toEqual({ viewApps: true, launchApps: true });
  });
});

describe('nextFileGrants', () => {
  const base = { browse: true, download: false, upload: false, modify: false, delete: false, share: false };
  it('sends the whole set with one grant changed', () => {
    expect(nextFileGrants(base, { download: true })).toEqual({ ...base, download: true });
    expect(nextFileGrants(base, { browse: false })).toEqual({ ...base, browse: false });
  });
});

describe('bandwidth and device helpers', () => {
  it('lists an odd current limit ahead of the presets', () => {
    expect(bandwidthOptions(5 * 1024 * 1024)).toBe(BANDWIDTH_PRESETS);
    expect(bandwidthOptions(123)).toEqual([123, ...BANDWIDTH_PRESETS]);
  });
  it('patches one device only', () => {
    const list = [{ id: 'a', browse: false }, { id: 'b', browse: false }] as Device[];
    const out = patchDevice(list, 'b', { browse: true });
    expect(out.map((d) => d.browse)).toEqual([false, true]);
    expect(out[0]).toBe(list[0]);
  });
});
