import { useAudioPlayer, useAudioPlayerStatus } from 'expo-audio';
import { useVideoPlayer, VideoView } from 'expo-video';
import { Music, Pause, Play, Video as VideoIcon } from 'lucide-react-native';
import { useEffect, useState } from 'react';
import { View } from 'react-native';

import type { Entry } from '../../../core/api/models';
import type { Host } from '../../../core/models/host';
import { mediaProxy } from '../../../core/native';
import { Pressable, Text } from '../../../design/components';
import { useScheme } from '../../../design/theme';
import { FontFamily, Radii, Spacing } from '../../../design/tokens';
import { clientForHost } from '../../../services';
import { hashKey } from '../../explorer/thumbnails';
import { humanizeError } from '../../pairing/pairingService';
import { leaseMediaProxy } from '../mediaProxyLease';
import { PreviewError, PreviewLoading, PreviewTooLarge } from '../PreviewChrome';
import { MAX_AUDIO_PREVIEW_BYTES, usePreviewFile } from '../previewFile';

/**
 * Port of VideoPreviewScreen: streams through the native loopback proxy (the player never sees the
 * pin or token) and seeks with Range. Only the current pager page streams; neighbours show a still.
 */
export function VideoViewer({ host, entry, isCurrent }: { host: Host; entry: Entry; isCurrent: boolean }) {
  if (!isCurrent) {
    return (
      <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', backgroundColor: '#000' }}>
        <VideoIcon size={48} color="rgba(255,255,255,0.6)" />
      </View>
    );
  }
  return <VideoStream host={host} entry={entry} />;
}

function VideoStream({ host, entry }: { host: Host; entry: Entry }) {
  const [src, setSrc] = useState<{ uri: string } | { error: unknown } | null>(null);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let live = true;
    const id = `v${hashKey(entry.path)}${attempt}`;
    const lease = leaseMediaProxy(mediaProxy, id, async () => {
      const client = await clientForHost(host);
      return client.downloadSpec(entry.path);
    });
    lease.uri.then(
      (uri) => live && setSrc({ uri }),
      (error) => live && setSrc({ error }),
    );
    return () => {
      live = false;
      lease.release();
    };
  }, [host, entry.path, attempt]);

  if (src === null) return <PreviewLoading onDark message="Preparing video…" />;
  if ('error' in src) {
    return (
      <PreviewError
        onDark
        message={`Could not play this video.\n${humanizeError(src.error)}`}
        onRetry={() => {
          setSrc(null);
          setAttempt((n) => n + 1);
        }}
      />
    );
  }
  return <VideoSurface uri={src.uri} />;
}

function VideoSurface({ uri }: { uri: string }) {
  const player = useVideoPlayer(uri, (p) => {
    p.loop = false;
    p.play();
  });
  return <VideoView player={player} style={{ flex: 1, backgroundColor: '#000' }} nativeControls contentFit="contain" fullscreenOptions={{ enable: true }} />;
}

const clock = (s: number) => {
  if (!Number.isFinite(s) || s < 0) s = 0;
  const m = Math.floor(s / 60);
  const sec = Math.floor(s % 60);
  return `${m}:${sec.toString().padStart(2, '0')}`;
};

/** Port of AudioPreviewScreen: downloads (≤100 MB) to the preview cache, then plays locally. */
export function AudioViewer({ host, entry, isCurrent }: { host: Host; entry: Entry; isCurrent: boolean }) {
  const { state, retry } = usePreviewFile(host, entry, MAX_AUDIO_PREVIEW_BYTES);
  if (state.status === 'loading') return <PreviewLoading message="Loading audio…" />;
  if (state.status === 'tooLarge') return <PreviewTooLarge size={state.size} />;
  if (state.status === 'error') return <PreviewError message={`Could not load this audio file.\n${humanizeError(state.error)}`} onRetry={retry} />;
  return <AudioPlayerView uri={state.uri} name={entry.name} isCurrent={isCurrent} />;
}

function AudioPlayerView({ uri, name, isCurrent }: { uri: string; name: string; isCurrent: boolean }) {
  const c = useScheme();
  const player = useAudioPlayer({ uri });
  const status = useAudioPlayerStatus(player);
  const [trackWidth, setTrackWidth] = useState(1);
  useEffect(() => {
    if (!isCurrent) player.pause();
  }, [isCurrent, player]);
  const duration = status.duration || 0;
  const progress = duration > 0 ? Math.min(status.currentTime / duration, 1) : 0;
  const toggle = () => {
    if (status.playing) return player.pause();
    if (status.didJustFinish || (duration > 0 && status.currentTime >= duration - 0.25)) void player.seekTo(0);
    player.play();
  };
  return (
    <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', padding: Spacing.lg, gap: Spacing.lg }}>
      <View style={{ width: 160, height: 160, borderRadius: Radii.lg, backgroundColor: `${c.primary}24`, alignItems: 'center', justifyContent: 'center' }}>
        <Music size={64} color={c.primary} />
      </View>
      <Text numberOfLines={2} style={{ fontSize: 17, fontFamily: FontFamily.semibold, textAlign: 'center' }}>{name}</Text>
      <View style={{ alignSelf: 'stretch', gap: 6 }}>
        <Pressable
          accessibilityLabel="Seek"
          accessibilityRole="adjustable"
          onLayout={(e) => setTrackWidth(Math.max(e.nativeEvent.layout.width, 1))}
          onPress={(e) => duration > 0 && void player.seekTo((e.nativeEvent.locationX / trackWidth) * duration)}
          pressedScale={1}
        >
          <View style={{ height: 24, justifyContent: 'center' }}>
            <View style={{ height: 4, borderRadius: 2, backgroundColor: c.surfaceContainerHighest }}>
              <View style={{ width: `${progress * 100}%`, height: 4, borderRadius: 2, backgroundColor: c.primary }} />
            </View>
          </View>
        </Pressable>
        <View style={{ flexDirection: 'row', justifyContent: 'space-between' }}>
          <Text variant="bodySmall" muted>{clock(status.currentTime)}</Text>
          <Text variant="bodySmall" muted>{clock(duration)}</Text>
        </View>
      </View>
      <Pressable onPress={toggle} accessibilityLabel={status.playing ? 'Pause' : 'Play'} pressedScale={0.92}>
        <View style={{ width: 64, height: 64, borderRadius: 32, backgroundColor: c.primary, alignItems: 'center', justifyContent: 'center' }}>
          {status.playing ? <Pause size={28} color={c.onPrimary} /> : <Play size={28} color={c.onPrimary} />}
        </View>
      </Pressable>
    </View>
  );
}
