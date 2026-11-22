import { isUserCancel } from '../../core/userCancel';

/**
 * Asks the engine to cancel one transfer. Never rejects: a rejection that is itself a cancel (the abort the cancel
 * caused) is dropped so no failure message follows the person's own cancel, and any other failure goes to [onError].
 */
export async function cancelTransfer(id: string, cancel: (id: string) => Promise<void>, onError: (e: unknown) => void): Promise<void> {
  try {
    await cancel(id);
  } catch (e) {
    if (!isUserCancel(e)) onError(e);
  }
}
