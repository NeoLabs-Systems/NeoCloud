'use strict';

// The bundled Noto Sans subsets (SIL OFL 1.1, see server/assets/signing/fonts):
// Latin (incl. Extended-A/B and Vietnamese), Greek, Cyrillic, general
// punctuation and currency signs. CJK, Arabic, Hebrew, Indic scripts and
// emoji are NOT covered; text.js turns such characters into "?".

const fs = require('fs');
const path = require('path');
const fontkit = require('@pdf-lib/fontkit');
const { Measurer } = require('./text');

const FONT_DIR = path.join(__dirname, '..', '..', '..', 'assets', 'signing', 'fonts');
const FILES = {
  regular: { file: 'NotoSans-Regular.subset.ttf', name: 'NotoSans-Regular' },
  bold: { file: 'NotoSans-Bold.subset.ttf', name: 'NotoSans-Bold' },
};

let loaded;

/** Font bytes and supported code points, read from disk once per process. */
function loadFontData() {
  if (loaded) return loaded;
  loaded = {};
  for (const [weight, { file }] of Object.entries(FILES)) {
    const bytes = fs.readFileSync(path.join(FONT_DIR, file));
    loaded[weight] = { bytes, charSet: new Set(fontkit.create(bytes).characterSet) };
  }
  return loaded;
}

/**
 * Registers fontkit on `doc` and embeds both weights as subsets. A fixed font
 * name keeps the output deterministic (pdf-lib would add a random suffix).
 * Returns `{ regular, bold, charSet, measure }`; `charSet` is the set of code
 * points both weights can draw and `measure.regular/bold` are caching Measurers.
 */
async function embedFonts(doc) {
  const data = loadFontData();
  doc.registerFontkit(fontkit);
  const regular = await doc.embedFont(data.regular.bytes, { subset: true, customName: FILES.regular.name });
  const bold = await doc.embedFont(data.bold.bytes, { subset: true, customName: FILES.bold.name });
  const charSet = new Set([...data.regular.charSet].filter((cp) => data.bold.charSet.has(cp)));
  return { regular, bold, charSet, measure: { regular: new Measurer(regular), bold: new Measurer(bold) } };
}

module.exports = { embedFonts };
