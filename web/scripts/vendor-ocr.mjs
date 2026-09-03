// Copies the page-scanner's OCR runtime out of node_modules and into
// public/assets/ocr/, where nginx serves it from our own origin.
//
// Design invariant 5 says nothing loads from a third party, and Tesseract.js
// defaults to jsDelivr for both its WASM core and its language data. Pointing
// it at local paths is the whole reason this script exists: the scanner works
// on a phone on a LAN with no internet, and no page load tells a CDN which
// book anyone is reading.
//
// The output is generated, not committed — it is 15 MB of vendored binaries
// that npm already pins in the lockfile. `npm run build` and `npm run dev`
// both run this first.

import { createRequire } from "node:module";
import { copyFile, mkdir, stat } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
const outDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../public/assets/ocr");

const core = path.dirname(require.resolve("tesseract.js-core/package.json"));
const data = path.dirname(require.resolve("@tesseract.js-data/eng/package.json"));
const worker = path.dirname(require.resolve("tesseract.js/package.json"));

// Tesseract.js picks its core file at runtime from what the device's WASM
// engine supports, so all three LSTM builds have to be present even though a
// browser only ever downloads one of them.
const files = [
  [path.join(worker, "dist/worker.min.js"), "worker.min.js"],
  [path.join(core, "tesseract-core-lstm.wasm.js"), "tesseract-core-lstm.wasm.js"],
  [path.join(core, "tesseract-core-simd-lstm.wasm.js"), "tesseract-core-simd-lstm.wasm.js"],
  [path.join(core, "tesseract-core-relaxedsimd-lstm.wasm.js"), "tesseract-core-relaxedsimd-lstm.wasm.js"],
  // The integer-quantised "best" English model: a third of the size of the
  // standard one and more accurate on the clean printed text of a book page,
  // which is the only thing this scanner ever points at.
  [path.join(data, "4.0.0_best_int/eng.traineddata.gz"), "eng.traineddata.gz"],
];

await mkdir(outDir, { recursive: true });
let total = 0;
for (const [from, name] of files) {
  await copyFile(from, path.join(outDir, name));
  total += (await stat(from)).size;
}
console.log(`vendored ${files.length} OCR assets (${(total / 1024 / 1024).toFixed(1)} MB) into public/assets/ocr`);
