// Screenshot gate for the Devices screen (the merged Computers + Devices page with the picked device's panel), light and dark, full and empty.
// Pattern and limits: visual-region-gate.mjs. Runs on request:
//   RFE_VISUAL_SCREENS=devices node --test visual-devices.test.mjs
import { defineRegionGate } from "./visual-region-gate.mjs";

defineRegionGate({ key: "devices", names: ["devices"], label: "devices" });
