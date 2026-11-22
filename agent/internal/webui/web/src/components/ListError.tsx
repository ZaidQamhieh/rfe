import { apiMessage } from '@/lib/api';

/**
 * The error placeholder for a list whose query failed with nothing to show. Callers render it only when the query has
 * no data, so a failed refetch keeps the last list on screen.
 */
export function ListError({ title, error }: { title: string; error: unknown }) {
  return (
    <div className="empty">
      <h4>{title}</h4>
      <p>{apiMessage(error, 'The agent did not answer.')}</p>
    </div>
  );
}
