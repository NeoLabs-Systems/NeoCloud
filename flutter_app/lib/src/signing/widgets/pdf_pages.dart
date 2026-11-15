import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:pdfrx/pdfrx.dart';

import '../../l10n/app_language.dart';
import '../../theme/palette.dart';
import 'page_list.dart';

/// Renders a PDF's pages with pdfrx (PDFium on web, Android and iOS) as a
/// [SigningPageList]: lazy, cached per page, zoomable, with an overlay of
/// your own on each page.
///
/// ```dart
/// SigningPdfPages(
///   bytes: pdfBytes,
///   controller: pages,
///   overlayBuilder: (context, pageIndex, pageSize) => <Widget>[...],
/// )
/// ```
///
/// Page sizes are those pdfrx reports: already rotated, so they match the
/// "page as displayed" the field fractions are stored against.
class SigningPdfPages extends StatefulWidget {
  const SigningPdfPages({
    super.key,
    required this.bytes,
    this.overlayBuilder,
    this.controller,
    this.onLoaded,
    this.padding = const EdgeInsets.symmetric(horizontal: 16, vertical: 16),
    this.pageGap = 16,
    this.maxPageWidth = 1000,
  });

  /// The PDF file. A different list (not just equal content) reloads.
  final Uint8List bytes;
  final PageOverlayBuilder? overlayBuilder;
  final SigningPageController? controller;

  /// Called once the pages are known, with each page's size in points.
  final ValueChanged<List<Size>>? onLoaded;
  final EdgeInsets padding;
  final double pageGap;
  final double maxPageWidth;

  @override
  State<SigningPdfPages> createState() => _SigningPdfPagesState();
}

class _SigningPdfPagesState extends State<SigningPdfPages> {
  PdfDocument? _document;
  List<Size>? _sizes;
  Object? _error;
  int _generation = 0;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void didUpdateWidget(covariant SigningPdfPages old) {
    super.didUpdateWidget(old);
    if (!identical(old.bytes, widget.bytes)) _load();
  }

  @override
  void dispose() {
    _generation++;
    _document?.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    final generation = ++_generation;
    final previous = _document;
    setState(() {
      _error = null;
      _sizes = null;
    });
    try {
      await pdfrxFlutterInitialize();
      final document = await PdfDocument.openData(widget.bytes, sourceName: 'signing.pdf');
      if (!mounted || generation != _generation) {
        await document.dispose();
        return;
      }
      final sizes = <Size>[for (final page in document.pages) Size(page.width, page.height)];
      setState(() {
        _document = document;
        _sizes = sizes;
      });
      await previous?.dispose();
      widget.onLoaded?.call(sizes);
    } catch (error) {
      if (mounted && generation == _generation) setState(() => _error = error);
    }
  }

  @override
  Widget build(BuildContext context) {
    final palette = paletteFor(Theme.of(context).brightness);
    final sizes = _sizes;
    final document = _document;
    if (_error != null) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Text(appStrings.signingPdfLoadFailed, textAlign: TextAlign.center, style: TextStyle(color: palette.textSecondary)),
        ),
      );
    }
    if (sizes == null || document == null) return const Center(child: CircularProgressIndicator());
    return SigningPageList(
      pageSizes: sizes,
      controller: widget.controller,
      overlayBuilder: widget.overlayBuilder,
      padding: widget.padding,
      pageGap: widget.pageGap,
      maxPageWidth: widget.maxPageWidth,
      pageContent: (context, index, size) => PdfPageView(
        document: document,
        pageNumber: index + 1,
        maximumDpi: 220,
        alignment: Alignment.topLeft,
        decorationBuilder: (context, pageSize, page, image) => image ?? const SizedBox.shrink(),
      ),
    );
  }
}
