/** Local, non-secret id for a transfer journal entry. [prefix] tells the kind apart (`u` upload, `d` download). */
export const newTransferId = (prefix: string) => `${prefix}${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;
