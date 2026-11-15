'use strict';

const {
  PDFCheckBox,
  PDFDropdown,
  PDFName,
  PDFNumber,
  PDFOptionList,
  PDFRadioGroup,
  PDFSignature,
  PDFTextField,
  PDFArray,
  PDFDict,
  TextAlignment,
} = require('pdf-lib');
const { loadPdfDocument } = require('./load');
const { pageGeometry, userRectToArea } = require('./geometry');

const FLAG_HIDDEN = 2;
const FLAG_NO_VIEW = 32;
const MIN_FRACTION = 0.001;

/** Identity map widget dictionary -> page index, built from each page's /Annots. */
function mapAnnotationsToPages(doc) {
  const byDict = new Map();
  doc.getPages().forEach((page, index) => {
    const annots = page.node.lookupMaybe(PDFName.of('Annots'), PDFArray);
    if (!annots) return;
    for (let i = 0; i < annots.size(); i += 1) {
      const dict = annots.lookupMaybe(i, PDFDict);
      if (dict) byDict.set(dict, index);
    }
  });
  return byDict;
}

function widgetIsVisible(widget) {
  const flags = widget.dict.lookupMaybe(PDFName.of('F'), PDFNumber);
  const value = flags ? flags.asNumber() : 0;
  return (value & (FLAG_HIDDEN | FLAG_NO_VIEW)) === 0;
}

/** Fractional areas of a field's visible widgets; `optionOf(widget)` tags radio options. */
function collectAreas(doc, field, pageOf, geometries, optionOf) {
  const areas = [];
  for (const widget of field.acroField.getWidgets()) {
    if (!widgetIsVisible(widget)) continue;
    const pageIndex = pageOf.get(widget.dict);
    if (pageIndex === undefined) continue;
    const rect = widget.getRectangle();
    const area = userRectToArea(rect, geometries[pageIndex]);
    if (area.w < MIN_FRACTION || area.h < MIN_FRACTION) continue;
    const entry = { page: pageIndex, ...area };
    const option = optionOf ? optionOf(widget) : undefined;
    if (option !== undefined) entry.option = option;
    areas.push(entry);
  }
  return areas;
}

/** Font size and colour out of a default appearance string such as "/Helv 12 Tf 0 0 1 rg". */
function prefsFromDefaultAppearance(da) {
  const prefs = {};
  if (!da) return prefs;
  const size = /([\d.]+)\s+Tf/.exec(da);
  if (size && Number(size[1]) > 0) prefs.fontSize = Number(size[1]);
  const rgb = /([\d.]+)\s+([\d.]+)\s+([\d.]+)\s+rg/.exec(da);
  const gray = /([\d.]+)\s+g(?:\s|$)/.exec(da);
  const channels = rgb ? [rgb[1], rgb[2], rgb[3]] : gray ? [gray[1], gray[1], gray[1]] : null;
  if (channels) {
    const hex = channels.map((c) => Math.round(Math.min(1, Number(c)) * 255).toString(16).padStart(2, '0')).join('');
    if (hex !== '000000') prefs.color = `#${hex}`;
  }
  return prefs;
}

function textFieldSpec(field) {
  const prefs = prefsFromDefaultAppearance(field.acroField.getDefaultAppearance());
  const alignment = field.getAlignment();
  if (alignment === TextAlignment.Center) prefs.align = 'center';
  if (alignment === TextAlignment.Right) prefs.align = 'right';
  const maxLength = field.getMaxLength();
  if (field.isCombed() && maxLength) return { type: 'cells', prefs: { ...prefs, cells: maxLength } };
  if (field.isMultiline()) prefs.multiline = true;
  return { type: 'text', prefs };
}

/**
 * A widget's on-state name is the option itself, or - when the field has an
 * /Opt array, as pdf-lib writes - the option's index into it.
 */
function radioOptionResolver(field, options) {
  return (widget) => {
    const on = widget.getOnValue();
    if (!on) return undefined;
    const name = on.decodeText();
    return options.includes(name) ? name : options[Number(name)] ?? name;
  };
}

function describeField(field) {
  if (field instanceof PDFTextField) return textFieldSpec(field);
  if (field instanceof PDFCheckBox) return { type: 'checkbox', prefs: {} };
  if (field instanceof PDFRadioGroup) return { type: 'radio', prefs: {}, options: field.getOptions() };
  if (field instanceof PDFDropdown) return { type: 'select', prefs: {}, options: field.getOptions() };
  if (field instanceof PDFOptionList) {
    return { type: field.isMultiselect() ? 'multiple' : 'select', prefs: {}, options: field.getOptions() };
  }
  if (field instanceof PDFSignature) return { type: 'signature', prefs: {} };
  return null; // push buttons and anything exotic
}

/** Reading order: page, then top to bottom, then left to right. */
function byReadingOrder(a, b) {
  const first = (field) => field.areas[0];
  return first(a).page - first(b).page || first(a).y - first(b).y || first(a).x - first(b).x;
}

/**
 * Existing AcroForm widgets as draft signing fields (never assigned to a
 * party). Read-only fields and push buttons are left alone: they are flattened
 * into the page when the document is finalised.
 */
async function importAcroFields(bytes) {
  const doc = await loadPdfDocument(bytes);
  if (!doc.catalog.has(PDFName.of('AcroForm'))) return [];

  const pageOf = mapAnnotationsToPages(doc);
  const geometries = doc.getPages().map(pageGeometry);
  let fields;
  try {
    fields = doc.getForm().getFields();
  } catch {
    return []; // a damaged field tree is not worth failing the upload for
  }

  const imported = [];
  for (const field of fields) {
    if (field.isReadOnly()) continue;
    const spec = describeField(field);
    if (!spec) continue;
    const optionOf = field instanceof PDFRadioGroup ? radioOptionResolver(field, spec.options) : undefined;
    const areas = collectAreas(doc, field, pageOf, geometries, optionOf);
    if (areas.length === 0) continue;
    const entry = { type: spec.type, name: field.getName(), required: field.isRequired(), areas, prefs: spec.prefs };
    if (spec.options) entry.options = spec.options;
    imported.push(entry);
  }
  return imported.sort(byReadingOrder);
}

module.exports = { importAcroFields };
