import { VisibilityEditor } from '../../features/settings/VisibilityEditor';
import { SettingsPage } from '../../features/settings/parts';
import { useSettings } from '../../state/settings';

/** The app-wide default for which files the explorer hides; a computer can override it in its own settings. */
export default function VisibilitySettings() {
  const prefs = useSettings((s) => s.state.app.visibility);
  const setApp = useSettings((s) => s.setApp);
  return (
    <SettingsPage>
      <VisibilityEditor prefs={prefs} onChange={(v) => void setApp('visibility', v)} />
    </SettingsPage>
  );
}
