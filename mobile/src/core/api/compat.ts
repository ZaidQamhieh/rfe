import { AgentApiError, UNSUPPORTED_BY_AGENT } from "./agentClient";
import type { Health } from "./models";
import { t, type StringKey } from "../../i18n";

/** Optional agent capabilities this app knows about. Everything else the app does is baseline and never gated. */
export type FeatureId = "pairRequest";

/** The en.json key of each feature's plain-words label, read at call time. */
const FEATURE_LABEL_KEYS: Record<FeatureId, StringKey> = {
  pairRequest: "featurePairRequest",
};

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
  new AgentApiError(0, UNSUPPORTED_BY_AGENT, t("agentTooOldForFeature", { feature: t(FEATURE_LABEL_KEYS[feature]) }));

export const isUnsupportedByAgent = (e: unknown): e is AgentApiError =>
  e instanceof AgentApiError && e.code === UNSUPPORTED_BY_AGENT;

/**
 * A route that a newer app expects but an older agent lacks answers 404 or 405 with no JSON error code (code UNKNOWN).
 * A coded 404 (NOT_FOUND for an expired request) is a real answer from a capable agent and is left alone.
 */
export const isMissingRoute = (e: unknown): boolean =>
  e instanceof AgentApiError &&
  (e.statusCode === 405 || (e.statusCode === 404 && e.code === "UNKNOWN"));
