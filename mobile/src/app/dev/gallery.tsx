import { Download } from 'lucide-react-native';
import { ScrollView, View } from 'react-native';

import { Button, ErrorRetry, GroupedCard, ListingSkeleton, OfflineBanner, ScreenHeader, SectionLabel, Text, TextField } from '../../design/components';
import { Spacing } from '../../design/tokens';

/** Dev-only: every shared component on one screen, for side-by-side visual comparison in both themes. */
export default function Gallery() {
  return (
    <ScrollView contentContainerStyle={{ padding: Spacing.md, gap: Spacing.md }}>
      <ScreenHeader title="Design gallery" subtitle="Lumen tokens · Inter" />
      <SectionLabel title="Buttons" />
      <GroupedCard>
        <View style={{ gap: 10 }}>
          <Button label="Primary action" icon={<Download size={18} color="#fff" />} />
          <Button label="Filled" kind="filled" />
          <Button label="Outlined" kind="outlined" />
          <Button label="Delete" kind="outlined" destructive />
          <Button label="Busy" busy />
          <Button label="Disabled" disabled />
        </View>
      </GroupedCard>
      <SectionLabel title="Text" />
      <GroupedCard>
        <Text variant="titleLarge">Title large</Text>
        <Text variant="titleMedium">Title medium (file names)</Text>
        <Text variant="bodyMedium" muted>Body medium muted · 1.2 MB · 3 min ago</Text>
        <Text variant="bodySmall" muted style={{ fontFamily: 'JetBrainsMono-Regular' }}>/srv/share/docs/report.pdf</Text>
      </GroupedCard>
      <SectionLabel title="Fields" />
      <GroupedCard>
        <View style={{ gap: 12 }}>
          <TextField label="Host address" placeholder="192.168.1.20:8765" />
          <TextField label="Fingerprint" mono value="97f2268f886978d5" onChangeText={() => {}} error="Fingerprint must be 64 hex characters" />
        </View>
      </GroupedCard>
      <SectionLabel title="States" />
      <GroupedCard padded={false}>
        <OfflineBanner />
        <ListingSkeleton rows={2} />
      </GroupedCard>
      <GroupedCard style={{ height: 220 }}>
        <ErrorRetry message="Could not reach the host." onRetry={() => {}} />
      </GroupedCard>
    </ScrollView>
  );
}
