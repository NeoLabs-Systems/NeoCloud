'use strict';

// Which of a signer's saved details belongs in which field. A field is
// matched by its name (case, accents and punctuation do not matter; English
// and German synonyms are known) or, for signatures and initials, by its type.
// Pure functions: the database and the profile are the caller's business.

const { typeOf } = require('./field_types');

// Field types a name can fill (what a person types or picks as text).
const NAMED_TYPES = ['text', 'phone', 'number', 'select', 'radio'];

// Each entry: the key it fills and the phrases that mean it. A phrase listed
// in `exact` must be the whole name ("name" alone, not "name of the bank").
const BUILTIN = [
  { key: 'email', phrases: ['email', 'e mail', 'email address', 'e mail address', 'e mail adresse', 'email adresse', 'emailadresse', 'mailadresse', 'mail'], exact: ['mail'] },
  { key: 'phone', phrases: ['phone', 'phone number', 'telephone', 'mobile', 'mobile number', 'telefon', 'telefonnummer', 'mobil', 'mobilnummer', 'handy', 'handynummer', 'rufnummer', 'tel'], exact: ['tel', 'mobil', 'handy'] },
  { key: 'firstName', phrases: ['first name', 'firstname', 'given name', 'forename', 'vorname'], exact: [] },
  { key: 'lastName', phrases: ['last name', 'lastname', 'surname', 'family name', 'nachname', 'familienname'], exact: [] },
  {
    key: 'fullName',
    phrases: ['full name', 'fullname', 'your name', 'your full name', 'signer', 'signer name', 'name', 'vollstandiger name', 'vor und nachname', 'vorname und nachname', 'vorname nachname', 'name vorname', 'nachname vorname', 'unterzeichner', 'ihr name', 'dein name'],
    exact: ['name', 'signer', 'unterzeichner'],
  },
];

// Presets: a saved label matches every phrase of its group (a label in none
// of them matches itself). Short phrases must be the whole field name.
const PRESET_GROUPS = [
  ['address', 'street address', 'postal address', 'home address', 'adresse', 'anschrift', 'postanschrift', 'wohnadresse', 'strasse', 'street'],
  ['company', 'company name', 'organization', 'organisation', 'employer', 'firma', 'firmenname', 'unternehmen', 'arbeitgeber'],
  ['title', 'job title', 'position', 'role', 'titel', 'berufsbezeichnung', 'funktion'],
  ['city', 'town', 'ort', 'stadt', 'wohnort'],
  ['zip', 'zip code', 'postal code', 'postcode', 'plz', 'postleitzahl'],
  ['country', 'land'],
  ['date of birth', 'birth date', 'birthday', 'dob', 'geburtsdatum', 'geboren am'],
  ['iban'],
  ['tax id', 'tax number', 'steuer id', 'steuernummer', 'steuer identifikationsnummer'],
  ['vat id', 'vat number', 'ust id', 'ustid', 'umsatzsteuer id'],
];

const MIN_PARTIAL_LENGTH = 5;

/** Lower case, no accents, "ß" as "ss", every run of other characters one space. */
function normalizeName(text) {
  return String(text ?? '')
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .toLowerCase()
    .replace(/ß/g, 'ss')
    .replace(/[^a-z0-9]+/g, ' ')
    .trim();
}

const containsWords = (name, phrase) => ` ${name} `.includes(` ${phrase} `);

// How well a phrase fits a field name: 0 for not at all, higher is better; a whole-name match beats any partial one.
function fit(name, phrase, exactOnly) {
  if (name === phrase) return 1000 + phrase.length;
  if (exactOnly || phrase.length < MIN_PARTIAL_LENGTH) return 0;
  return containsWords(name, phrase) ? phrase.length : 0;
}

function phrasesOfPreset(label) {
  const own = normalizeName(label);
  const group = PRESET_GROUPS.find((phrases) => phrases.includes(own));
  return group || [own];
}

function candidates(presets) {
  const list = [];
  presets.forEach((preset) => {
    for (const phrase of phrasesOfPreset(preset.label)) list.push({ phrase, exactOnly: false, source: { preset } });
  });
  for (const entry of BUILTIN) {
    for (const phrase of entry.phrases) list.push({ phrase, exactOnly: entry.exact.includes(phrase), source: { key: entry.key } });
  }
  return list;
}

/** The best source (a preset or a builtin key) for a field name, or null. Presets win a tie. */
function matchName(fieldName, presets) {
  const name = normalizeName(fieldName);
  if (!name) return null;
  let best = null;
  for (const candidate of candidates(presets)) {
    const score = fit(name, candidate.phrase, candidate.exactOnly);
    if (score > 0 && (!best || score > best.score)) best = { score, source: candidate.source };
  }
  return best ? best.source : null;
}

function splitName(displayName) {
  const parts = String(displayName || '').trim().split(/\s+/).filter(Boolean);
  if (parts.length < 2) return { first: parts[0] || '', last: '' };
  return { first: parts.slice(0, -1).join(' '), last: parts[parts.length - 1] };
}

/** The text a source stands for in the profile's details ('' when the profile has none). */
function valueOfSource(source, details) {
  if (source.preset) return source.preset.value;
  const names = splitName(details.displayName);
  const values = {
    email: details.email, phone: details.phone, fullName: details.displayName, firstName: names.first, lastName: names.last,
  };
  return values[source.key] || '';
}

/** What kind of fill a field gets: 'image' (signature, initials), 'today' (a date set to fill itself), 'named' or null. */
function fillKind(field) {
  if (field.type === 'signature' || field.type === 'initials') return 'image';
  if (field.type === 'date') return field.prefs.autoToday === true ? 'today' : null;
  return NAMED_TYPES.includes(field.type) && typeOf(field.type).storage === 'inline' ? 'named' : null;
}

module.exports = { fillKind, matchName, normalizeName, valueOfSource };
