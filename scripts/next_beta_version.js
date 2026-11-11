#!/usr/bin/env node
'use strict';

// Prints the next beta version for the release-beta workflow as GitHub
// output lines (version=…, release_tag=…). The base is the larger of
// package.json's version and the patch after the newest stable tag; the
// beta number continues from the newest vBASE-beta.N tag.

const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');

function parse(version) {
  const match = String(version).trim().match(/^v?(\d+)\.(\d+)\.(\d+)(?:-beta\.(\d+))?$/);
  if (!match) return null;
  return { major: +match[1], minor: +match[2], patch: +match[3], beta: match[4] == null ? null : +match[4] };
}

function compare(a, b) {
  for (const key of ['major', 'minor', 'patch']) {
    if (a[key] !== b[key]) return a[key] - b[key];
  }
  return (a.beta ?? Infinity) - (b.beta ?? Infinity);
}

const format = (v) => `${v.major}.${v.minor}.${v.patch}`;
const tags = (pattern) => execSync(`git tag --list '${pattern}'`, { encoding: 'utf8' })
  .split('\n').map((tag) => parse(tag)).filter(Boolean);

const stable = tags('v[0-9]*.[0-9]*.[0-9]*').filter((v) => v.beta == null).sort(compare).pop()
  || { major: 0, minor: 0, patch: 0, beta: null };
const pkg = JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'package.json'), 'utf8'));
const fromPackage = parse(String(pkg.version).split('-')[0]) || stable;
const afterStable = { ...stable, patch: stable.patch + 1, beta: null };
const base = compare(fromPackage, afterStable) >= 0 ? { ...fromPackage, beta: null } : afterStable;

const betas = tags(`v${format(base)}-beta.*`).sort(compare);
const next = betas.length > 0 ? betas[betas.length - 1].beta + 1 : 0;
const version = `${format(base)}-beta.${next}`;

process.stdout.write(`version=${version}\nrelease_tag=v${version}\n`);
