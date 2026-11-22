// The app's pages, in sidebar order: the one list App.tsx routes, the sidebar, the top bar title and the command palette read.
import {
  LayoutGrid,
  Folder,
  Activity,
  Monitor,
  Users,
  ScrollText,
  Settings,
} from 'lucide-react';

export const PAGES = [
  { id: 'overview', title: 'Overview', icon: LayoutGrid },
  { id: 'files', title: 'Files', icon: Folder },
  { id: 'transfers', title: 'Transfers', icon: Activity },
  { id: 'devices', title: 'Devices', icon: Monitor },
  { id: 'users', title: 'Users', icon: Users },
  { id: 'logs', title: 'Logs', icon: ScrollText },
  { id: 'settings', title: 'Settings', icon: Settings },
] as const;

export type PageId = (typeof PAGES)[number]['id'];

/** The app's landing page; the router falls back to it and sign-in lands on it. */
export const APP_HOME = '/app/overview';

/** Absolute path of a page. */
export const pagePath = (id: PageId): string => `/app/${id}`;
