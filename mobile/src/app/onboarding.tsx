import { useRouter } from 'expo-router';

import { OnboardingScreen } from '../features/onboarding/OnboardingScreen';
import { markOnboarded } from '../features/onboarding/onboardingState';
import { keyValue } from '../services';

export default function Onboarding() {
  const router = useRouter();
  return (
    <OnboardingScreen
      onComplete={() => {
        // A failed write must not strand the person on this screen: go on, and it is shown again next launch.
        void markOnboarded(keyValue)
          .catch(() => undefined)
          .then(() => router.replace('/'));
      }}
    />
  );
}
