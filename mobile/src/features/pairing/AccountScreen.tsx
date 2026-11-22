import { Stack, useLocalSearchParams, useRouter } from 'expo-router';
import { ScrollView, View } from 'react-native';

import { PageHead, useToast } from '../../design/components';
import { t } from '../../i18n';
import { StackTopBar } from '../hosts/LumenRows';
import { AccountForm } from './AccountForm';
import { AccountServerForm } from './AccountServerForm';
import { Spacing } from '../../design/tokens';

const COPY = {
  login: { title: () => t('loginTab'), context: t('loginContext'), subtitle: t('loginSubtitle') },
  server: { title: () => t('accountServerTitle'), context: t('loginContext'), subtitle: t('accountServerSubtitle') },
  register: { title: () => t('registerTab'), context: t('registerContext'), subtitle: t('registerSubtitle') },
} as const;

/** The login and register routes: one screen, the mode picks the wording and the form. */
export function AccountScreen({ mode }: { mode: 'login' | 'register' | 'server' }) {
  const router = useRouter();
  const toast = useToast();
  const { address } = useLocalSearchParams<{ address?: string }>();
  const copy = COPY[mode];
  const onPaired = (label: string) => {
    toast.success(t('pairedWith', { name: label }));
    router.dismissTo('/');
  };
  return (
    <View style={{ flex: 1 }}>
      <Stack.Screen options={{ headerShown: false, title: copy.title() }} />
      <StackTopBar context={copy.context} />
      <ScrollView contentContainerStyle={{ paddingBottom: Spacing.xl }} keyboardShouldPersistTaps="handled">
        <PageHead title={copy.title()} subtitle={copy.subtitle} />
        <View style={{ paddingHorizontal: 18 }}>
          {mode === 'server' ? (
            <AccountServerForm prefillAddress={address} onPaired={onPaired} />
          ) : (
            <AccountForm mode={mode} prefillAddress={address} onPaired={onPaired} />
          )}
        </View>
      </ScrollView>
    </View>
  );
}
