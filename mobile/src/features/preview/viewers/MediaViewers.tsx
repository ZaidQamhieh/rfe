import { useAudioPlayer, useAudioPlayerStatus } from 'expo-audio';
import { useVideoPlayer, VideoView, type VideoPlayer } from 'expo-video';
import { Check, Music, Pause, Play, Settings, Video as VideoIcon } from 'lucide-react-native';
import { useEffect, useState } from 'react';
import { View } from 'react-native';

import type { Entry, Health } from '../../../core/api/models';
import type { Host } from '../../../core/models/host';
import { mediaProxy } from '../../../core/native';
import { Menu, Pressable, Text, useToast } from '../../../design/components';
import { useScheme } from '../../../design/theme';
import { FontFamily, Radii, Spacing } from '../../../design/tokens';
import { t } from '../../../i18n';
import { clientForHost } from '../../../services';
import { hashKey } from '../../explorer/thumbnails';
import { humanizeError } from '../../../core/humanizeError';
import { ignoreError } from '../../../core/log';
import { fullscreenOrientationFor, type VideoSize } from '../fullscreenOrientation';
import { leaseMediaProxy } from '../mediaProxyLease';
import { PreviewError, PreviewLoading, PreviewTooLarge } from '../PreviewChrome';
import { MAX_AUDIO_PREVIEW_BYTES, usePreviewFile } from '../previewFile';
import { qualityChoices, resumePosition, transcodeErrorMessage, type VideoQualityChoice } from '../videoQuality';

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

/**
 * One video with a source choice. "Original" is the direct path; a quality plays the agent's HLS transcode through the
 * same pinned loopback proxy. Switching keeps the playback position, and a transcode that fails drops back to Original
 * at that position with a plain message.
 */
