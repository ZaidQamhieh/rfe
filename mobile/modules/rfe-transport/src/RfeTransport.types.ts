export type RfeResponse = {
  status: number;
  headers: Record<string, string>;
  /** UTF-8 text body. Binary payloads use downloadToFile, never this path. */
  bodyText: string;
  seenFingerprint: string | null;
};

export type RfeTransportErrorCode =
  | 'ERR_CERT_PIN_MISMATCH'
  | 'ERR_PIN_POLICY'
  | 'ERR_CONNECTION'
  | 'ERR_TLS';
