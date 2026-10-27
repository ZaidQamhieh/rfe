/** Options for `client.recent()`: limited to [root] when the Recent screen was opened from a folder. */
export const recentOptions = (root: string | undefined): { root?: string } => (root ? { root } : {});

/** Route params that open the Recent screen for one folder. */
export const recentRouteParams = (folder: string): { root: string } => ({ root: folder });
