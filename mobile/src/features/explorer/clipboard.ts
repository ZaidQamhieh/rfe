import { create } from 'zustand';

export type ClipboardMode = 'copy' | 'cut';
export type FileClipboard = { paths: string[]; mode: ClipboardMode; hostId: string };

/** In-app file clipboard (never the system clipboard): copy or cut a set of remote paths on one host. */
export const useFileClipboard = create<{ clip: FileClipboard | null; copy(paths: string[], hostId: string): void; cut(paths: string[], hostId: string): void; clear(): void }>((set) => ({
  clip: null,
  copy: (paths, hostId) => paths.length > 0 && set({ clip: { paths, mode: 'copy', hostId } }),
  cut: (paths, hostId) => paths.length > 0 && set({ clip: { paths, mode: 'cut', hostId } }),
  clear: () => set({ clip: null }),
}));
