/// What kind of file can become a signing document.
enum SignableKind {
  /// Used as it is.
  pdf,

  /// Becomes an A4 page.
  image,

  /// Converted by the server when it can (see `SigningCapabilities.office`).
  office,

  none;

  bool get isSignable => this != none;
}

const Set<String> _imageExtensions = <String>{'png', 'jpg', 'jpeg', 'webp', 'gif', 'bmp', 'tif', 'tiff', 'avif', 'heic', 'heif'};
const Set<String> _officeExtensions = <String>{'doc', 'docx', 'odt', 'rtf', 'xls', 'xlsx', 'ods', 'ppt', 'pptx', 'odp'};

SignableKind signableKindOf(String name, String? mime) {
  final type = (mime ?? '').toLowerCase();
  final dot = name.lastIndexOf('.');
  final extension = dot < 0 ? '' : name.substring(dot + 1).toLowerCase();
  if (type == 'application/pdf' || extension == 'pdf') return SignableKind.pdf;
  if (type.startsWith('image/') && !type.contains('svg') || _imageExtensions.contains(extension)) return SignableKind.image;
  if (type.contains('officedocument') || type.contains('msword') || type.contains('ms-excel') || type.contains('ms-powerpoint') || type.contains('opendocument') || _officeExtensions.contains(extension)) {
    return SignableKind.office;
  }
  return SignableKind.none;
}
