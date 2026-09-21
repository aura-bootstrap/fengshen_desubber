import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../models.dart';
import '../theme.dart';
import 'color_swatch.dart';
import 'font_file_field.dart';

/// 字段控件族:全部控件改动经 onChanged(key, value) 上报(字符串/数字/列表原样回写 JSON)。
/// 颜色一律经 context.tokens 取(明暗主题自适应)。

String _fmtNum(double v) {
  if (v == v.roundToDouble() && v.abs() >= 1) return v.toStringAsFixed(0);
  return v.toString();
}

/// 字段外壳:标签 + 控件 + 提示,供网格布局。
class FieldShell extends StatelessWidget {
  final FieldDef def;
  final Widget child;
  final Widget? trailing; // 右上角当前值
  const FieldShell({super.key, required this.def, required this.child, this.trailing});

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
      Row(children: [
        Expanded(
          // 单位紧跟标题文字,灰色括号括起来
          child: RichText(
            overflow: TextOverflow.ellipsis,
            text: TextSpan(children: [
              TextSpan(
                  text: def.label,
                  style: TextStyle(
                      fontSize: 13,
                      fontWeight: FontWeight.w600,
                      color: t.ink,
                      fontFamily: AppConst.fontFamily)),
              if (def.unit != null)
                TextSpan(
                    text: '（${def.unit}）',
                    style: TextStyle(
                        fontSize: 12,
                        color: t.faint,
                        fontFamily: AppConst.fontFamily)),
            ]),
          ),
        ),
        if (trailing != null) trailing!,
      ]),
      const SizedBox(height: 9),
      child,
      if (def.hint != null) ...[
        const SizedBox(height: 6),
        Text(def.hint!,
            style: TextStyle(fontSize: 12, color: t.faint)),
      ],
    ]);
  }
}

class ValueBadge extends StatelessWidget {
  final String text;
  const ValueBadge(this.text, {super.key});
  @override
  Widget build(BuildContext context) => Text(text,
      style: TextStyle(
          fontSize: 11.5,
          fontWeight: FontWeight.w500,
          color: context.tokens.faint));
}

// ---- 文本 / 数字 ----

class TextFieldWidget extends StatefulWidget {
  final FieldDef def;
  final Object? value;
  final ValueChanged<String> onChanged;
  final bool multiline;
  const TextFieldWidget({super.key, required this.def, required this.value,
      required this.onChanged, this.multiline = false});

  @override
  State<TextFieldWidget> createState() => _TextFieldWidgetState();
}

class _TextFieldWidgetState extends State<TextFieldWidget> {
  late final TextEditingController _c;

  String _asText(Object? v) =>
      v is List ? v.join(widget.multiline ? '\n' : ',') : (v?.toString() ?? '');

  @override
  void initState() {
    super.initState();
    _c = TextEditingController(text: _asText(widget.value));
  }

  @override
  void didUpdateWidget(TextFieldWidget old) {
    super.didUpdateWidget(old);
    // 外部改值(如"浏览"按钮选了新目录)时同步进输入框;
    // 用户自己输入时 onChanged 已把 state 改成同值,不会回冲
    final text = _asText(widget.value);
    if (_c.text != text) {
      _c.value = TextEditingValue(
        text: text,
        selection: TextSelection.collapsed(offset: text.length),
      );
    }
  }

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return TextField(
      controller: _c,
      maxLines: widget.multiline ? 7 : 1,
      style: TextStyle(fontSize: 13, color: context.tokens.ink),
      decoration: const InputDecoration(),
      onChanged: widget.onChanged,
    );
  }
}

// ---- 滑杆 + 数值联动 ----

class SliderNumberField extends StatefulWidget {
  final FieldDef def;
  final double value;
  final ValueChanged<double> onChanged;
  const SliderNumberField({super.key, required this.def, required this.value,
      required this.onChanged});

  @override
  State<SliderNumberField> createState() => _SliderNumberFieldState();
}

class _SliderNumberFieldState extends State<SliderNumberField> {
  late double _v;
  late final TextEditingController _c;
  late final FocusNode _focus;
  bool _dragging = false;

