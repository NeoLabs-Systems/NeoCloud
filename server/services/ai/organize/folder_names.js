'use strict';

// Folder names compared the way a person reads them, so AI reuses a folder
// instead of making a near-duplicate: case, accents, separators and
// numbering ("01_Finanzen") don't count, plural and singular are the same,
// and common folders are one concept across languages (Photos, Fotos,
// Bilder, Pictures and Images are all "photos"). Also which language the
// person names folders in, and the photo tree's year/month pattern.
// Pure: no database, no model.

// One concept per entry: its name in English and German (for new folders)
// and every spelling that means it. Spellings are compared normalized and
// singularized, so "Rechnung" and "Rechnungen" need only one of them.
const CONCEPTS = [
  { key: 'photos', en: 'Photos', de: 'Fotos', terms: {
    en: ['photos', 'pictures', 'images', 'pics', 'camera roll', 'camera uploads', 'camera', 'dcim', 'gallery'],
    de: ['fotos', 'bilder', 'bild', 'kamera', 'kamera uploads', 'kamerauploads', 'galerie'],
    other: ['imagenes', 'photographies', 'foto'],
  } },
  { key: 'screenshots', en: 'Screenshots', de: 'Bildschirmfotos', terms: {
    en: ['screenshots', 'screen shots'], de: ['bildschirmfotos', 'bildschirmfoto'], other: [],
  } },
  { key: 'videos', en: 'Videos', de: 'Videos', terms: {
    en: ['videos', 'movies', 'clips'], de: ['filme', 'film'], other: [],
  } },
  { key: 'documents', en: 'Documents', de: 'Dokumente', terms: {
    en: ['documents', 'docs', 'paperwork', 'papers'], de: ['dokumente', 'unterlagen', 'papiere', 'akten'],
    other: ['documentos', 'documenti'],
  } },
  { key: 'invoices', en: 'Invoices', de: 'Rechnungen', terms: {
    en: ['invoices', 'bills'], de: ['rechnungen'], other: ['factures', 'facturas', 'fatture'],
  } },
  { key: 'receipts', en: 'Receipts', de: 'Belege', terms: {
    en: ['receipts'], de: ['belege', 'quittungen', 'kassenbons', 'kassenzettel', 'kassenbelege'], other: ['recus', 'recibos'],
  } },
  { key: 'contracts', en: 'Contracts', de: 'Verträge', terms: {
    en: ['contracts', 'agreements'], de: ['vertrage'], other: ['contrats', 'contratos'],
  } },
  { key: 'payslips', en: 'Payslips', de: 'Gehaltsabrechnungen', terms: {
    en: ['payslips', 'pay slips', 'paychecks', 'salary'],
    de: ['gehaltsabrechnungen', 'lohnabrechnungen', 'gehalt', 'lohn', 'entgeltabrechnungen'], other: [],
  } },
  { key: 'statements', en: 'Bank statements', de: 'Kontoauszüge', terms: {
    en: ['statements', 'bank statements'], de: ['kontoauszuge'], other: [],
  } },
  { key: 'taxes', en: 'Taxes', de: 'Steuern', terms: {
    en: ['taxes', 'tax', 'tax returns'], de: ['steuern', 'steuer', 'steuererklarung', 'steuererklarungen'], other: ['impots'],
  } },
  { key: 'insurance', en: 'Insurance', de: 'Versicherungen', terms: {
    en: ['insurance', 'insurances'], de: ['versicherungen'], other: ['assurances', 'seguros'],
  } },
  { key: 'letters', en: 'Letters', de: 'Briefe', terms: {
    en: ['letters', 'correspondence'], de: ['briefe', 'korrespondenz', 'schreiben'], other: [],
  } },
  { key: 'certificates', en: 'Certificates', de: 'Zeugnisse', terms: {
    en: ['certificates', 'diplomas'], de: ['zeugnisse', 'urkunden', 'zertifikate'], other: [],
  } },
  { key: 'manuals', en: 'Manuals', de: 'Anleitungen', terms: {
    en: ['manuals', 'instructions', 'user guides'], de: ['anleitungen', 'bedienungsanleitungen', 'handbucher', 'handbuch'], other: [],
  } },
  { key: 'finance', en: 'Finance', de: 'Finanzen', terms: {
    en: ['finance', 'finances', 'banking', 'money'], de: ['finanzen', 'geld'], other: ['finanzas'],
  } },
  { key: 'music', en: 'Music', de: 'Musik', terms: { en: ['music', 'songs'], de: ['musik'], other: ['musique', 'musica'] } },
  { key: 'downloads', en: 'Downloads', de: 'Downloads', terms: { en: ['downloads'], de: [], other: [] } },
  { key: 'scans', en: 'Scans', de: 'Scans', terms: { en: ['scans', 'scanned'], de: ['gescannt'], other: [] } },
  { key: 'health', en: 'Health', de: 'Gesundheit', terms: { en: ['health', 'medical'], de: ['gesundheit', 'arzt', 'medizin'], other: [] } },
  { key: 'travel', en: 'Travel', de: 'Reisen', terms: { en: ['travel', 'trips', 'vacation', 'holidays'], de: ['reisen', 'urlaub'], other: [] } },
  { key: 'work', en: 'Work', de: 'Arbeit', terms: { en: ['work'], de: ['arbeit', 'beruf'], other: [] } },
  { key: 'car', en: 'Car', de: 'Auto', terms: { en: ['car', 'cars', 'vehicle'], de: ['auto', 'fahrzeug', 'kfz'], other: [] } },
  { key: 'home', en: 'Home', de: 'Wohnung', terms: { en: ['house', 'housing', 'apartment'], de: ['wohnung', 'haus', 'miete'], other: [] } },
  { key: 'presentations', en: 'Presentations', de: 'Präsentationen', terms: {
    en: ['presentations', 'slides'], de: ['prasentationen'], other: [],
  } },
];

