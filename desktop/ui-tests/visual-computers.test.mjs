// Screenshot gate for the Computers screen (the mockup's "servers" screen: the saved computers and the picked computer's details), light and dark, full and empty.
// Pattern and limits: visual-region-gate.mjs. Runs on request:
//   RFE_VISUAL_SCREENS=computers node --test visual-computers.test.mjs
import { defineRegionGate } from "./visual-region-gate.mjs";

defineRegionGate({ key: "servers", names: ["computers", "servers"], label: "computers" });
