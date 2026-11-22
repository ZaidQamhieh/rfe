import { Image } from 'expo-image';
import { useEffect, useState } from 'react';
import { Animated, PanResponder, View, type GestureResponderEvent } from 'react-native';

import type { Entry } from '../../../core/api/models';
import type { Host } from '../../../core/models/host';
import { humanizeError } from '../../../core/humanizeError';
import { PreviewError, PreviewLoading, PreviewTooLarge } from '../PreviewChrome';
import { MAX_IN_MEMORY_PREVIEW_BYTES, usePreviewFile } from '../previewFile';
import { swipeDirection, type SwipeDirection } from '../swipeNav';
import { createZoomGesture } from '../zoomGesture';

const distance = (e: GestureResponderEvent) => {
  const [a, b] = e.nativeEvent.touches;
  return a && b ? Math.hypot(a.pageX - b.pageX, a.pageY - b.pageY) : 0;
};

/**
 * Pinch / double-tap zoom and pan-while-zoomed, bound to Animated values; logic lives in zoomGesture.ts. A one-finger
 * horizontal swipe at 1x is reported through [setOnSwipe], so the pager moves even when this view holds the touch.
 */
function createZoom() {
  const scale = new Animated.Value(1);
  const tx = new Animated.Value(0);
  const ty = new Animated.Value(0);
  let onZoom: ((zoomed: boolean) => void) | undefined;
  let onSwipe: ((dir: SwipeDirection) => void) | undefined;
  const z = createZoomGesture((v, zoomedChanged) => {
    scale.setValue(v.scale);
    tx.setValue(v.x);
    ty.setValue(v.y);
    if (zoomedChanged !== null) onZoom?.(zoomedChanged);
  });
  const responder = PanResponder.create({
    onStartShouldSetPanResponder: () => true,
    // Single-finger moves at 1x belong to the pager; pinches and pans while zoomed are ours.
    onMoveShouldSetPanResponder: (e) => e.nativeEvent.touches.length >= 2 || z.zoomed(),
    onPanResponderTerminationRequest: () => z.canRelease(),
    onPanResponderGrant: (e) => z.start(distance(e)),
    onPanResponderMove: (e, gs) => z.move(distance(e), gs.dx, gs.dy),
    onPanResponderRelease: (_e, gs) => {
      // Read before z.end, which clears the pinch flag: a pinch or a drag while zoomed is never a page swipe.
      const dir = z.canRelease() ? swipeDirection({ dx: gs.dx, dy: gs.dy, vx: gs.vx }) : 0;
      z.end(gs.dx, gs.dy, Date.now());
      if (dir !== 0) onSwipe?.(dir);
    },
  });
  return {
    scale,
    tx,
    ty,
    panHandlers: responder.panHandlers,
    setOnZoom: (cb?: (zoomed: boolean) => void) => {
      onZoom = cb;
    },
    setOnSwipe: (cb?: (dir: SwipeDirection) => void) => {
      onSwipe = cb;
    },
  };
}

/**
 * Port of ImagePreviewScreen: black canvas, fit-to-screen, pinch / double-tap zoom and pan while
 * zoomed. [onZoomChange] lets the pager stop paging while the image is zoomed in.
 */
export function ImageViewer({ host, entry, onZoomChange, onSwipe }: { host: Host; entry: Entry; onZoomChange?: (zoomed: boolean) => void; onSwipe?: (dir: SwipeDirection) => void }) {
  const { state, retry } = usePreviewFile(host, entry, MAX_IN_MEMORY_PREVIEW_BYTES);
  const [zoom] = useState(createZoom);
  useEffect(() => zoom.setOnZoom(onZoomChange), [zoom, onZoomChange]);
  useEffect(() => zoom.setOnSwipe(onSwipe), [zoom, onSwipe]);

  if (state.status === 'loading') return <PreviewLoading onDark />;
  if (state.status === 'tooLarge') return <PreviewTooLarge size={state.size} onDark />;
  if (state.status === 'error') return <PreviewError message={humanizeError(state.error)} onRetry={retry} onDark />;
  return (
    <View style={{ flex: 1, backgroundColor: '#000', overflow: 'hidden' }} {...zoom.panHandlers}>
      <Animated.View style={{ flex: 1, transform: [{ translateX: zoom.tx }, { translateY: zoom.ty }, { scale: zoom.scale }] }}>
        <Image source={{ uri: state.uri }} style={{ flex: 1 }} contentFit="contain" accessibilityLabel={entry.name} />
      </Animated.View>
    </View>
  );
}
