import 'package:flutter/material.dart';
import 'package:file_selector/file_selector.dart';

import '../app_state.dart';
import '../models.dart';
import '../responsive.dart';
import '../theme.dart';
import '../widgets/param_field.dart';

/// 通用配置页:页头(标题/副标题)+ 分组卡片 + 响应式字段网格。
/// 无保存/重置按钮——任一改动经 AppState 防抖 1s 自动落盘(FR-6/FR-8,
/// 自动保存态见底部状态栏)。「项目」相关字段不在此——由任务向导按任务录入。
class ConfigPageView extends StatelessWidget {
  final AppState state;
  final ConfigPage page;
  const ConfigPageView({super.key, required this.state, required this.page});

  @override
  Widget build(BuildContext context) {
    final p = page;
    final st = state;
    final t = context.tokens;
    // 开发版:最终七域配置全部可调,无服务器托管只读页。
    return CenteredContent(
      child: ListView(
        padding: const EdgeInsets.fromLTRB(28, 24, 28, 24),
        children: [
          Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Expanded(
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Text(p.title,
                    style: TextStyle(
                        fontSize: 21, fontWeight: FontWeight.w700, color: t.ink)),
                const SizedBox(height: 4),
                Text(p.subtitle,
                    style: TextStyle(fontSize: 12.5, color: t.dim)),
              ]),
            ),
          ]),
          const SizedBox(height: 18),
          for (final g in p.groups) ...[
            _GroupCard(group: g, state: st),
            const SizedBox(height: 16),
          ],
        ],
      ),
    );
  }
}

class _GroupCard extends StatelessWidget {
  final FieldGroup group;
  final AppState state;
  const _GroupCard({required this.group, required this.state});

  Future<void> _browse(BuildContext context, FieldDef def) async {
    try {
      final String? path;
      if (def.kind == FieldKind.videofile) {
        final file = await openFile(acceptedTypeGroups: const [
          XTypeGroup(label: '视频', extensions: ['mp4', 'mov', 'mkv', 'avi', 'm4v', 'webm', 'ts', 'm2ts']),
        ]);
        path = file?.path;
      } else {
        path = await getDirectoryPath(confirmButtonText: '选择模板文件夹');
      }
      if (!context.mounted || path == null) return;
      state.editConfig(def.key, path);
    } catch (error) {
      if (!context.mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('无法打开选择窗口：$error')),
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    return Card(
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(20, 15, 20, 13),
          child: Row(children: [
            Text(group.title,
                style: TextStyle(
                    fontSize: 14, fontWeight: FontWeight.w700, color: t.ink)),
            const SizedBox(width: 10),
            // Flexible + ellipsis:窄窗口下卡头说明文字不再顶出右侧(溢出修复)
            Flexible(
              child: Text(group.caption,
                  style: TextStyle(fontSize: 12, color: t.faint),
                  overflow: TextOverflow.ellipsis),
            ),
          ]),
        ),
        const Divider(indent: 0),
        Padding(
          padding: const EdgeInsets.fromLTRB(20, 18, 20, 20),
          child: LayoutBuilder(builder: (context, c) {
            final cols = Responsive.fieldColumns(c.maxWidth);
            final gap = 36.0;
            final w = (c.maxWidth - gap * (cols - 1)) / cols;
            return Wrap(
              spacing: gap,
              runSpacing: 20,
              children: [
                for (final def in group.fields)
                  Builder(builder: (context) {
                    final disabled = fieldDisabled(state.config, def.key);
                    final ctl = buildFieldControl(
                      def: def,
                      fontsLoader: state.loadFonts,
                      onBrowse: () => _browse(context, def),
                      value: getPath(state.config, def.key),
                      onChanged: (key, value) {
                        if (def.kind == FieldKind.range && value is Map) {
                          final lo = asDouble(value['min']);
                          final hi = asDouble(value['max']);
                          if (lo == null || hi == null) return;
                          state.editConfig(key, {
                            'min': def.rangeInteger ? lo.round() : lo,
                            'max': def.rangeInteger ? hi.round() : hi,
                          });
                        } else {
                          state.editConfig(key, value);
                        }
                      },
                    );
                    final pathPrompt = switch (def.key) {
                      'clip.bookends.intro_path' => '请选择片头视频，开始生成前需补齐。',
                      'clip.bookends.outro_path' => '请选择片尾视频，开始生成前需补齐。',
                      _ => null,
                    };
                    final needsPath = !disabled && pathPrompt != null &&
                        (getPath(state.config, def.key)?.toString().trim().isEmpty ?? true);
                    return SizedBox(
                      width: w,
                      child: disabled
                          ? IgnorePointer(
                              child: Opacity(opacity: 0.45, child: ctl))
                          : Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                ctl,
                                if (needsPath) ...[
                                  const SizedBox(height: 6),
                                  Text(pathPrompt,
                                      style: TextStyle(fontSize: 12, color: t.warn)),
                                ],
                              ],
                            ),
                    );
                  }),
              ],
            );
          }),
        ),
      ]),
    );
  }
}
