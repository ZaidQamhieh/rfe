/** The default for every injectable `sleep`/`wait` option. */
export const sleep = (ms: number): Promise<void> => new Promise((r) => setTimeout(r, ms));
