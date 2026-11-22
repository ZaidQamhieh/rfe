import { Image } from 'expo-image';
import { useEffect, useState, useSyncExternalStore, type ReactNode } from 'react';
import { View, type StyleProp, type ViewStyle } from 'react-native';

import type { Entry } from '../../core/api/models';
import { ignoreError } from '../../core/log';
import { thumbnails } from './thumbnails';

/** Whether this computer refused previews for lack of a download grant; re-renders when that changes. */
export function useThumbsDenied(hostId: string): boolean {
  return useSyncExternalStore(thumbnails.subscribeDenied, () => thumbnails.isDenied(hostId));
}

/** Bounded, de-duplicated, cancellable server thumbnail (see thumbnails.ts); falls back to the file-type icon. */
export function Thumbnail({ hostId, entry, size, style, fallback }: { hostId: string; entry: Entry; size: number; style?: StyleProp<ViewStyle>; fallback: ReactNode }) {
  const [uri, setUri] = useState<string | null>(null);
  useEffect(() => {
    let live = true;
    const req = thumbnails.request(hostId, entry.path, entry.modified ?? '', size);
    req.promise.then((u) => live && setUri(u)).catch(ignoreError('thumbnail'));
    return () => {
      live = false;
      req.cancel();
    };
  }, [hostId, entry.path, entry.modified, size]);
  if (!uri) return <>{fallback}</>;
  return (
    <View style={style}>
      <Image source={{ uri }} style={{ width: '100%', height: '100%', borderRadius: (style as { borderRadius?: number } | undefined)?.borderRadius }} contentFit="cover" recyclingKey={entry.path} transition={120} />
    </View>
  );
}
