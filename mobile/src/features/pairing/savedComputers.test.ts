import type { Host } from '../../core/models/host';
import { savedHostAt } from './savedComputers';

const hosts: Host[] = [
  { id: 'a', label: 'Studio', address: '192.168.1.20:8765', tailscaleAddress: '100.64.0.7:8765' },
  { id: 'b', label: 'Laptop', address: '192.168.1.30:8765', internetAddress: 'Files.Example.com:8443' },
];

describe('savedHostAt', () => {
  it('finds a saved computer by its main, Tailscale or direct address', () => {
    expect(savedHostAt(hosts, '192.168.1.20:8765')?.id).toBe('a');
    expect(savedHostAt(hosts, '100.64.0.7:8765')?.id).toBe('a');
    expect(savedHostAt(hosts, ' files.example.com:8443 ')?.id).toBe('b');
  });
  it('is undefined for an address that is not saved', () => {
    expect(savedHostAt(hosts, '192.168.1.99:8765')).toBeUndefined();
    expect(savedHostAt([], '192.168.1.20:8765')).toBeUndefined();
  });
});