  // 编辑框极限保护:未显式给 hardMin/hardMax 时用滑杆行程
  double get _hardLo => widget.def.hardMin ?? widget.def.sliderMin;
  double get _hardHi => widget.def.hardMax ?? widget.def.sliderMax;

  @override
  void initState() {
    super.initState();
    _v = widget.value;
    _c = TextEditingController(text: _fmtNum(widget.value));
    _focus = FocusNode()..addListener(() {
      if (!_focus.hasFocus) _commit();
    });
  }

  @override
  void didUpdateWidget(SliderNumberField old) {
    super.didUpdateWidget(old);
    // 文本输入期间不回写,避免吃掉小数中间态("0."→"0")与光标
    if (!_dragging && !_focus.hasFocus && old.value != widget.value) {
      _v = widget.value;
      _c.text = _fmtNum(widget.value);
    }
  }

  @override
  void dispose() {
    _focus.dispose();
    _c.dispose();
    super.dispose();
  }

  /// 失焦/回车规范化:无法解析则还原为当前值;超极限取极限值回显。
  void _commit() {
    final parsed = double.tryParse(_c.text);
    if (parsed == null) {
      _c.text = _fmtNum(_v);
      return;
    }
    final v = parsed.clamp(_hardLo, _hardHi);
    final changed = v != widget.value;
    setState(() {
      _v = v;
      _c.text = _fmtNum(v);
    });
    if (changed) widget.onChanged(v);
  }

  @override
  Widget build(BuildContext context) {
    final d = widget.def;
    final t = context.tokens;
    return Row(children: [
      Expanded(
        child: Slider(
          // 滑块显示钳在滑杆行程内(超程顶到端点),输入保护用极限区间
          value: _v.clamp(d.sliderMin, d.sliderMax),
          min: d.sliderMin,
          max: d.sliderMax,
          onChanged: (v) {
            setState(() {
              _v = v;
              _dragging = true;
              _c.text = _fmtNum(v);
            });
            widget.onChanged(v);
          },
          onChangeEnd: (_) => _dragging = false,
        ),
      ),
      SizedBox(
        width: 92,
        child: TextField(
          controller: _c,
          focusNode: _focus,
          textAlign: TextAlign.center,
          style: TextStyle(
              fontSize: 13, fontWeight: FontWeight.w700, color: t.primaryInk),
          inputFormatters: [
            FilteringTextInputFormatter.allow(RegExp(r'[\d.\-]'))
          ],
          decoration: const InputDecoration(),
          onChanged: (s) {
            final v = double.tryParse(s);
            if (v != null) {
              final c = v.clamp(_hardLo, _hardHi);
              setState(() => _v = c);
              widget.onChanged(c);
            }
          },
          onSubmitted: (_) => _commit(),
        ),
      ),
    ]);
  }
}

// ---- 范围 min~max:双滑杆 + 两端数值输入联动 ----

class RangeField extends StatefulWidget {
  final FieldDef def;
  final Object? value; // {min, max}
  final void Function(double min, double max) onChanged;
  const RangeField({super.key, required this.def, required this.value,
      required this.onChanged});

  @override
  State<RangeField> createState() => _RangeFieldState();
}

class _RangeFieldState extends State<RangeField> {
  late double _lo, _hi, _sliderMax;
  late final TextEditingController _min, _max;
  late final FocusNode _minFocus, _maxFocus;
  bool _dragging = false;
  bool _remoteTakeoverDuringDrag = false;

  FieldDef get d => widget.def;

  // 编辑框极限保护:未显式给 hardMin/hardMax 时用滑杆行程;
  // expandableRange 未给 hardMax 则上限不限(保持可扩程)
  double get _hardLo => d.hardMin ?? d.sliderMin;
  double get _hardHi =>
      d.hardMax ?? (d.expandableRange ? double.infinity : d.sliderMax);

