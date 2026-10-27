// Mirrors AgentClient.normalizeFingerprint: canonical lowercase hex SHA-256.
export function normalizeFingerprint(fp: string | null | undefined): string | null {
  if (fp == null) return null;
  const n = fp.trim().replaceAll(':', '').toLowerCase();
  return /^[0-9a-f]{64}$/.test(n) ? n : null;
}

export class CertPinMismatch extends Error {
  readonly code = 'ERR_CERT_PIN_MISMATCH';
  constructor(message = 'Certificate fingerprint mismatch') {
    super(message);
    this.name = 'CertPinMismatch';
  }
}

/** Thrown when a pin is missing/invalid. The app must re-pair; it never falls back to unpinned. */
export class MissingCertPin extends Error {
  readonly code = 'ERR_PIN_POLICY';
  constructor(
    message = 'The host certificate fingerprint is missing or invalid. Re-pair the host to restore its secure pin.',
  ) {
    super(message);
    this.name = 'MissingCertPin';
  }
}
