import 'dart:math' as math;
import 'dart:typed_data';

import 'package:image/image.dart' as img;

// The 64-bit dHash of a photo, made like the server's (Review › Duplicates,
// server/services/ai/duplicates.js): white behind transparency, grayscale,
// resized to 9×8 (Lanczos-3, like sharp), each bit set when a pixel is brighter than its right
// neighbour, rows top to bottom, most significant bit first, as 16 hex
// characters. The phone works from a small, already oriented thumbnail.

/// The dHash of an encoded image (JPEG, PNG, …), or null when it can't be
/// decoded.
String? dHashOfEncoded(Uint8List bytes) {
  final img.Image? image;
  try {
    image = img.decodeImage(bytes);
  } catch (_) {
    return null;
  }
  if (image == null || image.width < 1 || image.height < 1) return null;
  return dHashOfImage(image);
}

String dHashOfImage(img.Image image) {
  final width = image.width;
  final height = image.height;
  // Gray like libvips' sRGB → B_W: luminance from linear light, encoded
  // back to sRGB, which is what is then resized.
  final gray = Float64List(width * height);
  for (final pixel in image) {
    final alpha = image.hasAlpha ? pixel.aNormalized.toDouble() : 1.0;
    double channel(num value) => _toLinear(value / pixel.maxChannelValue * alpha + (1 - alpha));
    final y = 0.2126 * channel(pixel.r) + 0.7152 * channel(pixel.g) + 0.0722 * channel(pixel.b);
    gray[pixel.y * width + pixel.x] = _fromLinear(y) * 255;
  }
  final small = _lanczosResize(gray, width, height, 9, 8);
  final hex = StringBuffer();
  for (var y = 0; y < 8; y++) {
    var byte = 0;
    for (var x = 0; x < 8; x++) {
      final left = small[y * 9 + x].round();
      final right = small[y * 9 + x + 1].round();
      byte = (byte << 1) | (left > right ? 1 : 0);
    }
    hex.write(byte.toRadixString(16).padLeft(2, '0'));
  }
  return hex.toString();
}

/// Bits that differ between two hashes (0–64).
int dHashDistance(String a, String b) {
  var bits = 0;
  for (var i = 0; i < 16; i += 4) {
    var x = int.parse(a.substring(i, i + 4), radix: 16) ^ int.parse(b.substring(i, i + 4), radix: 16);
    while (x != 0) {
      bits += x & 1;
      x >>= 1;
    }
  }
  return bits;
}

double _toLinear(double v) => v <= 0.04045 ? v / 12.92 : math.pow((v + 0.055) / 1.055, 2.4).toDouble();

double _fromLinear(double v) => v <= 0.0031308 ? v * 12.92 : 1.055 * math.pow(v, 1 / 2.4) - 0.055;

/// Lanczos-3 resampling, horizontal pass first, the way libvips reduces an
/// image (sharp's default kernel), so the phone lands on the server's pixels.
Float64List _lanczosResize(Float64List source, int width, int height, int toWidth, int toHeight) {
  final rows = Float64List(toWidth * height);
  for (var y = 0; y < height; y++) {
    final line = _reduce((i) => source[y * width + i], width, toWidth);
    for (var x = 0; x < toWidth; x++) {
      rows[y * toWidth + x] = line[x];
    }
  }
  final result = Float64List(toWidth * toHeight);
  for (var x = 0; x < toWidth; x++) {
    final column = _reduce((j) => rows[j * toWidth + x], height, toHeight);
    for (var y = 0; y < toHeight; y++) {
      result[y * toWidth + x] = column[y];
    }
  }
  return result;
}

List<double> _reduce(double Function(int index) at, int length, int toLength) {
  final factor = length / toLength;
  final radius = (3 * factor).ceil();
  return <double>[for (var o = 0; o < toLength; o++) _sample(at, length, (o + 0.5) * factor - 0.5, factor, radius)];
}

double _sample(double Function(int index) at, int length, double centre, double factor, int radius) {
  var sum = 0.0;
  var weights = 0.0;
  for (var k = centre.floor() - radius; k <= centre.ceil() + radius; k++) {
    final weight = _lanczos3((k - centre) / factor);
    if (weight == 0) continue;
    sum += weight * at(k.clamp(0, length - 1));
    weights += weight;
  }
  return weights == 0 ? 0 : sum / weights;
}

double _lanczos3(double x) {
  if (x == 0) return 1;
  if (x.abs() >= 3) return 0;
  final p = math.pi * x;
  return 3 * math.sin(p) * math.sin(p / 3) / (p * p);
}