  @override
  void initState() {
    super.initState();
    final v = widget.value;
    final rawLo = v is Map ? asDouble(v['min']) : null;
    final rawHi = v is Map ? asDouble(v['max']) : null;
    _sliderMax = d.expandableRange
        ? [d.sliderMax, rawLo ?? d.sliderMin, rawHi ?? d.sliderMax]
            .reduce((a, b) => a > b ? a : b)
        : d.sliderMax;
    _lo = rawLo ?? d.sliderMin;
    _hi = rawHi ?? d.sliderMax;
    if (_lo > _hi) _hi = _lo;
    _min = TextEditingController(text: _fmtNum(_lo));
    _max = TextEditingController(text: _fmtNum(_hi));
    _minFocus = FocusNode()..addListener(() => _onFocus('min'));
    _maxFocus = FocusNode()..addListener(() => _onFocus('max'));
  }

  @override
  void didUpdateWidget(RangeField old) {
    super.didUpdateWidget(old);
    // 编辑中不回写;外部改值(回滚/粘贴新建)同步进控件
    if (_minFocus.hasFocus || _maxFocus.hasFocus) return;
    final v = widget.value;
    if (v is! Map) return;
    final rawLo = asDouble(v['min']);
    final rawHi = asDouble(v['max']);
    if (rawLo == null || rawHi == null || (rawLo == _lo && rawHi == _hi)) {
      return;
    }
    if (_dragging) {
      _remoteTakeoverDuringDrag = true;
      return;
    }
    setState(() {
      if (d.expandableRange) {
        final requested = rawLo > rawHi ? rawLo : rawHi;
        if (requested > _sliderMax) _sliderMax = requested;
      }
      _lo = rawLo;
      _hi = rawHi;
      if (_lo > _hi) _hi = _lo;
      _min.text = _fmtNum(_lo);
      _max.text = _fmtNum(_hi);
    });
  }

  @override
  void dispose() {
    _minFocus.dispose();
    _maxFocus.dispose();
    _min.dispose();
    _max.dispose();
    super.dispose();
  }

  double _normalize(double value) =>
      d.rangeInteger ? value.roundToDouble() : value;

  void _emit() => widget.onChanged(_lo, _hi);

  void _fromSlider(RangeValues v) {
    if (_remoteTakeoverDuringDrag) return;
    setState(() {
      _lo = _normalize(v.start);
      _hi = _normalize(v.end);
      _min.text = _fmtNum(_lo);
      _max.text = _fmtNum(_hi);
    });
    _emit();
  }

  void _onSliderStart(RangeValues _) {
    _dragging = true;
    _remoteTakeoverDuringDrag = false;
  }

  void _onSliderEnd(RangeValues _) {
    _dragging = false;
    if (!_remoteTakeoverDuringDrag) return;
    _remoteTakeoverDuringDrag = false;
    Future<void>.delayed(const Duration(milliseconds: 250), () {
      if (!mounted || _dragging) return;
      final v = widget.value;
      if (v is! Map) return;
      final rawLo = asDouble(v['min']);
      final rawHi = asDouble(v['max']);
      if (rawLo == null || rawHi == null) return;
      setState(() {
        if (d.expandableRange) {
          final requested = rawLo > rawHi ? rawLo : rawHi;
          if (requested > _sliderMax) _sliderMax = requested;
        }
        _lo = rawLo;
        _hi = rawHi;
        if (_lo > _hi) _hi = _lo;
        _min.text = _fmtNum(_lo);
        _max.text = _fmtNum(_hi);
      });
    });
  }

  void _fromText(String edge) {
    final parsedLo = double.tryParse(_min.text);
    final parsedHi = double.tryParse(_max.text);
    if (parsedLo == null && parsedHi == null) return;
    setState(() {
      final wantLo = (parsedLo ?? _lo).clamp(_hardLo, _hardHi);
      final wantHi = (parsedHi ?? _hi).clamp(_hardLo, _hardHi);
      if (d.expandableRange) {
        final requested = wantLo > wantHi ? wantLo : wantHi;
        if (requested > _sliderMax) _sliderMax = _normalize(requested);
      }
      _lo = _normalize(wantLo);
      _hi = _normalize(wantHi);
      // min>max 时以正在编辑的一侧为准推高/压低另一侧
      if (_lo > _hi) {
        if (edge == 'min') {
          _hi = _lo;
        } else {
          _lo = _hi;
        }
      }
      // 只重写未在输入的对侧框,焦点框保留原文(保住 "0." 中间态与光标)
      if (edge == 'min') {
        _max.text = _fmtNum(_hi);
      } else {
        _min.text = _fmtNum(_lo);
      }
    });
    _emit();
  }

