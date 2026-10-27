import { Stack } from 'expo-router';
import { useCallback, useEffect, useState } from 'react';

import type { Host } from '../core/models/host';
import { ErrorRetry, Loading } from '../design/components';
import { CrossHostSearchScreen } from '../features/search/CrossHostSearchScreen';
import { humanizeError } from '../features/pairing/pairingService';
import { hostStore } from '../services';

/** Search every paired computer. */
export default function CrossHostSearch() {
  const [hosts, setHosts] = useState<Host[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let live = true;
    hostStore.listHosts().then(
      (l) => live && setHosts(l),
      // A failed read must not leave the spinner up for good.
      (e) => live && setError(humanizeError(e)),
    );
    return () => {
      live = false;
    };
  }, [attempt]);
  const retry = useCallback(() => {
    setError(null);
    setAttempt((n) => n + 1);
  }, []);
  let body: React.ReactNode;
  if (error !== null) body = <ErrorRetry message={error} onRetry={retry} />;
  else if (hosts === null) body = <Loading />;
  else body = <CrossHostSearchScreen hosts={hosts} />;
  return (
    <>
      <Stack.Screen options={{ headerShown: false }} />
      {body}
    </>
  );
}
