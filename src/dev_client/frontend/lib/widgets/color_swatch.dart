import 'package:flutter/material.dart';

import '../models.dart';
import '../theme.dart';

/// 颜色库编辑器(色带/文字颜色库):色块列表 + 「添加」弹自绘拾色器。
/// 色块悬停出现 × 逐条删除;点色块重开拾色器编辑该条;添加/编辑与存量去重(忽略大小写)。
/// 值一律以 "0xRRGGBB" 大写回写;存量英文色名/#hex 仅作显示解析,编辑后归一为 0x 形式。
class ColorSwatchList extends StatelessWidget {
  final FieldDef def;
  final List<String> colors;
  final ValueChanged<List<String>> onChanged;
  const ColorSwatchList({super.key, required this.def, required this.colors,
      required this.onChanged});

  static String encode(Color c) =>
      '0x${c.toARGB32().toRadixString(16).padLeft(8, '0').substring(2).toUpperCase()}';

  /// 显示解析:0xRRGGBB / #RRGGBB / 常见英文色名;无法解析返回 null(显示为占位灰)。
  static Color? decode(String s) {
    final v = s.trim();
    final hex = RegExp(r'^(?:0x|#)([0-9a-fA-F]{6})$').firstMatch(v);
    if (hex != null) {
      return Color(0xFF000000 | int.parse(hex.group(1)!, radix: 16));
    }
    const names = {
      'white': 0xFFFFFFFF, 'black': 0xFF000000, 'red': 0xFFFF0000,
      'green': 0xFF00FF00, 'blue': 0xFF0000FF, 'yellow': 0xFFFFFF00,
      'orange': 0xFFFFA500, 'purple': 0xFF800080, 'pink': 0xFFFFC0CB,
      'cyan': 0xFF00FFFF, 'gray': 0xFF808080, 'grey': 0xFF808080,
    };
    final n = names[v.toLowerCase()];
    return n == null ? null : Color(n);
  }

  void _edit(BuildContext context, int index) async {
    final initial = decode(colors[index]) ?? const Color(0xFF00FF00);
    final picked = await showDialog<Color>(
      context: context,
      builder: (_) => ColorPickerDialog(initial: initial, title: '编辑颜色'),
    );
    if (picked == null) return;
    final hex = encode(picked);
    final next = List<String>.from(colors);
    // 与其它条去重:命中则删除旧位(相当于移动)
    final dup = next.indexWhere((e) => e.toUpperCase() == hex);
    if (dup >= 0 && dup != index) next.removeAt(dup);
    if (dup >= 0 && dup < index) {
      next[index - 1] = hex;
    } else {
      next[index] = hex;
    }
    onChanged(next);
  }

  void _add(BuildContext context) async {
    final picked = await showDialog<Color>(
      context: context,
      builder: (_) => ColorPickerDialog(
          initial: colors.isEmpty
              ? const Color(0xFF00FF00)
              : (decode(colors.last) ?? const Color(0xFF00FF00)),
          title: '添加颜色'),
    );
    if (picked == null) return;
    final hex = encode(picked);
    if (colors.any((e) => e.toUpperCase() == hex)) return; // 去重:已存在不重复添加
    onChanged([...colors, hex]);
  }

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    return Wrap(spacing: 8, runSpacing: 8, children: [
      for (var i = 0; i < colors.length; i++)
        _Swatch(
          color: decode(colors[i]),
          label: colors[i],
          onTap: () => _edit(context, i),
          onDelete: () {
            final next = List<String>.from(colors)..removeAt(i);
            onChanged(next);
          },
        ),
      InkWell(
        borderRadius: BorderRadius.circular(AppConst.radiusCtrl),
        onTap: () => _add(context),
        child: Container(
          width: 30, height: 30,
          decoration: BoxDecoration(
            color: t.surface,
            borderRadius: BorderRadius.circular(AppConst.radiusCtrl),
            border: Border.all(color: t.border),
          ),
          child: Icon(Icons.add, size: 16, color: t.dim),
        ),
      ),
    ]);
  }
}

