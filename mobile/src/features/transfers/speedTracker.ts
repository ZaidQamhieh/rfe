import { formatSize } from '../../core/format';

type Sample = { t: number; bytes: number; rate: number | null };

/**
 * Smoothed transfer speed per id from the progress events the engine already emits. Samples closer than [minGapMs]
 * are folded into the next one so bursty events do not make the number jump; a byte count that goes backwards
 * (retry from a smaller offset) restarts the estimate.
 */
export class SpeedTracker {
  private readonly samples = new Map<string, Sample>();

  constructor(
    private readonly now: () => number = Date.now,
    private readonly alpha = 0.3,
    private readonly minGapMs = 500,
  ) {}

  /** Feeds one record and returns bytes/second, or null when there is no estimate (not running, or too few samples). */
  sample(id: string, received: number, running: boolean): number | null {
    if (!running) {
      this.samples.delete(id);
      return null;
    }
    const t = this.now();
    const last = this.samples.get(id);
    if (!last || received < last.bytes) {
      this.samples.set(id, { t, bytes: received, rate: null });
      return null;
    }
    const dt = t - last.t;
    if (dt < this.minGapMs) return last.rate;
    const inst = ((received - last.bytes) / dt) * 1000;
    const rate = last.rate === null ? inst : this.alpha * inst + (1 - this.alpha) * last.rate;
    this.samples.set(id, { t, bytes: received, rate });
    return rate;
  }
}

/** Seconds left at [speed] bytes/second, or null when it cannot be told. */
export function etaSeconds(total: number, received: number, speed: number | null): number | null {
  if (speed === null || speed <= 0 || total <= 0 || received >= total) return null;
  return (total - received) / speed;
}

export function formatEta(seconds: number): string {
  const s = Math.max(1, Math.round(seconds));
  if (s < 60) return `${s} s`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m} min`;
  const h = Math.floor(m / 60);
  const rem = m % 60;
  return rem === 0 ? `${h} h` : `${h} h ${rem} min`;
}

export const formatSpeed = (bytesPerSecond: number): string => `${formatSize(Math.round(bytesPerSecond))}/s`;