function VideoStream({ host, entry }: { host: Host; entry: Entry }) {
  const toast = useToast();
  const [src, setSrc] = useState<{ uri: string } | { error: unknown } | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [quality, setQuality] = useState<VideoQualityChoice>('original');
  const [startAt, setStartAt] = useState(0);
  const [health, setHealth] = useState<Pick<Health, 'features'> | null>(null);

  // The quality menu depends on what the agent advertises; a failed reading leaves only Original.
  useEffect(() => {
    let live = true;
    clientForHost(host)
      .then((c) => c.health())
      .then(
        (h) => live && setHealth(h),
        ignoreError('video quality health'),
      );
    return () => {
      live = false;
    };
  }, [host]);

  // Back to the direct source at `position`, telling the viewer why.
  const fallBackToOriginal = (error: unknown, position: number | undefined) => {
    toast.error(t('qualityFellBack', { reason: transcodeErrorMessage(error) }));
    setStartAt(resumePosition(position));
    setSrc(null);
    setQuality('original');
  };
  const pickQuality = (choice: VideoQualityChoice, position: number | undefined) => {
    if (choice === quality) return;
    setStartAt(resumePosition(position));
    setSrc(null);
    setQuality(choice);
  };

  useEffect(() => {
    let live = true;
    const id = `v${hashKey(entry.path)}${quality}${attempt}`;
    const lease = leaseMediaProxy(mediaProxy, id, async () => {
      const client = await clientForHost(host);
      return quality === 'original' ? client.downloadSpec(entry.path) : client.streamSpec(entry.path, quality);
    });
    lease.uri.then(
      (uri) => live && setSrc({ uri }),
      (error) => {
        if (!live) return;
        if (quality === 'original') setSrc({ error });
        else fallBackToOriginal(error, startAt);
      },
    );
    return () => {
      live = false;
      lease.release();
    };
    // fallBackToOriginal and startAt are read only when the lease fails for the quality this effect started with.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [host, entry.path, attempt, quality]);

  if (src === null) return <PreviewLoading onDark message={t('preparingVideo')} />;
  if ('error' in src) {
    return (
      <PreviewError
        onDark
        message={t('couldNotPlayVideo', { error: humanizeError(src.error) })}
        onRetry={() => {
          setSrc(null);
          setAttempt((n) => n + 1);
        }}
      />
    );
  }
  return (
    <VideoSurface
      uri={src.uri}
      startAt={startAt}
      quality={quality}
      choices={qualityChoices(health)}
      onPickQuality={pickQuality}
      onStreamError={fallBackToOriginal}
    />
  );
}

/** Jumps a player to `seconds` (the player's documented way to seek is assigning currentTime). */
const seekTo = (p: VideoPlayer, seconds: number) => {
  p.currentTime = seconds;
};

/** The playing video with its native controls; the fullscreen button opens it turned to match the picture (landscape for a wide one). */
function VideoSurface({
  uri,
  startAt,
  quality,
  choices,
  onPickQuality,
  onStreamError,
}: {
  uri: string;
  startAt: number;
  quality: VideoQualityChoice;
  choices: VideoQualityChoice[];
  onPickQuality: (choice: VideoQualityChoice, position: number) => void;
  onStreamError: (error: unknown, position: number) => void;
}) {
  const player = useVideoPlayer(uri, (p) => {
    p.loop = false;
    p.play();
  });
  // Continue from the position the previous source reached, once the new one can seek.
  useEffect(() => {
    if (startAt <= 0) return;
    if (player.status === 'readyToPlay') {
      seekTo(player, startAt);
      return;
    }
    const sub = player.addListener('statusChange', ({ status }) => {
      if (status !== 'readyToPlay') return;
      seekTo(player, startAt);
      sub.remove();
    });
    return () => sub.remove();
  }, [player, startAt]);
  // A transcode the player cannot play is not a dead end: report it so the viewer returns to Original.
  useEffect(() => {
    if (quality === 'original') return;
    const sub = player.addListener('statusChange', ({ status, error }) => {
      if (status === 'error') onStreamError(error, player.currentTime > 0 ? player.currentTime : startAt);
    });
    return () => sub.remove();
  }, [player, quality, startAt, onStreamError]);
  const [size, setSize] = useState<VideoSize | null>(player.videoTrack?.size ?? null);
  useEffect(() => {
    const sub = player.addListener('videoTrackChange', ({ videoTrack }) => setSize(videoTrack?.size ?? null));
    return () => sub.remove();
  }, [player]);
  return (
    <View style={{ flex: 1 }}>
      <VideoView player={player} style={{ flex: 1, backgroundColor: '#000' }} nativeControls contentFit="contain" fullscreenOptions={{ enable: true, orientation: fullscreenOrientationFor(size) }} />
      {choices.length > 1 && (
        <View style={{ position: 'absolute', top: Spacing.sm, right: Spacing.sm, borderRadius: Radii.lg, backgroundColor: 'rgba(0,0,0,0.45)' }}>
          <Menu
            accessibilityLabel={t('videoQuality')}
            trigger={<Settings size={22} color="#fff" />}
            items={choices.map((choice) => ({
              label: choice === 'original' ? t('qualityOriginal') : choice,
              icon: choice === quality ? <Check size={18} color="#888" /> : <View style={{ width: 18 }} />,
              onPress: () => onPickQuality(choice, player.currentTime),
            }))}
          />
        </View>
      )}
    </View>
  );
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
  if (state.status === 'loading') return <PreviewLoading message={t('loadingAudio')} />;
  if (state.status === 'tooLarge') return <PreviewTooLarge size={state.size} />;
  if (state.status === 'error') return <PreviewError message={t('couldNotLoadAudio', { error: humanizeError(state.error) })} onRetry={retry} />;
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
          accessibilityLabel={t('seekLabel')}
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
      <Pressable onPress={toggle} accessibilityLabel={status.playing ? t('trPause') : t('playLabel')} pressedScale={0.92}>
        <View style={{ width: 64, height: 64, borderRadius: 32, backgroundColor: c.primary, alignItems: 'center', justifyContent: 'center' }}>
          {status.playing ? <Pause size={28} color={c.onPrimary} /> : <Play size={28} color={c.onPrimary} />}
        </View>
      </Pressable>
    </View>
  );
}
