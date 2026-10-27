/**
 * What an `rfe://` link or share hand-off from another app may open. Any app or web page can fire
 * these links, so nothing that acts on its own (pairing, file operations, previews) is reachable:
 * the only external destination is the Add-workspace screen with its address prefilled, which still
 * needs a tap to do anything. Everything else lands on the home screen.
 */
const HOME = '/';

export function externalLinkTarget(raw: string): string {
  let host = '';
  let pathname = '';
  let address: string | null = null;
  try {
    const u = new URL(raw, 'rfe://app');
    // rfe://pair/request parses as host "pair", path "/request"; /pair/request as host "app".
    const isScheme = /^[a-z][a-z0-9+.-]*:\/\//i.test(raw);
    host = isScheme ? u.hostname : '';
    pathname = (isScheme ? `/${host}${u.pathname}` : u.pathname).replace(/\/+$/, '');
    address = u.searchParams.get('address');
  } catch {
    return HOME;
  }
  if (pathname !== '/pair') return HOME;
  if (!address || address.length > 255 || /[\u0000-\u001f\u007f]/.test(address)) return '/pair';
  return `/pair?address=${encodeURIComponent(address)}`;
}
