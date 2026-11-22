import { AgentApiError, errCode, HTTP_UNAUTHORIZED, isBareHttpStatusMessage } from "./api/agentClient";
import { t } from "../i18n";
import { CertPinMismatch, MissingCertPin } from "./api/pin";

/** The agent's code for a phone whose saved identity no longer matches the one this computer has on record. */
const DEVICE_KEY_MISMATCH = 'DEVICE_KEY_MISMATCH';

/** The agent's code for a pairing code that would replace the key of a phone it already knows (409 from POST /pair). */
const PAIR_REPLACES = 'PAIR_REPLACES';

/** The agent's code for a missing, revoked or unknown token (its JSON 401 body). */
const UNAUTHORIZED = 'UNAUTHORIZED';

/** What a phone is told when its computer no longer accepts it. Read at call time, so the locale is not fixed at import. */
export function phoneRemovedMessage(): string {
  return t('phoneRemovedMessage');
}

/** Message shown for a failed call (port of humanizeError). */
export function humanizeError(e: unknown): string {
  if (e instanceof CertPinMismatch) return t('certPinMismatchMessage');
  if (e instanceof MissingCertPin) return e.message;
  if (e instanceof AgentApiError) {
    // A streamed download only knows the status; the agent's own message (when there is one) already says what is missing.
    if (e.code === 'CAPABILITY_DENIED' || (e.statusCode === 403 && isBareHttpStatusMessage(e.message))) {
      // A streamed download carries only the status, so this cannot tell a missing grant from a path outside the shared folders (audit F-34).
      return t('capabilityDeniedMessage');
    }
    // A revoked phone gets a bare 401 from a streamed call (audit F-33), or the agent's coded 401 from a normal call.
    if (e.statusCode === HTTP_UNAUTHORIZED && (e.code === UNAUTHORIZED || isBareHttpStatusMessage(e.message))) {
      return phoneRemovedMessage();
    }
    // The agent's own text names a "device id" and a "key": replace it with the plain wording.
    if (e.code === DEVICE_KEY_MISMATCH) return t('deviceKeyMismatch');
    // The agent's own text suggests a CLI command; the phone's way out is the approve-on-computer pairing screen.
    if (e.code === PAIR_REPLACES) return t('pairReplaces');
    // A dropped connection says only "lost": show the same plain wording as a failed first contact.
    if (e.code === 'CONNECTION') return t('connectionFailedMessage');
    return e.message;
  }
  // A raw native failure before any agent answered (the first-contact certificate probe): its message is often only the
  // host name ("Could not ask the computer: myhost"), which says nothing about what to fix.
  const code = errCode(e);
  if (code === 'ERR_CONNECTION') return t('connectionFailedMessage');
  if (code === 'ERR_TLS') return t('tlsFailedMessage');
  const message = e instanceof Error ? e.message : String(e);
  // A native module rejection wraps the real reason: "Call to function 'X.y' has been rejected.\n→ Caused by: <reason>".
  return message.replace(/^Call to function '[^']*' has been rejected\.\s*→ Caused by:\s*/, '');
}
