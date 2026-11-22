import { useEffect } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { connectEvents, queryKeysFor } from './events';

/** While mounted, keeps the page fresh from the agent's event stream. */
export function useAgentEvents(): void {
  const qc = useQueryClient();
  useEffect(
    () =>
      connectEvents((kind) => {
        for (const queryKey of queryKeysFor(kind)) void qc.invalidateQueries({ queryKey });
      }),
    [qc],
  );
}