  void _onFocus(String edge) {
    final focused = edge == 'min' ? _minFocus.hasFocus : _maxFocus.hasFocus;
    if (focused) return;
    _commit(edge);
  }

  /// 失焦/回车规范化本框:无法解析则还原;超极限取极限值,取整后回显。
  void _commit(String edge) {
    final c = edge == 'min' ? _min : _max;
    final cur = edge == 'min' ? _lo : _hi;
    final parsed = double.tryParse(c.text);
    if (parsed == null) {
      c.text = _fmtNum(cur);
      return;
    }
    final oldLo = _lo, oldHi = _hi;
    setState(() {
      final v = _normalize(parsed.clamp(_hardLo, _hardHi));
      if (edge == 'min') {
        _lo = v;
        if (_lo > _hi) _hi = _lo;
      } else {
        _hi = v;
        if (_hi < _lo) _lo = _hi;
      }
      _min.text = _fmtNum(_lo);
      _max.text = _fmtNum(_hi);
    });
    if (_lo != oldLo || _hi != oldHi) _emit();
  }

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    Widget box(TextEditingController c, FocusNode focus, String edge) =>
        SizedBox(
          width: 76,
          child: TextField(
            key: ValueKey('range-${d.key}-$edge'),
            controller: c,
            focusNode: focus,
            textAlign: TextAlign.center,
            style: TextStyle(
                fontSize: 13, fontWeight: FontWeight.w700, color: t.primaryInk),
            inputFormatters: [
              FilteringTextInputFormatter.allow(RegExp(r'[\d.\-]'))
            ],
            decoration: const InputDecoration(),
            onChanged: (_) => _fromText(edge),
            onSubmitted: (_) => _commit(edge),
          ),
        );
    return Row(children: [
      box(_min, _minFocus, 'min'),
      Expanded(
        child: RangeSlider(
          key: ValueKey('range-${d.key}'),
          // 滑块显示钳在滑杆行程内(超程顶到端点),输入保护用极限区间
          values: RangeValues(
            _lo.clamp(d.sliderMin, _sliderMax),
            _hi.clamp(d.sliderMin, _sliderMax),
          ),
          min: d.sliderMin,
          max: _sliderMax,
          onChangeStart: _onSliderStart,
          onChanged: _fromSlider,
          onChangeEnd: _onSliderEnd,
        ),
      ),
      box(_max, _maxFocus, 'max'),
    ]);
  }
}

// ---- chips 多选 ----

class ChipsField extends StatelessWidget {
  final FieldDef def;
  final List<String> selected;
  final ValueChanged<List<String>> onChanged;
  const ChipsField({super.key, required this.def, required this.selected,
      required this.onChanged});

  @override
  Widget build(BuildContext context) {
    return Wrap(spacing: 8, runSpacing: 8, children: [
      for (final opt in def.options)
        _Chip(
          label: opt,
          on: selected.contains(opt),
          onTap: () {
            final next = List<String>.from(selected);
            next.contains(opt) ? next.remove(opt) : next.add(opt);
            onChanged(next);
          },
        ),
    ]);
  }
}

class _Chip extends StatelessWidget {
  final String label;
  final bool on;
  final VoidCallback onTap;
  const _Chip({required this.label, required this.on, required this.onTap});

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    final isDark = Theme.of(context).brightness == Brightness.dark;
    return InkWell(
      borderRadius: BorderRadius.circular(AppConst.radiusCtrl),
      onTap: onTap,
      child: AnimatedContainer(
        duration: const Duration(milliseconds: 120),
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 7),
        decoration: BoxDecoration(
          color: on ? t.primarySoft : t.surface,
          borderRadius: BorderRadius.circular(AppConst.radiusCtrl),
          border: Border.all(
              color: on
                  ? (isDark ? t.primary : const Color(0xFFC3CFFB))
                  : t.border),
        ),
        child: Text(label,
            style: TextStyle(
                fontSize: 12.5,
                fontWeight: FontWeight.w600,
                color: on ? t.primaryInk : t.dim)),
      ),
    );
  }
}