class _Swatch extends StatefulWidget {
  final Color? color;
  final String label;
  final VoidCallback onTap, onDelete;
  const _Swatch({required this.color, required this.label,
      required this.onTap, required this.onDelete});

  @override
  State<_Swatch> createState() => _SwatchState();
}

class _SwatchState extends State<_Swatch> {
  bool _hover = false;

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    return MouseRegion(
      onEnter: (_) => setState(() => _hover = true),
      onExit: (_) => setState(() => _hover = false),
      child: GestureDetector(
        onTap: widget.onTap,
        child: Tooltip(
          message: widget.label,
          child: Stack(clipBehavior: Clip.none, children: [
            Container(
              width: 30, height: 30,
              decoration: BoxDecoration(
                color: widget.color ?? t.border,
                borderRadius: BorderRadius.circular(AppConst.radiusCtrl),
                border: Border.all(
                    color: _hover ? t.primary : t.border,
                    width: _hover ? 1.6 : 1),
              ),
              child: widget.color == null
                  ? Icon(Icons.question_mark, size: 14, color: t.faint)
                  : null,
            ),
            if (_hover)
              Positioned(
                right: -6, top: -6,
                child: GestureDetector(
                  onTap: widget.onDelete,
                  child: Container(
                    width: 16, height: 16,
                    decoration: BoxDecoration(
                      color: t.danger,
                      shape: BoxShape.circle,
                      border: Border.all(color: t.surface, width: 1.5),
                    ),
                    child: const Icon(Icons.close, size: 10, color: Colors.white),
                  ),
                ),
              ),
          ]),
        ),
      ),
    );
  }
}

/// 自绘拾色器:HSV 饱和明度面板 + 色相滑条 + hex 输入 + 预览,不引第三方包。
class ColorPickerDialog extends StatefulWidget {
  final Color initial;
  final String title;
  const ColorPickerDialog({super.key, required this.initial, required this.title});

  @override
  State<ColorPickerDialog> createState() => _ColorPickerDialogState();
}

class _ColorPickerDialogState extends State<ColorPickerDialog> {
  late HSVColor _hsv;
  late final TextEditingController _hex;

  @override
  void initState() {
    super.initState();
    _hsv = HSVColor.fromColor(widget.initial);
    _hex = TextEditingController(text: ColorSwatchList.encode(widget.initial));
  }

  @override
  void dispose() {
    _hex.dispose();
    super.dispose();
  }

  void _setHSV(HSVColor v, {bool syncHex = true}) {
    setState(() {
      _hsv = v;
      if (syncHex) _hex.text = ColorSwatchList.encode(v.toColor());
    });
  }

  void _onHex(String s) {
    final c = ColorSwatchList.decode(s);
    if (c != null) _setHSV(HSVColor.fromColor(c), syncHex: false);
  }

  void _onSV(Offset p, Size size) {
    final sat = (p.dx / size.width).clamp(0.0, 1.0);
    final val = 1.0 - (p.dy / size.height).clamp(0.0, 1.0);
    _setHSV(_hsv.withSaturation(sat).withValue(val));
  }

