import { CircleAlert, CircleCheck, Info } from 'lucide-react-native';
import * as Haptics from 'expo-haptics';
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { Animated, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { useScheme } from '../theme';
import { t } from '../../i18n';
import { FontFamily, Radii, Spacing } from '../tokens';
import { Pressable } from './Pressable';
import { Text } from './Text';

type Kind = 'success' | 'error' | 'info';
type Msg = { id: number; kind: Kind; text: string; retry?: () => void; label?: string };
type Api = { success(text: string, onUndo?: () => void): void; error(text: string, onRetry?: () => void): void; info(text: string): void };

const VISIBLE_MS = 4000;
const VISIBLE_WITH_ACTION_MS = 8000;
const HAPTIC_STYLE: Partial<Record<Kind, 'Light' | 'Heavy'>> = { success: 'Light', error: 'Heavy' };

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
      const style = HAPTIC_STYLE[kind];
      if (style) Haptics.impactAsync(Haptics.ImpactFeedbackStyle[style]).catch(() => {});
      setMsg({ id: Date.now(), kind, text, retry, label });
      clearTimeout(timer.current);
      timer.current = setTimeout(() => setMsg(null), retry ? VISIBLE_WITH_ACTION_MS : VISIBLE_MS);
    },
    [],
  );
  useEffect(() => {
    Animated.timing(opacity, { toValue: msg ? 1 : 0, duration: 150, useNativeDriver: true }).start();
  }, [msg, opacity]);
  useEffect(() => () => clearTimeout(timer.current), []);

  const api = useMemo<Api>(
    () => ({ success: (text, undo) => show('success', text, undo, t('undoButton')), error: (t, r) => show('error', t, r), info: (t) => show('info', t) }),
    [show],
  );

  const bg = msg?.kind === 'success' ? c.success : msg?.kind === 'error' ? c.error : c.inverseSurface;
  const fg = msg?.kind === 'success' ? c.onSuccess : msg?.kind === 'error' ? c.onError : c.onInverseSurface;
  const Icon = msg?.kind === 'success' ? CircleCheck : msg?.kind === 'error' ? CircleAlert : Info;
  return (
    <Ctx.Provider value={api}>
      {children}
      {msg && (
        <Animated.View
          pointerEvents="box-none"
          accessibilityLiveRegion="polite"
          style={{ position: 'absolute', left: Spacing.md2, right: Spacing.md2, bottom: insets.bottom + Spacing.md2, opacity }}
        >
          <View pointerEvents={msg.retry ? 'auto' : 'none'} style={{ backgroundColor: bg, borderRadius: Radii.card - 4, padding: 14, flexDirection: 'row', alignItems: 'center', gap: 10 }}>
            <Icon size={20} color={fg} />
            <Text style={{ flex: 1 }} color={fg}>
              {msg.text}
            </Text>
            {msg.retry && (
              <Pressable onPress={() => { const r = msg.retry; setMsg(null); r?.(); }} accessibilityLabel={msg.label ?? t('retryButton')}>
                <Text variant="labelLarge" color={fg} style={{ fontFamily: FontFamily.bold }}>{msg.label ?? t('retryButton')}</Text>
              </Pressable>
            )}
          </View>
        </Animated.View>
      )}
    </Ctx.Provider>
  );
}
