'use strict';

// Reads finished PDFs with pdf.js (an independent implementation of page
// rotation and CropBox handling) so tests can check where things landed.

let pdfjsPromise;
function pdfjs() {
  pdfjsPromise ||= import('unpdf').then((unpdf) => unpdf.getResolvedPDFJS());
  return pdfjsPromise;
}

async function openPdf(bytes) {
  const lib = await pdfjs();
  const doc = await lib.getDocument({ data: new Uint8Array(bytes), isEvalSupported: false, useSystemFonts: false, verbosity: 0 }).promise;
  return { lib, doc };
}

// Matrices are [a, b, c, d, e, f]: (x, y) -> (a*x + c*y + e, b*x + d*y + f).
const apply = (m, x, y) => [m[0] * x + m[2] * y + m[4], m[1] * x + m[3] * y + m[5]];
const compose = (outer, inner) => [
  outer[0] * inner[0] + outer[2] * inner[1],
  outer[1] * inner[0] + outer[3] * inner[1],
  outer[0] * inner[2] + outer[2] * inner[3],
  outer[1] * inner[2] + outer[3] * inner[3],
  outer[0] * inner[4] + outer[2] * inner[5] + outer[4],
  outer[1] * inner[4] + outer[3] * inner[5] + outer[5],
];

/**
 * Bounding boxes { x, y, w, h } of every image drawn on a page, in points of
 * the DISPLAYED page measured from its top-left, plus the page size.
 */
async function imageBoxes(bytes, pageIndex) {
  const { lib, doc } = await openPdf(bytes);
  const page = await doc.getPage(pageIndex + 1);
  const viewport = page.getViewport({ scale: 1 });
  const { fnArray, argsArray } = await page.getOperatorList();
  const { OPS } = lib;

  let ctm = [1, 0, 0, 1, 0, 0];
  const stack = [];
  const boxes = [];
  fnArray.forEach((fn, i) => {
    if (fn === OPS.save) stack.push(ctm);
    else if (fn === OPS.restore) ctm = stack.pop() || ctm;
    else if (fn === OPS.transform) ctm = compose(ctm, argsArray[i]);
    else if (fn === OPS.paintImageXObject || fn === OPS.paintInlineImageXObject) {
      const corners = [[0, 0], [1, 0], [0, 1], [1, 1]].map(([x, y]) => viewport.convertToViewportPoint(...apply(ctm, x, y)));
      const xs = corners.map((c) => c[0]);
      const ys = corners.map((c) => c[1]);
      boxes.push({ x: Math.min(...xs), y: Math.min(...ys), w: Math.max(...xs) - Math.min(...xs), h: Math.max(...ys) - Math.min(...ys) });
    }
  });
  return { boxes, width: viewport.width, height: viewport.height };
}

/** All text of every page, joined by newlines. */
async function pageTexts(bytes) {
  const { doc } = await openPdf(bytes);
  const out = [];
  for (let n = 1; n <= doc.numPages; n += 1) {
    const page = await doc.getPage(n);
    const content = await page.getTextContent();
    out.push(content.items.map((item) => item.str).join(' '));
  }
  return out;
}

async function pageCountOf(bytes) {
  const { doc } = await openPdf(bytes);
  return doc.numPages;
}

module.exports = { imageBoxes, pageCountOf, pageTexts };
