import type { EntryDensity } from '../core/models/sort';
import { useSettings } from './settings';

/** The app-wide Comfortable / Compact choice, for lists that are not tied to one computer's own view settings. */
export function useAppDensity(): EntryDensity {
  return useSettings((s) => s.state.app.density);
}
