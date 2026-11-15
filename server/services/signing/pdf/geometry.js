'use strict';

// Page geometry. Clients and the database speak in the DISPLAYED page: the
// CropBox (else MediaBox) with /Rotate applied, origin top-left, y down,
// fractions 0..1. PDF content lives in unrotated user space (origin bottom
// left, y up, offset by the box corner). This module is the only place that
// converts between the two.
//
//   rotate 0 :  ux = bx + X          uy = by + Y
//   rotate 90:  ux = bx + bw - Y     uy = by + X
//   rotate 180: ux = bx + bw - X     uy = by + bh - Y   (see boxMatrix)
//   rotate 270: ux = bx + Y          uy = by + bh - X
//
// where (X, Y) is a point of the displayed page measured from its BOTTOM-left
// (y up) and (bx, by, bw, bh) is the visible box in user space.

function normalizeRotation(angle) {
  const snapped = Math.round((Number(angle) || 0) / 90) * 90;
  return ((snapped % 360) + 360) % 360;
}

function normalizedRect(rect) {
  const x1 = Math.min(rect.x, rect.x + rect.width);
  const y1 = Math.min(rect.y, rect.y + rect.height);
  return { x: x1, y: y1, w: Math.abs(rect.width), h: Math.abs(rect.height) };
}

function intersect(a, b) {
  const x = Math.max(a.x, b.x);
  const y = Math.max(a.y, b.y);
  const w = Math.min(a.x + a.w, b.x + b.w) - x;
  const h = Math.min(a.y + a.h, b.y + b.h) - y;
  return w > 0 && h > 0 ? { x, y, w, h } : null;
}

/**
 * The geometry of a pdf-lib page: visible box, rotation and displayed size.
 * Broken boxes (zero size) fall back to US Letter.
 */
function pageGeometry(page) {
  const media = normalizedRect(page.getMediaBox());
  const usableMedia = media.w > 0 && media.h > 0 ? media : { x: 0, y: 0, w: 612, h: 792 };
  const crop = normalizedRect(page.getCropBox());
  const box = intersect(usableMedia, crop) || usableMedia;
  const rotate = normalizeRotation(page.getRotation().angle);
  const swapped = rotate === 90 || rotate === 270;
  return {
    box,
    rotate,
    width: swapped ? box.h : box.w,
    height: swapped ? box.w : box.h,
  };
}

/**
 * [a, b, c, d, e, f] mapping displayed coordinates (origin bottom-left, y up,
 * in points) to user space: ux = a*X + c*Y + e, uy = b*X + d*Y + f. Content
 * drawn after `cm` with this matrix appears upright on the displayed page.
 */
function boxMatrix(geo) {
  const { x: bx, y: by, w: bw, h: bh } = geo.box;
  switch (geo.rotate) {
    case 90:
      return [0, 1, -1, 0, bx + bw, by];
    case 180:
      return [-1, 0, 0, -1, bx + bw, by + bh];
    case 270:
      return [0, -1, 1, 0, bx, by + bh];
    default:
      return [1, 0, 0, 1, bx, by];
  }
}

/** A fractional top-left area -> rectangle in displayed space (y up, points). */
function areaToDisplayedRect(area, geo) {
  const w = area.w * geo.width;
  const h = area.h * geo.height;
  return { x: area.x * geo.width, y: geo.height - area.y * geo.height - h, w, h };
}

/** User-space point -> displayed point measured from the TOP-left (y down). */
function userToDisplayedTopLeft(ux, uy, geo) {
  const { x: bx, y: by, w: bw, h: bh } = geo.box;
  switch (geo.rotate) {
    case 90:
      return { dx: uy - by, dy: ux - bx };
    case 180:
      return { dx: bx + bw - ux, dy: uy - by };
    case 270:
      return { dx: by + bh - uy, dy: bx + bw - ux };
    default:
      return { dx: ux - bx, dy: by + bh - uy };
  }
}

/** A user-space rectangle -> fractional top-left area, clamped to 0..1. */
function userRectToArea(rect, geo) {
  const a = userToDisplayedTopLeft(rect.x, rect.y, geo);
  const b = userToDisplayedTopLeft(rect.x + rect.width, rect.y + rect.height, geo);
  const round = (n) => Math.round(Math.min(1, Math.max(0, n)) * 1e5) / 1e5;
  const x = round(Math.min(a.dx, b.dx) / geo.width);
  const y = round(Math.min(a.dy, b.dy) / geo.height);
  const x2 = round(Math.max(a.dx, b.dx) / geo.width);
  const y2 = round(Math.max(a.dy, b.dy) / geo.height);
  return { x, y, w: round(x2 - x), h: round(y2 - y) };
}

module.exports = { areaToDisplayedRect, boxMatrix, pageGeometry, userRectToArea, userToDisplayedTopLeft };
