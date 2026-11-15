'use strict';

const {
  clip,
  concatTransformationMatrix,
  endPath,
  popGraphicsState,
  pushGraphicsState,
  rectangle,
} = require('pdf-lib');
const { areaToDisplayedRect, boxMatrix, pageGeometry } = require('./geometry');

/**
 * Draws on one PDF page in DISPLAYED coordinates (points, origin bottom-left
 * of the page as the reader sees it), whatever the page's /Rotate and
 * CropBox/MediaBox. A single `cm` maps displayed space to user space, so
 * every drawing call downstream is rotation-agnostic.
 *
 * The page's existing content is wrapped in q/Q first: a source PDF that
 * leaves its graphics state changed (or a stray `cm`) cannot shift our marks.
 */
class PagePainter {
  constructor(page) {
    this.page = page;
    this.geo = pageGeometry(page);
    const context = page.doc.context;
    const start = context.register(page.createContentStream(pushGraphicsState()));
    const end = context.register(page.createContentStream(popGraphicsState()));
    page.node.wrapContentStreams(start, end);
    page.pushOperators(pushGraphicsState(), concatTransformationMatrix(...boxMatrix(this.geo)));
  }

  /** Displayed rectangle { x, y, w, h } (y up) of a fractional area. */
  rectOf(area) {
    return areaToDisplayedRect(area, this.geo);
  }

  /** Runs `draw` with everything clipped to `rect`. */
  withClip(rect, draw) {
    this.page.pushOperators(pushGraphicsState(), rectangle(rect.x, rect.y, rect.w, rect.h), clip(), endPath());
    try {
      draw();
    } finally {
      this.page.pushOperators(popGraphicsState());
    }
  }

  /** Must be called once all drawing is done. */
  finish() {
    this.page.pushOperators(popGraphicsState());
  }
}

module.exports = { PagePainter };
