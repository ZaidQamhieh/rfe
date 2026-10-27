export type SortField = 'name' | 'size' | 'date' | 'type';
export type SortOrder = { field: SortField; ascending: boolean };
export const defaultSort = (): SortOrder => ({ field: 'name', ascending: true });
export type EntryDensity = 'comfortable' | 'compact';
