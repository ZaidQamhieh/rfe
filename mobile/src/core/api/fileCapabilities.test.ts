import { can, parseDevice, parseStatus } from './models';

describe('file capabilities', () => {
  it('reads what the agent says this device may do', () => {
    const s = parseStatus({ fileCapabilities: { browse: true, download: false, upload: true, modify: false } });
    expect(s.fileCapabilities).toEqual({ browse: true, download: false, upload: true, modify: false, delete: false, share: false });
    expect(can(s.fileCapabilities, 'upload')).toBe(true);
    expect(can(s.fileCapabilities, 'download')).toBe(false);
  });
  it('reads the Wake-on-LAN grant and a device with it off', () => {
    expect(parseStatus({ fileCapabilities: { browse: true, wake: true } }).fileCapabilities?.wake).toBe(true);
    expect(can(parseStatus({ fileCapabilities: { browse: true, wake: false } }).fileCapabilities, 'wake')).toBe(false);
    expect(parseDevice({ id: 'd', wake: true }).wake).toBe(true);
    expect(parseDevice({ id: 'd' }).wake).toBeUndefined();
  });
  it('an older agent (no field) hides nothing; the agent still enforces', () => {
    const s = parseStatus({});
    expect(s.fileCapabilities).toBeUndefined();
    expect(can(s.fileCapabilities, 'delete')).toBe(true);
  });
});