  void _onHue(Offset p, Size size) {
    final h = (p.dx / size.width).clamp(0.0, 1.0) * 360;
    _setHSV(_hsv.withHue(h));
  }

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    final color = _hsv.toColor();
    return AlertDialog(
      backgroundColor: t.surface,
      title: Text(widget.title,
          style: TextStyle(fontSize: 15, fontWeight: FontWeight.w700, color: t.ink)),
      content: SizedBox(
        width: 320,
        child: Column(mainAxisSize: MainAxisSize.min, children: [
          LayoutBuilder(builder: (context, c) {
            return GestureDetector(
              onPanDown: (d) => _onSV(d.localPosition, c.biggest),
              onPanUpdate: (d) => _onSV(d.localPosition, c.biggest),
              child: ClipRRect(
                borderRadius: BorderRadius.circular(AppConst.radiusCtrl),
                child: SizedBox(
                  width: 320, height: 180,
                  child: CustomPaint(
                    painter: _SVPainter(_hsv),
                  ),
                ),
              ),
            );
          }),
          const SizedBox(height: 14),
          LayoutBuilder(builder: (context, c) {
            return GestureDetector(
              onPanDown: (d) => _onHue(d.localPosition, c.biggest),
              onPanUpdate: (d) => _onHue(d.localPosition, c.biggest),
              child: ClipRRect(
                borderRadius: BorderRadius.circular(7),
                child: SizedBox(
                  width: 320, height: 14,
                  child: CustomPaint(painter: _HuePainter(_hsv.hue)),
                ),
              ),
            );
          }),
          const SizedBox(height: 14),
          Row(children: [
            Container(
              width: 40, height: 28,
              decoration: BoxDecoration(
                color: color,
                borderRadius: BorderRadius.circular(AppConst.radiusCtrl),
                border: Border.all(color: t.border),
              ),
            ),
            const SizedBox(width: 10),
            Expanded(
              child: TextField(
                controller: _hex,
                style: TextStyle(fontSize: 13, color: t.ink,
                    fontFamily: AppConst.fontFamily),
                decoration: const InputDecoration(
                    hintText: '0xRRGGBB', isDense: true),
                onChanged: _onHex,
              ),
            ),
          ]),
        ]),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context),
          child: const Text('取消'),
        ),
        FilledButton(
          onPressed: () => Navigator.pop(context, color),
          child: const Text('确定'),
        ),
      ],
    );
  }
}

class _SVPainter extends CustomPainter {
  final HSVColor hsv;
  _SVPainter(this.hsv);

  @override
  void paint(Canvas canvas, Size size) {
    final rect = Offset.zero & size;
    // 底=当前色相纯色;横向白→透明(饱和度),纵向透明→黑(明度)
    canvas.drawRect(rect,
        Paint()..color = HSVColor.fromAHSV(1, hsv.hue, 1, 1).toColor());
    canvas.drawRect(
        rect,
        Paint()
          ..shader = const LinearGradient(
                  colors: [Colors.white, Color(0x00FFFFFF)])
              .createShader(rect));
    canvas.drawRect(
        rect,
        Paint()
          ..shader = const LinearGradient(
                  begin: Alignment.topCenter,
                  end: Alignment.bottomCenter,
                  colors: [Colors.transparent, Colors.black])
              .createShader(rect));
    // 当前取色点
    final dx = hsv.saturation * size.width;
    final dy = (1 - hsv.value) * size.height;
    canvas.drawCircle(
        Offset(dx, dy), 7,
        Paint()
          ..style = PaintingStyle.stroke
          ..strokeWidth = 2.4
          ..color = Colors.white);
    canvas.drawCircle(
        Offset(dx, dy), 8.4,
        Paint()
          ..style = PaintingStyle.stroke
          ..strokeWidth = 1.2
          ..color = Colors.black45);
  }

  @override
  bool shouldRepaint(_SVPainter old) => old.hsv != hsv;
}

class _HuePainter extends CustomPainter {
  final double hue;
  _HuePainter(this.hue);

  @override
  void paint(Canvas canvas, Size size) {
    final rect = Offset.zero & size;
    canvas.drawRect(
        rect,
        Paint()
          ..shader = LinearGradient(colors: [
            for (var i = 0; i <= 6; i++)
              HSVColor.fromAHSV(1, i * 60.0, 1, 1).toColor(),
          ]).createShader(rect));
    final dx = (hue / 360).clamp(0.0, 1.0) * size.width;
    canvas.drawCircle(
        Offset(dx, size.height / 2), size.height / 2 + 1.5,
        Paint()
          ..style = PaintingStyle.stroke
          ..strokeWidth = 2.4
          ..color = Colors.white);
    canvas.drawCircle(
        Offset(dx, size.height / 2), size.height / 2 + 2.7,
        Paint()
          ..style = PaintingStyle.stroke
          ..strokeWidth = 1.2
          ..color = Colors.black45);
  }

  @override
  bool shouldRepaint(_HuePainter old) => old.hue != hue;
}
