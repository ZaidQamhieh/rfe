/** The part of the native loopback proxy that a lease uses. */
export interface MediaProxyApi {
  start(id: string, url: string, headers: Record<string, string>, pin: string | null): Promise<string>;
  stop(id: string): Promise<void>;
}

export interface MediaProxySpec {
  url: string;
  headers: Record<string, string>;
  pin: string | null;
}

/**
 * One proxy listener for one video, started once the spec is known. `release` is safe at any time:
 * before the spec resolves it prevents `start`; while `start` is in flight, or after it, the listener
 * is stopped as soon as it exists. Leaving a video early never leaves a loopback server (and its
 * bearer headers) alive.
 */
export function leaseMediaProxy(
  proxy: MediaProxyApi,
  id: string,
  getSpec: () => Promise<MediaProxySpec>,
): { uri: Promise<string>; release: () => void } {
  let released = false;
  let startCalled = false;
  let startSettled = false;
  const stop = () => {
    void proxy.stop(id).catch(() => {});
  };
  const uri = getSpec().then((spec) => {
    if (released) throw new Error('media proxy released');
    startCalled = true;
    return proxy.start(id, spec.url, spec.headers, spec.pin);
  });
  const settled = () => {
    startSettled = true;
    if (released && startCalled) stop();
  };
  uri.then(settled, settled);
  return {
    uri,
    release: () => {
      if (released) return;
      released = true;
      // Once `start` has returned (or failed) nothing else will stop it; before that, `settled` does.
      if (startSettled && startCalled) stop();
    },
  };
}
