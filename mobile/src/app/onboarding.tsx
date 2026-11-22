import { useRouter } from 'expo-router';

import { ignoreError } from '../core/log';
import { markOnboarded } from '../features/onboarding/onboardingState';
import { FirstRunView } from '../features/pairing/GettingInViews';
import { keyValue } from '../services';

/** First run: one screen with one button. Pressing it marks the person as onboarded and opens Add a computer. */
export default function Onboarding() {
  const router = useRouter();
  return (
    <FirstRunView
      onAdd={() => {
        // A failed write must not strand the person on this screen: go on, and it is shown again next launch.
        void markOnboarded(keyValue)
          .catch(ignoreError('onboarding'))
          .then(() => {
            router.replace('/');
            router.push('/pair');
          });
      }}
    />
  );
}