// ---- 下拉 ----

/// 与主界面风格统一的自绘下拉:触发器复用输入框外壳(labelText 与 TextField 同款
/// 浮动 label:showValue=false 时占位显示在框内,打开菜单或选中子项后浮到框缘),
/// 菜单落在触发器正下方(间距 6,不遮挡触发器),白底 radius 12 + 1px 浅描边 + 投影,
/// 内缩 primarySoft 圆角 8 选中块 + primaryInk 加粗 + 主色 check(对齐 dd_a/dd_b mockup)。
class StyledDropdown extends StatefulWidget {
  final String? value;
  final List<String> options;
  final ValueChanged<String?> onChanged;
  final InputDecoration? decoration;
  final String Function(String)? labelOf;
  // false=未选占位态:框内显示 label,值隐藏;true=显示值,label 浮在框缘
  final bool showValue;
  const StyledDropdown(
      {super.key,
      required this.value,
      required this.options,
      required this.onChanged,
      this.decoration,
      this.labelOf,
      this.showValue = true});

  @override
  State<StyledDropdown> createState() => _StyledDropdownState();
}

class _StyledDropdownState extends State<StyledDropdown> {
  final LayerLink _link = LayerLink();
  final GlobalKey _triggerKey = GlobalKey();
  OverlayEntry? _entry;

  String _label(String o) => widget.labelOf == null ? o : widget.labelOf!(o);

  bool get _open => _entry != null;

  void _toggle() => _open ? _close() : _show();

