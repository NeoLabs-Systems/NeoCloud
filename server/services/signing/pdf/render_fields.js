'use strict';

// Draws the filled values of signing fields onto their pages. One renderer
// per field type; an unknown type or an unusable value draws nothing and
// never throws, so one bad value cannot block finalisation.

const { drawCheckbox, drawRadio } = require('./draw_marks');
const { drawFileLabel, drawImageContained } = require('./draw_media');
const { drawCells, drawTextBox } = require('./draw_text');
const { formatDate, formatNumber } = require('./format');
const { toBytes } = require('./normalize');

const TRUTHY = new Set(['true', '1', 'on', 'yes', 'checked']);
const isChecked = (value) => value === true || value === 1 || TRUTHY.has(String(value).toLowerCase());

const hasText = (value) => value !== undefined && value !== null && String(value) !== '';

function labelOf(field, value) {
  const option = field.options.find((candidate) => candidate.value === String(value));
  return option ? option.label : String(value);
}

/** A choice value as a list of strings. */
function chosenValues(value) {
  if (Array.isArray(value)) return value.map(String);
  return hasText(value) ? [String(value)] : [];
}

/** Which option an area stands for (radio/multiple with one area per option), or undefined. */
function optionOfArea(field, area, index, areaCount) {
  if (area.option !== undefined && area.option !== null) return String(area.option);
  if (areaCount > 1 && field.options.length === areaCount) return field.options[index].value;
  return undefined;
}

function drawText(ctx, painter, rect, field, text) {
  drawTextBox(ctx, painter, rect, text, field.prefs);
}

const RENDERERS = {
  text: (ctx, painter, rect, field, entry) => drawText(ctx, painter, rect, field, entry.value),
  phone: (ctx, painter, rect, field, entry) => drawText(ctx, painter, rect, field, entry.value),
  number: (ctx, painter, rect, field, entry) =>
    drawText(ctx, painter, rect, field, formatNumber(entry.value, field.prefs.format)),
  date: (ctx, painter, rect, field, entry) =>
    drawText(ctx, painter, rect, field, formatDate(entry.value, field.prefs.format, ctx.locale)),
  cells: (ctx, painter, rect, field, entry) => drawCells(ctx, painter, rect, entry.value, field.prefs),
  heading: (ctx, painter, rect, field) =>
    drawTextBox(ctx, painter, rect, field.text ?? field.defaultValue ?? field.name, field.prefs, {
      boldByDefault: true,
      defaultSize: 14,
    }),
  checkbox: (ctx, painter, rect, field, entry) => drawCheckbox(painter, rect, isChecked(entry.value), field.prefs),
  radio: (ctx, painter, rect, field, entry, area, index, areaCount) => {
    const option = optionOfArea(field, area, index, areaCount);
    if (option === undefined) drawText(ctx, painter, rect, field, labelOf(field, entry.value));
    else drawRadio(painter, rect, String(entry.value) === option, field.prefs);
  },
  select: (ctx, painter, rect, field, entry) => drawText(ctx, painter, rect, field, labelOf(field, entry.value)),
  multiple: (ctx, painter, rect, field, entry, area, index, areaCount) => {
    const chosen = chosenValues(entry.value);
    const option = optionOfArea(field, area, index, areaCount);
    if (option === undefined) {
      drawText(ctx, painter, rect, field, chosen.map((value) => labelOf(field, value)).join(', '));
    } else {
      drawCheckbox(painter, rect, chosen.includes(option), field.prefs);
    }
  },
};

/** Signature, initials, stamp and image share one drawing path. */
async function renderImageField(ctx, painter, rect, field, entry) {
  const bytes = toBytes(entry.bytes) || toBytes(entry.value && entry.value.bytes);
  if (bytes && (await drawImageContained(ctx, painter, rect, bytes))) return;
  // A typed signature that arrived without its rendered image: print the name.
  const typed = entry.value && typeof entry.value === 'object' ? entry.value.text : entry.value;
  if (hasText(typed) && typeof typed === 'string') drawTextBox(ctx, painter, rect, typed, field.prefs);
}

function renderFileField(ctx, painter, rect, field, entry, files) {
  const names = files.map((file) => file.filename);
  drawFileLabel(ctx, painter, rect, names.join(', '), field.prefs);
}

function hasContent(field, entry) {
  if (field.type === 'heading') return true;
  if (!entry) return false;
  if (['signature', 'initials', 'stamp', 'image'].includes(field.type)) {
    return Boolean(toBytes(entry.bytes) || toBytes(entry.value && entry.value.bytes) || hasText(entry.value));
  }
  if (field.type === 'file') return Boolean(entry.files || toBytes(entry.bytes));
  if (field.type === 'checkbox') return true; // an empty box documents "not ticked"
  if (field.type === 'multiple') return chosenValues(entry.value).length > 0 || field.areas.length > 1;
  if (field.type === 'radio') return hasText(entry.value);
  return hasText(entry.value);
}

/**
 * Draws every area of `field` with its value. `painters` maps a page index to
 * its PagePainter; `fileList` is the already-normalised attachment list for
 * file fields.
 */
async function renderField(ctx, painters, field, entry, fileList) {
  if (!hasContent(field, entry)) return;
  const areaCount = field.areas.length;
  for (let index = 0; index < areaCount; index += 1) {
    const area = field.areas[index];
    const painter = painters.get(area.page);
    if (!painter) continue; // area on a page the PDF does not have
    const rect = painter.rectOf(area);
    if (['signature', 'initials', 'stamp', 'image'].includes(field.type)) {
      await renderImageField(ctx, painter, rect, field, entry);
    } else if (field.type === 'file') {
      renderFileField(ctx, painter, rect, field, entry, fileList);
    } else if (RENDERERS[field.type]) {
      RENDERERS[field.type](ctx, painter, rect, field, entry || {}, area, index, areaCount);
    }
  }
}

module.exports = { renderField };
