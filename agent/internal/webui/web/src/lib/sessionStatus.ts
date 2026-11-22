import { ApiError } from './api';

/**
 * True only when the agent itself refused the session (HTTP 401 or 403). A network failure or a server error says
 * nothing about the session, so the caller keeps it and offers a retry instead.
 */
export function isSessionRefused(err: unknown): boolean {
  return err instanceof ApiError && (err.status === 401 || err.status === 403);
}
