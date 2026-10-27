import { File } from 'expo-file-system';
import { Stack, useNavigation } from 'expo-router';
import { ArrowLeft } from 'lucide-react-native';
import { useEffect, useRef, useState } from 'react';
import { ActivityIndicator, KeyboardAvoidingView, Platform, TextInput, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { AgentApiError } from '../core/api/agentClient';
import type { Entry } from '../core/api/models';
import { Pressable, Text, useDialogs, useToast } from '../design/components';
import { useScheme } from '../design/theme';
import { FontFamily, Radii } from '../design/tokens';
import { t } from '../i18n';
import { clientForHost } from '../services';
import { humanizeError } from '../features/pairing/pairingService';
import { PreviewIconButton } from '../features/preview/PreviewChrome';
import { fetchPreviewFile, MAX_EDITABLE_BYTES } from '../features/preview/previewFile';
import { useEditorSession } from '../features/preview/session';
import { decodeAsText } from '../features/preview/textDecode';
import { MONO } from '../features/preview/viewers/TextViewer';

/**
 * Port of TextEditorScreen: edits a small text file and saves it with PUT /content. Saves are
 * optimistic (baseModified); a STALE_WRITE offers reload or overwrite, and leaving with unsaved or
 * in-flight edits asks first.
 */
export default function Editor() {
  const session = useEditorSession((s) => s.session);
  if (!session) {
    return (
      <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center' }}>
        <Stack.Screen options={{ headerShown: false }} />
        <Text muted>Nothing to edit.</Text>
      </View>
    );
  }
  return <EditorBody key={session.entry.path} />;
}

function EditorBody() {
  const c = useScheme();
  const insets = useSafeAreaInsets();
  const toast = useToast();
  const dialogs = useDialogs();
  const navigation = useNavigation();
  const { host, entry, text: initialText, onSaved } = useEditorSession((s) => s.session)!;
  const text = useRef(initialText);
  const revision = useRef(0);
  const base = useRef<string | undefined>(entry.modified);
  const [generation, setGeneration] = useState(0); // remounts the uncontrolled input on reload
  const [seedText, setSeedText] = useState(initialText);
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);
  const guard = useRef({ dirty: false, saving: false });
  useEffect(() => {
    guard.current = { dirty, saving };
  }, [dirty, saving]);

  // Leaving with unsaved edits (or mid-save, whose outcome is unknown) asks first.
  useEffect(
    () =>
      navigation.addListener('beforeRemove', (e) => {
        if (!guard.current.dirty && !guard.current.saving) return;
        e.preventDefault();
        void dialogs
          .confirm({ title: t('discardChangesTitle'), description: t('unsavedChangesMessage'), confirmLabel: t('discardButton'), cancelLabel: t('keepEditingButton'), destructive: true })
          .then((discard) => {
            if (!discard) return;
            guard.current = { dirty: false, saving: false };
            navigation.dispatch(e.data.action);
          });
      }),
    [navigation, dialogs],
  );

  async function save(forceOverwrite = false) {
    if (saving) return;
    const atSave = revision.current;
    setSaving(true);
    try {
      const client = await clientForHost(host);
      const updated: Entry = await client.putContent(entry.path, text.current, forceOverwrite ? undefined : base.current);
      base.current = updated.modified;
      // Only the snapshot taken at save time was persisted; typing during the save stays dirty.
      setDirty(revision.current !== atSave);
      setSaving(false);
      onSaved?.(updated);
      toast.success(t('savedFile', { name: entry.name }));
    } catch (e) {
      setSaving(false);
      const code = e instanceof AgentApiError ? e.code : '';
      if (code === 'READ_ONLY') toast.error(t('readOnlyModeSaveError'));
      else if (code === 'PAYLOAD_TOO_LARGE') toast.error(t('fileTooLargeToSave'));
      else if (code === 'STALE_WRITE') await resolveStale();
      else toast.error(t('couldNotSaveFile', { error: humanizeError(e) }), () => void save(forceOverwrite));
    }
  }

  async function resolveStale() {
    const choice = await dialogs.choose<'reload' | 'overwrite'>({
      title: t('fileChangedOnDisk'),
      subtitle: t('staleWriteMessage'),
      options: [
        { value: 'reload', label: t('reloadButton') },
        { value: 'overwrite', label: t('overwriteButton'), tint: c.error },
      ],
    });
    if (choice === 'reload') await reload();
    else if (choice === 'overwrite') await save(true);
  }

  /** Body and metadata are separate requests; meta is re-read after the body and a mismatch is rejected (PR-38). */
  async function reload() {
    try {
      const client = await clientForHost(host);
      const before = await client.meta(entry.path);
      const uri = await fetchPreviewFile(host, before, MAX_EDITABLE_BYTES);
      const after = await client.meta(entry.path);
      if (after.modified !== before.modified || after.size !== before.size) throw new Error('File changed while reloading — try again.');
      const fresh = decodeAsText(await new File(uri).bytes());
      text.current = fresh;
      base.current = after.modified;
      setSeedText(fresh);
      setGeneration((g) => g + 1);
      setDirty(false);
      onSaved?.(after); // the preview behind should show this version too
      toast.info(t('reloadedFromHost'));
    } catch (e) {
      toast.error(t('couldNotReloadFile', { error: humanizeError(e) }), () => void reload());
    }
  }

  return (
    <KeyboardAvoidingView style={{ flex: 1, backgroundColor: c.surface }} behavior={Platform.OS === 'ios' ? 'padding' : undefined}>
      <Stack.Screen options={{ headerShown: false }} />
      <View style={{ paddingTop: insets.top + 6, paddingBottom: 8, paddingHorizontal: 12, flexDirection: 'row', alignItems: 'center', gap: 10 }}>
        <PreviewIconButton label="Back" onPress={() => navigation.goBack()}>
          <ArrowLeft size={19} color={c.onSurfaceVariant} />
        </PreviewIconButton>
        <Text numberOfLines={1} style={{ flex: 1, fontSize: 15.5, fontFamily: FontFamily.semibold }}>{entry.name}</Text>
        {saving ? <ActivityIndicator /> : null}
        <Pressable onPress={() => void save()} disabled={!dirty || saving} accessibilityLabel={t('saveButton')} pressedScale={0.96}>
          <View style={{ paddingHorizontal: 16, paddingVertical: 8, borderRadius: Radii.stadium, backgroundColor: dirty && !saving ? c.primary : c.surfaceContainerHighest }}>
            <Text style={{ fontFamily: FontFamily.semibold, fontSize: 13.5 }} color={dirty && !saving ? c.onPrimary : c.onSurfaceVariant}>{t('saveButton')}</Text>
          </View>
        </Pressable>
      </View>
      <TextInput
        key={generation}
        defaultValue={seedText}
        onChangeText={(v) => {
          text.current = v;
          revision.current++;
          if (!dirty) setDirty(true);
        }}
        multiline
        autoCorrect={false}
        autoCapitalize="none"
        spellCheck={false}
        textAlignVertical="top"
        accessibilityLabel={entry.name}
        style={[MONO, { flex: 1, color: c.onSurface, paddingHorizontal: 16, paddingBottom: insets.bottom + 16 }]}
      />
    </KeyboardAvoidingView>
  );
}