  void _show() {
    final box = _triggerKey.currentContext!.findRenderObject() as RenderBox;
    final t = context.tokens;
    final dark = Theme.of(context).brightness == Brightness.dark;
    _entry = OverlayEntry(
      builder: (_) => Stack(children: [
        // 透明屏障:点外部收菜单
        Positioned.fill(
          child: GestureDetector(
            onTap: _close,
            behavior: HitTestBehavior.translucent,
            child: const SizedBox.expand(),
          ),
        ),
        CompositedTransformFollower(
          link: _link,
          showWhenUnlinked: false,
          offset: Offset(0, box.size.height + 6),
          child: Align(
            alignment: Alignment.topLeft,
            // 底色必须画在 Material 上:InkWell 的 hover 墨水绘在最近 Material 层,
            // 若底色由子级 Container 画会盖住墨水,悬停永远不可见
            child: Container(
              width: box.size.width,
              constraints: const BoxConstraints(maxHeight: 360),
              decoration: BoxDecoration(
                borderRadius: BorderRadius.circular(12),
                border: Border.all(
                    color: dark ? t.border : const Color(0xFFE3E7EE)),
                boxShadow: [
                  BoxShadow(
                    color: dark
                        ? Colors.black54
                        : const Color.fromRGBO(20, 30, 60, .16),
                    blurRadius: 24,
                    offset: const Offset(0, 6),
                  ),
                ],
              ),
              child: Material(
                color: t.surface,
                borderRadius: BorderRadius.circular(12),
                clipBehavior: Clip.antiAlias,
                child: Padding(
                  padding: const EdgeInsets.all(6),
                  child: SingleChildScrollView(
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [for (final o in widget.options) _item(t, o)],
                    ),
                  ),
                ),
              ),
            ),
          ),
        ),
      ]),
    );
    Overlay.of(context).insert(_entry!);
    setState(() {}); // _open 转真:聚焦描边与浮标(InputDecorator 自带过渡)
  }

  Widget _item(AppTokens t, String o) {
    final sel = o == widget.value;
    // 上下各 2px 空隙:相邻选中/悬停色块不互贴
    return SizedBox(
      height: 38,
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 2),
        child: InkResponse(
          borderRadius: BorderRadius.circular(8),
          highlightShape: BoxShape.rectangle,
          hoverColor: t.primarySoft,
          splashColor: Colors.transparent,
          highlightColor: Colors.transparent,
          onTap: () {
            _close();
            widget.onChanged(o);
          },
          child: Container(
            padding: const EdgeInsets.symmetric(horizontal: 10),
            decoration: sel
                ? BoxDecoration(
                    color: t.primarySoft, borderRadius: BorderRadius.circular(8))
                : null,
            child: Row(children: [
              Text(_label(o),
                  style: TextStyle(
                    fontSize: 13,
                    color: sel ? t.primaryInk : t.ink,
                    fontWeight: sel ? FontWeight.w600 : null,
                    fontFamily: AppConst.fontFamily,
                  )),
              const Spacer(),
              if (sel) Icon(Icons.check, size: 15, color: t.primary),
            ]),
          ),
        ),
      ),
    );
  }

  void _close() {
    if (_entry == null) return;
    _entry!.remove();
    _entry = null;
    setState(() {}); // 触发器聚焦描边与浮标随菜单开合还原
  }

  @override
  void dispose() {
    _entry?.remove();
    _entry = null;
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    final d = widget.decoration ?? const InputDecoration();
    // label 交给 InputDecorator 原生浮动(与「批次」TextField 同机制:字号/居中/
    // 缺口/高度全自动一致)。showValue=false 视为空(占位 label 在框内);打开菜单
    // (isFocused)或已选子项(showValue=true)时浮到框缘
    final showContent = widget.showValue || _open;
    final current = widget.value;
    return CompositedTransformTarget(
      link: _link,
      child: MouseRegion(
        cursor: SystemMouseCursors.click,
        child: GestureDetector(
          key: _triggerKey,
          behavior: HitTestBehavior.opaque,
          onTap: _toggle,
          child: InputDecorator(
            decoration: d,
            isEmpty: !widget.showValue,
            isFocused: _open,
            // 定高:值显隐切换时盒子不缩;字号与筛选输入框正文一致(13)
            child: SizedBox(
              height: 19,
              // 箭头常驻(含占位态);只值文本按态显隐
              child: Row(children: [
                Expanded(
                  child: !showContent
                      ? const SizedBox.shrink()
                      : Text(
                          current == null ? '' : _label(current),
                          overflow: TextOverflow.ellipsis,
                          // 显式钉字体族:textStyle 缺 fontFamily 会在合并链上丢掉雅黑
                          style: TextStyle(
                              fontSize: 13,
                              color: t.ink,
                              fontFamily: AppConst.fontFamily),
                        ),
                ),
                Icon(Icons.expand_more_rounded, size: 18, color: t.dim),
              ]),
            ),
          ),
        ),
      ),
    );
  }
}

class DropdownField extends StatelessWidget {
  final FieldDef def;
  final String value;
  final ValueChanged<String> onChanged;
  const DropdownField({super.key, required this.def, required this.value,
      required this.onChanged});

  @override
  Widget build(BuildContext context) {
    return StyledDropdown(
      value: def.options.contains(value) ? value : null,
      options: def.options,
      labelOf: def.optionLabels.isEmpty
          ? null
          : (o) => def.optionLabels[o] ?? o,
      onChanged: (v) {
        if (v != null) onChanged(v);
      },
    );
  }
}

// ---- 目录选择(经文本输入 + 浏览按钮,浏览由父级处理) ----

class PathField extends StatelessWidget {
  final FieldDef def;
  final Object? value;
  final ValueChanged<String> onChanged;
  final VoidCallback onBrowse;
  const PathField({super.key, required this.def, required this.value,
      required this.onBrowse, required this.onChanged});

  @override
  Widget build(BuildContext context) {
    return Row(children: [
      Expanded(
        child: TextFieldWidget(def: def, value: value, onChanged: onChanged),
      ),
      const SizedBox(width: 8),
      OutlinedButton.icon(
        onPressed: onBrowse,
        icon: const Icon(Icons.folder_open, size: 16),
        label: const Text('浏览'),
      ),
    ]);
  }
}