// Folders that collect unsorted things: files in them are treated like
// files at the top level and may be filed anywhere.
const INBOX_STEMS = new Set(['inbox', 'eingang', 'unsorted', 'unsortiert', 'misc', 'sonstig', 'sonstige', 'desktop',
  'schreibtisch', 'new', 'neu', 'temp', 'tmp', 'to sort', 'zu sortieren']);
const INBOX_CONCEPTS = new Set(['downloads', 'scans']);
// Concepts whose folders hold photos and videos (the "photo tree").
const MEDIA_CONCEPTS = new Set(['photos', 'screenshots', 'videos']);

const MONTHS = [
  ['jan', 'januar', 'january', 'janvier', 'enero'],
  ['feb', 'februar', 'february', 'fevrier', 'febrero'],
  ['mar', 'marz', 'march', 'mars', 'marzo', 'maerz'],
  ['apr', 'april', 'avril', 'abril'],
  ['may', 'mai', 'mayo'],
  ['jun', 'juni', 'june', 'juin', 'junio'],
  ['jul', 'juli', 'july', 'juillet', 'julio'],
  ['aug', 'august', 'aout', 'agosto'],
  ['sep', 'sept', 'september', 'septembre', 'septiembre'],
  ['oct', 'okt', 'oktober', 'october', 'octobre', 'octubre'],
  ['nov', 'november', 'novembre', 'noviembre'],
  ['dec', 'dez', 'dezember', 'december', 'decembre', 'diciembre'],
];

/** Lowercase, accents and ß folded, separators and leading numbering ("01_", "2. ") gone. */
function normalizeFolderName(name) {
  return String(name || '')
    .normalize('NFKD')
    .replace(/[̀-ͯ]/g, '')
    .replace(/ß/g, 'ss')
    .toLowerCase()
    .replace(/[^\p{L}\p{N}]+/gu, ' ')
    .trim()
    .replace(/^\d{1,3} (?=\p{L})/u, '')
    .replace(/\s+/g, ' ');
}

// The singular of one word, by rules that are safe in English and German.
function singular(word) {
  if (word.length <= 3) return word;
  if (word.endsWith('ungen')) return word.slice(0, -2);
  if (word.endsWith('ies') && word.length > 4) return `${word.slice(0, -3)}y`;
  if (/(ss|ch|sh|x)es$/.test(word)) return word.slice(0, -2);
  let result = word;
  if (result.endsWith('s') && !result.endsWith('ss') && !result.endsWith('us')) result = result.slice(0, -1);
  // German plural -e ("Dokumente") and English -e before -s ("Packages")
  // both drop, so singular and plural always meet.
  if (result.endsWith('e') && result.length > 5) result = result.slice(0, -1);
  return result;
}

