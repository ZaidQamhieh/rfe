import { Stack } from 'expo-router';
import { useEffect, useState } from 'react';

import type { Host } from '../core/models/host';
import { EmptyState, ErrorRetry, Loading } from '../design/components';
import { humanizeError } from '../core/humanizeError';
import { DualPaneScreen } from '../features/dual/DualPaneScreen';
import { t } from '../i18n';
import { hostStore } from '../services';

/** Two panes side by side: copy between two folders, on one computer or two. */
export default function DualPane() {
  const [hosts, setHosts] = useState<Host[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    let live = true;
    hostStore.listHosts().then(
      (l) => live && setHosts(l),
      (e) => live && setError(humanizeError(e)),
    );
    return () => {
      live = false;
    };
  }, []);
  return (
    <>
      <Stack.Screen options={{ title: t('dualTitle') }} />
      {error ? <ErrorRetry message={error} onRetry={() => setError(null)} /> : hosts === null ? <Loading /> : hosts.length === 0 ? <EmptyState message={t('dualNoComputers')} /> : <DualPaneScreen hosts={hosts} />}
    </>
  );
}