// ---- 开关(布尔) ----

class BoolField extends StatelessWidget {
  final FieldDef def;
  final bool value;
  final ValueChanged<bool> onChanged;
  const BoolField({super.key, required this.def, required this.value,
      required this.onChanged});

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    return Row(children: [
      Switch(value: value, onChanged: onChanged),
      const SizedBox(width: 4),
      Text(value ? '已开启' : '已关闭',
          style: TextStyle(
              fontSize: 12.5, fontWeight: FontWeight.w600, color: t.dim)),
    ]);
  }
}

/// 按字段类型分发控件。
Widget buildFieldControl({
  required FieldDef def,
  required Object? value,
  required void Function(String key, Object? value) onChanged,
  VoidCallback? onBrowse,
  Future<List<String>> Function()? fontsLoader,
}) {
  switch (def.kind) {
    case FieldKind.bool:
      return FieldShell(
        def: def,
        child: BoolField(
          def: def,
          value: value == true || value?.toString() == 'true',
          onChanged: (v) => onChanged(def.key, v),
        ),
      );
    case FieldKind.slider:
      return FieldShell(
        def: def,
        child: SliderNumberField(
          def: def,
          value: asDouble(value) ?? 0,
          onChanged: (v) => onChanged(def.key, v),
        ),
      );
    case FieldKind.range:
      return FieldShell(
        def: def,
        child: RangeField(
          def: def,
          value: value,
          onChanged: (lo, hi) =>
              onChanged(def.key, {'min': lo, 'max': hi}),
        ),
      );
    case FieldKind.chips:
      final sel = value is List
          ? value.map((e) => e.toString()).toList()
          : <String>[];
      return FieldShell(
        def: def,
        child: ChipsField(
          def: def,
          selected: sel,
          onChanged: (next) {
            // crf_choices 是 []int,其余 choices 是 []string
            if (def.key.contains('crf_choices')) {
              onChanged(def.key,
                  next.map((e) => int.tryParse(e) ?? 0).toList());
            } else {
              onChanged(def.key, next);
            }
          },
        ),
      );
    case FieldKind.dropdown:
      return FieldShell(
        def: def,
        child: DropdownField(
          def: def,
          value: value?.toString() ?? '',
          onChanged: (v) => onChanged(def.key, v),
        ),
      );
    case FieldKind.multiline:
      return FieldShell(
        def: def,
        child: TextFieldWidget(
            def: def, value: value, multiline: true,
            onChanged: (s) => onChanged(def.key,
                s.split('\n').where((l) => l.trim().isNotEmpty).toList())),
      );
    case FieldKind.colorlist:
      final list = value is List
          ? value.map((e) => e.toString()).toList()
          : <String>[];
      return FieldShell(
        def: def,
        child: ColorSwatchList(
          def: def,
          colors: list,
          onChanged: (next) => onChanged(def.key, next),
        ),
      );
    case FieldKind.fontfile:
      return FieldShell(
        def: def,
        child: FontFileDropdown(
          def: def,
          value: value?.toString() ?? '',
          fontsLoader: fontsLoader ?? () async => const [],
          onChanged: (v) => onChanged(def.key, v),
        ),
      );
    case FieldKind.videofile:
    case FieldKind.path:
      return FieldShell(
        def: def,
        child: PathField(
          def: def,
          value: value,
          onChanged: (s) => onChanged(def.key, s),
          onBrowse: onBrowse ?? () {},
        ),
      );
    case FieldKind.integer:
    case FieldKind.decimal:
    case FieldKind.text:
      return FieldShell(
        def: def,
        child: TextFieldWidget(
          def: def,
          value: value,
          onChanged: (s) {
            if (def.kind == FieldKind.integer) {
              onChanged(def.key, int.tryParse(s) ?? s);
            } else if (def.kind == FieldKind.decimal) {
              onChanged(def.key, double.tryParse(s) ?? s);
            } else {
              onChanged(def.key, s);
            }
          },
        ),
      );
  }
}
