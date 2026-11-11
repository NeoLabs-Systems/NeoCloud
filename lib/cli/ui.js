'use strict';

// Terminal output for the neocloud CLI in the NeoLabs palette (gold accent,
// soft green/red marks). Colors drop out when stdout is not a terminal.

const COLORS = Object.freeze({
  accent: [225, 176, 82],
  ok: [116, 192, 124],
  error: [222, 138, 120],
  muted: [174, 183, 166],
  faint: [126, 136, 119],
});

const useColor = process.stdout.isTTY && !('NO_COLOR' in process.env) && process.env.TERM !== 'dumb';

function paint(name, text) {
  if (!useColor) return String(text);
  const [r, g, b] = COLORS[name];
  return `\x1b[38;2;${r};${g};${b}m${text}\x1b[0m`;
}

function bold(text) {
  return useColor ? `\x1b[1m${text}\x1b[0m` : String(text);
}

const ui = {
  title: (text) => console.log(`\n${paint('accent', '◆')} ${bold(text)}`),
  ok: (text) => console.log(`  ${paint('ok', '✓')} ${text}`),
  warn: (text) => console.log(`  ${paint('accent', '!')} ${text}`),
  error: (text) => console.error(`  ${paint('error', '✕')} ${text}`),
  info: (text) => console.log(`  ${paint('faint', '•')} ${text}`),
  row: (label, value) => console.log(`  ${paint('muted', label.padEnd(14))} ${value}`),
  plain: (text = '') => console.log(text),
  accent: (text) => paint('accent', text),
  muted: (text) => paint('muted', text),
};

module.exports = ui;
