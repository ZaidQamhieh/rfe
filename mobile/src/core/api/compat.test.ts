import { AgentApiError, type Transport } from "./agentClient";
import {
  hasFeature,
  isMissingRoute,
  isUnsupportedByAgent,
  unsupportedByAgent,
} from "./compat";
import {
  parseDevice,
  parseEntry,
  parseHealth,
  parseListing,
  parseStatus,
} from "./models";
import { DeviceIdentity } from "../security/deviceIdentity";
import { MemorySecureStore } from "../security/secureStore";
import { HostStore, MemoryKeyValueStore } from "../storage/hostStore";
import {
  humanizeError,
  requestPairing,
  type PairingDeps,
} from "../../features/pairing/pairingService";

const PIN = "ab".repeat(32);

describe("parseHealth", () => {
  it("reads an old (v1.42.x) agent as apiVersion 1 with no features", () => {
    const h = parseHealth({
      status: "ok",
      name: "PC",
      version: "1.42.3",
      os: "linux",
    });
    expect(h.apiVersion).toBe(1);
    expect(h.features).toEqual([]);
  });
  it("reads a new agent", () => {
    const h = parseHealth({
      status: "ok",
      apiVersion: 2,
      features: ["pairRequest"],
    });
    expect(h.apiVersion).toBe(2);
    expect(h.features).toEqual(["pairRequest"]);
  });
  it("tolerates wrong types and unknown fields", () => {
    const h = parseHealth({
      apiVersion: "2",
      features: ["a", 3, null, "a", ""],
      future: { x: 1 },
    });
    expect(h.apiVersion).toBe(1);
    expect(h.features).toEqual(["a"]);
    expect(
      parseHealth({ apiVersion: 0.5, features: "pairRequest" }),
    ).toMatchObject({ apiVersion: 1, features: [] });
  });
  it("rejects a non-object body so an empty 200 is not a healthy agent", () => {
    for (const v of [null, undefined, 5, "x", []])
      expect(() => parseHealth(v as never)).toThrow();
  });
});

describe("other parsers ignore extra and missing fields", () => {
  it("Device, Status, Entry, Listing", () => {
    expect(parseDevice({ id: "d", brandNew: [1] } as never).id).toBe("d");
    expect(
      parseStatus({ roots: [1, "a"], extra: true } as never).roots,
    ).toEqual(["a"]);
    expect(
      parseEntry({ name: "n", size: "big", extra: 1 } as never),
    ).toMatchObject({ name: "n", size: undefined });
    expect(() => parseListing({ entries: [null] } as never)).toThrow();
    for (const f of [parseStatus, parseEntry])
      expect(() => f(null as never)).toThrow();
    for (const f of [parseDevice, parseListing])
      expect(() => f(null as never)).not.toThrow();
  });
});

describe("hasFeature", () => {
  it("does not block when health is unknown", () =>
    expect(hasFeature(null, "pairRequest")).toBe(true));
  it("checks the advertised list", () => {
    expect(hasFeature(parseHealth({}), "pairRequest")).toBe(false);
    expect(
      hasFeature(parseHealth({ features: ["pairRequest"] }), "pairRequest"),
    ).toBe(true);
  });
});

function deps(status: number, bodyText: string): PairingDeps {
  const secure = new MemorySecureStore();
  const transport: Transport = {
    async request(a) {
      if (a.url.endsWith("/auth/challenge"))
        return {
          status: 200,
          headers: {},
          bodyText: '{"nonce":"n","proof":"v2"}',
        };
      return { status, headers: {}, bodyText };
    },
  };
  const rnd = (n: number) => Uint8Array.from({ length: n }, (_, i) => i);
  return {
    transport,
    identity: new DeviceIdentity(secure, rnd),
    store: new HostStore(new MemoryKeyValueStore(), secure),
    deviceId: async () => null,
    probe: async () => PIN,
    randomBytes: rnd,
  };
}

describe("UNSUPPORTED_BY_AGENT mapping", () => {
  it.each([
    [404, "404 page not found"],
    [405, "Method Not Allowed"],
  ])("maps a bare %i from POST /pair/request", async (status, body) => {
    const e = await requestPairing(deps(status, body), "10.0.0.2:8765").catch(
      (x) => x,
    );
    expect(isUnsupportedByAgent(e)).toBe(true);
    expect(humanizeError(e)).toMatch(/too old/);
  });
  it("does not map other failures", async () => {
    const e = await requestPairing(
      deps(500, '{"code":"BOOM","message":"x"}'),
      "10.0.0.2:8765",
    ).catch((x) => x);
    expect(isUnsupportedByAgent(e)).toBe(false);
    expect(e).toBeInstanceOf(AgentApiError);
  });
  it("skips the call when health shows the feature is missing", async () => {
    const e = await requestPairing(
      deps(200, "{}"),
      "10.0.0.2:8765",
      parseHealth({ apiVersion: 1 }),
    ).catch((x) => x);
    expect(isUnsupportedByAgent(e)).toBe(true);
  });
  it("leaves a coded 404 alone", () => {
    expect(isMissingRoute(new AgentApiError(404, "NOT_FOUND", "gone"))).toBe(
      false,
    );
    expect(isMissingRoute(unsupportedByAgent("pairRequest"))).toBe(false);
  });
});
