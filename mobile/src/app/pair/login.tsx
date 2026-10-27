import { Stack, useLocalSearchParams, useRouter } from 'expo-router';
import { ScrollView, View } from 'react-native';

import { PageHead, useToast } from '../../design/components';
import { StackTopBar } from '../../features/hosts/LumenRows';
import { AccountForm } from '../../features/pairing/AccountForm';
import { t } from '../../i18n';

export default function Screen() {
  const router = useRouter();
  const toast = useToast();
  const { address } = useLocalSearchParams<{ address?: string }>();
  return (
    <View style={{ flex: 1 }}>
      <Stack.Screen options={{ headerShown: false, title: t('loginTab') }} />
      <StackTopBar context="RFE · Log in" />
      <ScrollView contentContainerStyle={{ paddingBottom: 32 }} keyboardShouldPersistTaps="handled">
        <PageHead title={t('loginTab')} subtitle="Sign in to a computer you already have an account on." />
        <View style={{ paddingHorizontal: 18 }}>
          <AccountForm
            mode="login"
            prefillAddress={address}
            onPaired={(label) => {
              toast.success(t('pairedWith', { name: label }));
              router.dismissTo('/');
            }}
          />
        </View>
      </ScrollView>
    </View>
  );
}
