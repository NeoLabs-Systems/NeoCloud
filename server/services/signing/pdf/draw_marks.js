'use strict';

const { rgb } = require('pdf-lib');
const { colorOf } = require('./draw_text');

const OUTLINE = rgb(0.25, 0.25, 0.3);

/** Largest square inside `rect`, centred. */
function squareIn(rect) {
  const side = Math.min(rect.w, rect.h);
  return { x: rect.x + (rect.w - side) / 2, y: rect.y + (rect.h - side) / 2, side };
}

/** An outlined box with a check mark when `checked`. */
function drawCheckbox(painter, rect, checked, prefs) {
  const { x, y, side } = squareIn(rect);
  painter.page.drawRectangle({ x, y, width: side, height: side, borderColor: OUTLINE, borderWidth: Math.max(0.6, side * 0.05) });
  if (!checked) return;
  const color = colorOf(prefs.color);
  const thickness = Math.max(1, side * 0.11);
  const at = (fx, fy) => ({ x: x + fx * side, y: y + fy * side });
  painter.page.drawLine({ start: at(0.2, 0.52), end: at(0.42, 0.28), thickness, color });
  painter.page.drawLine({ start: at(0.42, 0.28), end: at(0.8, 0.76), thickness, color });
}

/** An outlined circle with a dot when `chosen`. */
function drawRadio(painter, rect, chosen, prefs) {
  const { x, y, side } = squareIn(rect);
  const center = { x: x + side / 2, y: y + side / 2 };
  const radius = side / 2 - Math.max(0.3, side * 0.03);
  painter.page.drawCircle({ ...center, size: radius, borderColor: OUTLINE, borderWidth: Math.max(0.6, side * 0.05) });
  if (chosen) painter.page.drawCircle({ ...center, size: radius * 0.52, color: colorOf(prefs.color) });
}

module.exports = { drawCheckbox, drawRadio };
