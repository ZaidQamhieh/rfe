import { create } from 'zustand';

import type { Entry } from '../../core/api/models';
import type { Host } from '../../core/models/host';

/**
 * Hand-off from the explorer to the /preview route: route params can't carry the sibling listing,
 * so the opener stores it here before navigating. `onChanged` lets the explorer refresh after a
 * delete or save from inside the preview.
 */
export type PreviewSession = { host: Host; entries: Entry[]; index: number; onChanged?: () => void };

export const usePreviewSession = create<{ session: PreviewSession | null; open: (s: PreviewSession) => void; clear: () => void }>((set) => ({
  session: null,
  open: (session) => set({ session }),
  clear: () => set({ session: null }),
}));

/** Text handed from the text viewer to the editor route (avoids a second fetch, like Flutter). `onSaved` also fires after a reload. */
export type EditorSession = { host: Host; entry: Entry; text: string; onSaved?: (e: Entry) => void };

export const useEditorSession = create<{ session: EditorSession | null; open: (s: EditorSession) => void }>((set) => ({
  session: null,
  open: (session) => set({ session }),
}));
