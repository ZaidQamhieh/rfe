import { CircleAlert, CircleCheck, Info } from 'lucide-react-native';
import * as Haptics from 'expo-haptics';
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { Animated, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { useScheme } from '../theme';
import { Radii } from '../tokens';
import { Pressable } from './Pressable';
import { Text } from './Text';

type Kind = 'success' | 'error' | 'info';
type Msg = { id: number; kind: Kind; text: string; retry?: () => void; label?: string };
type Api = { success(text: string, onUndo?: () => void): void; error(text: string, onRetry?: () => void): void; info(text: string): void };

const Ctx = createContext<Api>({ success() {}, error() {}, info() {} });
export const useToast = () => useContext(Ctx);

/** Floating snackbar (port of core/ui/feedback.dart): success green, error scheme.error, info inverse; light/heavy haptic. */
export function ToastProvider({ children }: { children: ReactNode }) {
  const c = useScheme();
  const insets = useSafeAreaInsets();
  const [msg, setMsg] = useState<Msg | null>(null);
  const opacity = useState(() => new Animated.Value(0))[0];
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);

  const show = useCallback(
    (kind: Kind, text: string, retry?: () => void, label?: string) => {
      if (kind === 'success') Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light).catch(() => {});
      if (kind === 'error') Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Heavy).catch(() => {});
      setMsg({ id: Date.now(), kind, text, retry, label });
      clearTimeout(timer.current);
      timer.current = setTimeout(() => setMsg(null), retry ? 8000 : 4000);
    },
    [],
  );
  useEffect(() => {
    Animated.timing(opacity, { toValue: msg ? 1 : 0, duration: 150, useNativeDriver: true }).start();
  }, [msg, opacity]);
  useEffect(() => () => clearTimeout(timer.current), []);

  const api = useMemo<Api>(
    () => ({ success: (t, undo) => show('success', t, undo, 'Undo'), error: (t, r) => show('error', t, r), info: (t) => show('info', t) }),
    [show],
  );

  const bg = msg?.kind === 'success' ? (c.dark ? '#2E7D32' : '#1B5E20') : msg?.kind === 'error' ? c.error : c.inverseSurface;
  const fg = msg?.kind === 'success' ? '#FFFFFF' : msg?.kind === 'error' ? c.onError : c.onInverseSurface;
  const Icon = msg?.kind === 'success' ? CircleCheck : msg?.kind === 'error' ? CircleAlert : Info;
  return (
    <Ctx.Provider value={api}>
      {children}
      {msg && (
        <Animated.View
          pointerEvents="box-none"
          accessibilityLiveRegion="polite"
          style={{ position: 'absolute', left: 12, right: 12, bottom: insets.bottom + 12, opacity }}
        >
          <View pointerEvents={msg.retry ? 'auto' : 'none'} style={{ backgroundColor: bg, borderRadius: Radii.card - 4, padding: 14, flexDirection: 'row', alignItems: 'center', gap: 10 }}>
            <Icon size={20} color={fg} />
            <Text style={{ flex: 1 }} color={fg}>
              {msg.text}
            </Text>
            {msg.retry && (
              <Pressable onPress={() => { const r = msg.retry; setMsg(null); r?.(); }} accessibilityLabel={msg.label ?? 'Retry'}>
                <Text variant="labelLarge" color={fg} style={{ fontFamily: 'Lato_700Bold' }}>{msg.label ?? 'Retry'}</Text>
              </Pressable>
            )}
          </View>
        </Animated.View>
      )}
    </Ctx.Provider>
  );
}
