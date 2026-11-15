'use strict';

const { PDFArray, PDFBool, PDFDict, PDFName } = require('pdf-lib');

/**
 * Removes what a finished, sealed document must not carry from its source:
 * open actions, additional actions and document-level JavaScript. (Link
 * annotations are left alone; they are the document's own business.)
 */
function stripActiveContent(doc) {
  const { catalog } = doc;
  catalog.delete(PDFName.of('OpenAction'));
  catalog.delete(PDFName.of('AA'));
  const names = catalog.lookupMaybe(PDFName.of('Names'), PDFDict);
  if (names) names.delete(PDFName.of('JavaScript'));
  for (const page of doc.getPages()) page.node.delete(PDFName.of('AA'));
}

/**
 * Drops every remaining widget annotation from the pages, and the dangling
 * references pdf-lib's own flatten leaves behind in /Annots.
 */
function removeWidgetAnnotations(doc) {
  for (const page of doc.getPages()) {
    const annots = page.node.lookupMaybe(PDFName.of('Annots'), PDFArray);
    if (!annots) continue;
    for (let i = annots.size() - 1; i >= 0; i -= 1) {
      const dict = annots.lookupMaybe(i, PDFDict);
      const subtype = dict && dict.get(PDFName.of('Subtype'));
      if (!dict || subtype === PDFName.of('Widget')) annots.remove(i);
    }
  }
}

/**
 * Bakes the source's AcroForm fields into the page content so nothing in the
 * final file stays editable. Existing appearance streams are kept as they
 * are; when the file asks readers to regenerate them (NeedAppearances) they
 * are regenerated with the bundled Unicode font first. Never throws: a form
 * pdf-lib cannot flatten has its widgets removed instead.
 */
function flattenAcroForm(doc, font) {
  const acroForm = doc.catalog.lookupMaybe(PDFName.of('AcroForm'), PDFDict);
  if (!acroForm) return;
  try {
    const form = doc.getForm();
    if (acroForm.get(PDFName.of('NeedAppearances')) === PDFBool.True) {
      try {
        form.updateFieldAppearances(font);
      } catch {
        // Keep whatever appearances the file already has.
      }
    }
    form.flatten({ updateFieldAppearances: false });
  } catch {
    // Fall through to the blunt removal below.
  }
  removeWidgetAnnotations(doc);
  doc.catalog.delete(PDFName.of('AcroForm'));
}

module.exports = { flattenAcroForm, stripActiveContent };
