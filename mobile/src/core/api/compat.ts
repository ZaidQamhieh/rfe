import { AgentApiError } from "./agentClient";
import type { Health } from "./models";

/** Optional agent capabilities this app knows about. Everything else the app does is baseline and never gated. */
export type FeatureId = "pairRequest";

export const FEATURE_LABELS: Record<FeatureId, string> = {
  pairRequest: "asking to pair without a code",
};

export const UNSUPPORTED_BY_AGENT = "UNSUPPORTED_BY_AGENT";

/**
 * Whether the agent offers an optional feature. Without a health reading (unpaired, offline, not loaded yet) the
 * answer is true: the call is attempted and a 404/405 is mapped to UNSUPPORTED_BY_AGENT, so an unknown agent is
 * never blocked on a guess.
 */
export function hasFeature(
  health: Pick<Health, "features"> | null | undefined,
  id: FeatureId | string,
): boolean {
  if (!health) return true;
  return Array.isArray(health.features) && health.features.includes(id);
  // Note: an unauthenticated /health carries no features; pass only a reading from a paired device.
}

export const unsupportedByAgent = (feature: FeatureId) =>
  new AgentApiError(
    0,
    UNSUPPORTED_BY_AGENT,
    `This computer’s agent is too old for ${FEATURE_LABELS[feature]}. Update the agent on that computer.`,
  );

export const isUnsupportedByAgent = (e: unknown): e is AgentApiError =>
  e instanceof AgentApiError && e.code === UNSUPPORTED_BY_AGENT;

/**
 * A route that a newer app expects but an older agent lacks answers 404 or 405 with no JSON error code (code UNKNOWN).
 * A coded 404 (NOT_FOUND for an expired request) is a real answer from a capable agent and is left alone.
 */
export const isMissingRoute = (e: unknown): boolean =>
  e instanceof AgentApiError &&
  (e.statusCode === 405 || (e.statusCode === 404 && e.code === "UNKNOWN"));