function stem(name) {
  return normalizeFolderName(name).split(' ').filter(Boolean).map(singular).join(' ');
}

const CONCEPT_BY_STEM = new Map();
const LANGUAGE_BY_STEM = new Map();
for (const concept of CONCEPTS) {
  for (const [language, terms] of Object.entries(concept.terms)) {
    for (const term of [...terms, concept.en, concept.de]) {
      const key = stem(term);
      if (!CONCEPT_BY_STEM.has(key)) CONCEPT_BY_STEM.set(key, concept);
      if (language !== 'other' && !LANGUAGE_BY_STEM.has(key) && terms.includes(term)) LANGUAGE_BY_STEM.set(key, language);
    }
  }
}

/** The concept a folder name stands for ("photos", "invoices", …), or null. */
function conceptOf(name) {
  return CONCEPT_BY_STEM.get(stem(name))?.key ?? null;
}

/**
 * What two folder names are compared by: the concept for known folders,
 * else the singular normalized name. Equal keys mean "the same folder".
 */
function folderKey(name) {
  const value = stem(name);
  const concept = CONCEPT_BY_STEM.get(value);
  return concept ? `concept:${concept.key}` : value;
}

/** The name for a known concept in `language` ('de' or 'en'), or null for an unknown one. */
function localizedConceptName(name, language) {
  const concept = CONCEPT_BY_STEM.get(stem(name));
  if (!concept) return null;
  return language === 'de' ? concept.de : concept.en;
}

/**
 * The language the person names folders in, from their folder names:
 * 'de', 'en' or null when the names don't say. Umlauts count for German.
 */
function folderLanguage(names) {
  let de = 0;
  let en = 0;
  for (const name of names) {
    if (/[äöüÄÖÜß]/.test(name)) de += 1;
    const language = LANGUAGE_BY_STEM.get(stem(name));
    if (language === 'de') de += 1;
    if (language === 'en') en += 1;
  }
  if (de === en) return null;
  return de > en ? 'de' : 'en';
}

function isInboxName(name) {
  const value = stem(name);
  return INBOX_STEMS.has(value) || INBOX_STEMS.has(normalizeFolderName(name)) || INBOX_CONCEPTS.has(CONCEPT_BY_STEM.get(value)?.key);
}

function isMediaName(name) {
  return MEDIA_CONCEPTS.has(conceptOf(name));
}

/** Whether a folder name is a year ("2026"). */
function isYearName(name, year) {
  return normalizeFolderName(name) === String(year);
}

/** Whether a folder name stands for month 1..12: "03", "3", "2026-03", "März", "03 March". */
function isMonthName(name, month) {
  const value = normalizeFolderName(name);
  const padded = String(month).padStart(2, '0');
  const numeric = new RegExp(`^(\\d{4} )?0?${month}( |$)`);
  if (numeric.test(value) || value === padded) return true;
  return value.split(' ').some((word) => MONTHS[month - 1].includes(word));
}

/**
 * A new folder name the model offered, made safe and generic, or null:
 * at most three words and 40 characters, no digits (no "Invoice 2026-03"),
 * no path characters.
 */
function cleanNewFolderName(value) {
  if (typeof value !== 'string') return null;
  const name = value.replace(/[\u0000-\u001f\u007f/\\:*?"<>|]/g, '').replace(/\s+/g, ' ').trim();
  if (!name || name.length > 40 || name === '.' || name === '..') return null;
  if (/\d/.test(name) || name.split(' ').length > 3) return null;
  return name.charAt(0).toLocaleUpperCase() + name.slice(1);
}

module.exports = {
  cleanNewFolderName,
  conceptOf,
  folderKey,
  folderLanguage,
  isInboxName,
  isMediaName,
  isMonthName,
  isYearName,
  localizedConceptName,
  normalizeFolderName,
};
