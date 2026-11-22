import { useEffect, useState } from 'react';

import { transfers, type TransferRecord } from '../../core/native';
import { applyTransferUpdate } from '../transfers/transferLogic';

/**
 * Every transfer the native engine knows, kept current from its update events. Null until the first read; a failed
 * read becomes an empty list so the nav and the strip simply show nothing.
 */
export function useTransfers(): TransferRecord[] | null {
  const [items, setItems] = useState<TransferRecord[] | null>(null);
  useEffect(() => {
    let live = true;
    transfers.list().then(
      (l) => live && setItems((cur) => cur ?? l),
      () => live && setItems((cur) => cur ?? []),
    );
    const off = transfers.subscribe((r) => setItems((cur) => applyTransferUpdate(cur, r)));
    return () => {
      live = false;
      off();
    };
  }, []);
  return items;
}
