import 'package:flutter/material.dart';

import '../models.dart';
import 'param_field.dart';

/// 字体文件下拉:选项 = 「默认(全局字体)」+ 引擎 fonts/ 目录扫描结果。
/// 扫描由后端 GET /api/fonts 提供(loader 注入,admin 等无本地引擎端不传则退化为文本框)。
/// 存量值不在扫描结果里(字体被删)时保留显示并标"缺失",交由引擎渲染期回退兜底。
class FontFileDropdown extends StatefulWidget {
  final FieldDef def;
  final String value; // '' = 默认(全局字体)
  final ValueChanged<String> onChanged;
  final Future<List<String>> Function() fontsLoader;
  const FontFileDropdown({super.key, required this.def, required this.value,
      required this.onChanged, required this.fontsLoader});

  @override
  State<FontFileDropdown> createState() => _FontFileDropdownState();
}

class _FontFileDropdownState extends State<FontFileDropdown> {
  List<String>? _fonts;

  @override
  void initState() {
    super.initState();
    widget.fontsLoader().then((f) {
      if (mounted) setState(() => _fonts = f);
    }).catchError((_) {
      if (mounted) setState(() => _fonts = const []);
    });
  }

  @override
  Widget build(BuildContext context) {
    const defaultKey = '';
    final fonts = _fonts;
    if (fonts == null) {
      return const SizedBox(
          height: 48,
          child: Center(
              child: SizedBox(
                  width: 16, height: 16,
                  child: CircularProgressIndicator(strokeWidth: 2))));
    }
    final options = [defaultKey, ...fonts];
    final missing = widget.value.isNotEmpty && !fonts.contains(widget.value);
    return StyledDropdown(
      value: options.contains(widget.value) ? widget.value : null,
      options: options,
      labelOf: (o) {
        if (o == defaultKey) return '默认(全局字体)';
        return o;
      },
      decoration: InputDecoration(
        helperText: missing ? '当前值 ${widget.value} 不在 fonts/ 目录,渲染时将回退全局字体' : null,
        helperMaxLines: 2,
      ),
      onChanged: (v) {
        if (v != null) widget.onChanged(v);
      },
    );
  }
}
