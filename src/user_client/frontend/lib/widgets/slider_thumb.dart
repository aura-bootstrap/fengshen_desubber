import 'package:flutter/material.dart';

/// 白芯 + 主色描边环的滑杆圆球：浅色底上清晰、比实心球克制。
void paintRingThumb(Canvas canvas, Offset center, double radius,
    double ringWidth, Color fill, Color ring) {
  // 细投影
  canvas.drawCircle(
      center,
      radius + ringWidth,
      Paint()
        ..color = const Color(0x1F000000)
        ..maskFilter = const MaskFilter.blur(BlurStyle.normal, 1.5));
  // 描边环
  canvas.drawCircle(center, radius + ringWidth, Paint()..color = ring);
  // 内芯
  canvas.drawCircle(center, radius, Paint()..color = fill);
}

class RingThumbShape extends SliderComponentShape {
  final double radius, ringWidth;
  final Color fill, ring;
  const RingThumbShape({
    this.radius = 6.5,
    this.ringWidth = 2,
    required this.fill,
    required this.ring,
  });

  @override
  Size getPreferredSize(bool isEnabled, bool isDiscrete) =>
      Size.fromRadius(radius + ringWidth);

  @override
  void paint(
    PaintingContext context,
    Offset center, {
    required Animation<double> activationAnimation,
    required Animation<double> enableAnimation,
    required bool isDiscrete,
    required TextPainter labelPainter,
    required RenderBox parentBox,
    required SliderThemeData sliderTheme,
    required TextDirection textDirection,
    required double value,
    required double textScaleFactor,
    required Size sizeWithOverflow,
  }) {
    paintRingThumb(context.canvas, center, radius, ringWidth, fill, ring);
  }
}

class RingRangeThumbShape extends RangeSliderThumbShape {
  final double radius, ringWidth;
  final Color fill, ring;
  const RingRangeThumbShape({
    this.radius = 6.5,
    this.ringWidth = 2,
    required this.fill,
    required this.ring,
  });

  @override
  Size getPreferredSize(bool isEnabled, bool isDiscrete) =>
      Size.fromRadius(radius + ringWidth);

  @override
  void paint(
    PaintingContext context,
    Offset center, {
    required Animation<double> activationAnimation,
    required Animation<double> enableAnimation,
    bool isDiscrete = false,
    bool isEnabled = false,
    bool? isOnTop,
    TextDirection? textDirection,
    required SliderThemeData sliderTheme,
    Thumb? thumb,
    bool? isPressed,
  }) {
    paintRingThumb(context.canvas, center, radius, ringWidth, fill, ring);
  }
}
