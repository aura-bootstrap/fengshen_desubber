import 'package:file_selector/file_selector.dart';
import 'package:flutter/material.dart';

import '../app_state.dart';
import '../theme.dart';

/// 新建任务向导:选视频 + 参数快照 + 是否立即运行。
Future<void> showNewTaskDialog(BuildContext context, AppState state) {
  return showDialog(
    context: context,
    builder: (ctx) => NewTaskDialog(state: state),
  );
}

class NewTaskDialog extends StatefulWidget {
  final AppState state;
  const NewTaskDialog({super.key, required this.state});

  @override
  State<NewTaskDialog> createState() => _NewTaskDialogState();
}

class _NewTaskDialogState extends State<NewTaskDialog> {
  String? srcPath;
  final nameCtrl = TextEditingController();
  final outCtrl = TextEditingController();
  final crfCtrl = TextEditingController(text: '15');
  bool propainter = true;
  bool grain = true;
  bool ocr = true;
  // 修复引擎:DiffuEraser 扩散模型(默认,画质最好,最慢)或 ProPainter。
  // 引擎路由只留 painter 档:自动路由会把慢动事件分进 motion 档留残影,
  // 质量优先于耗时,固定强制(force_engine 对齐 slice1-pp-grain tag 参数)
  bool diffueraser = true;
  static const forceEngine = 'propainter';
  bool runNow = true;
  bool busy = false;

  @override
  void dispose() {
    nameCtrl.dispose();
    outCtrl.dispose();
    crfCtrl.dispose();
    super.dispose();
  }

  Future<void> pickVideo() async {
    const group = XTypeGroup(label: '视频', extensions: ['mp4', 'mkv', 'mov', 'avi']);
    final file = await openFile(acceptedTypeGroups: [group]);
    if (file == null) return;
    setState(() {
      srcPath = file.path;
      final base = file.name.replaceAll(RegExp(r'\.[^.]+$'), '');
      if (nameCtrl.text.isEmpty) nameCtrl.text = base;
      if (outCtrl.text.isEmpty) outCtrl.text = '${base}_fixed.mp4';
    });
  }

  Future<void> submit() async {
    final src = srcPath;
    if (src == null) return;
    setState(() => busy = true);
    try {
      await widget.state.client.createTask(
        name: nameCtrl.text.trim(),
        srcPath: src,
        outName: outCtrl.text.trim(),
        params: {
          'propainter': propainter,
          'grain': grain,
          'ocr': ocr,
          'crf': int.tryParse(crfCtrl.text) ?? 0,
          'force_engine': forceEngine,
          'diffueraser': diffueraser,
        },
        runNow: runNow,
      );
      await widget.state.refresh();
      if (mounted) Navigator.of(context).pop();
    } catch (e) {
      if (mounted) {
        setState(() => busy = false);
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('$e')));
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    return AlertDialog(
      title: const Text('新建去字幕任务'),
      content: SizedBox(
        width: 520,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    srcPath ?? '未选择视频文件',
                    style: TextStyle(
                        fontSize: 12,
                        color: srcPath == null ? t.faint : t.ink,
                        fontFamily: AppConst.fontMono),
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                const SizedBox(width: 8),
                OutlinedButton.icon(
                  onPressed: busy ? null : pickVideo,
                  icon: const Icon(Icons.video_file, size: 16),
                  label: const Text('选择视频'),
                ),
              ],
            ),
            const SizedBox(height: 12),
            TextField(
              controller: nameCtrl,
              decoration: const InputDecoration(labelText: '任务名'),
            ),
            const SizedBox(height: 10),
            TextField(
              controller: outCtrl,
              decoration: const InputDecoration(labelText: '输出文件名'),
            ),
            const SizedBox(height: 12),
            Wrap(
              spacing: 16,
              children: [
                _check('ProPainter 修复', propainter, (v) => setState(() => propainter = v)),
                _check('纹理匹配(grain)', grain, (v) => setState(() => grain = v)),
                _check('PaddleOCR 融合', ocr, (v) => setState(() => ocr = v)),
              ],
            ),
            const SizedBox(height: 10),
            Row(
              children: [
                SizedBox(
                  width: 120,
                  child: TextField(
                    controller: crfCtrl,
                    decoration: const InputDecoration(labelText: 'CRF'),
                    keyboardType: TextInputType.number,
                  ),
                ),
                const SizedBox(width: 16),
                Text('引擎:', style: TextStyle(fontSize: 13, color: t.dim)),
                const SizedBox(width: 8),
                DropdownButton<bool>(
                  value: diffueraser,
                  isDense: true,
                  items: const [
                    DropdownMenuItem(value: true, child: Text('DiffuEraser 扩散(最佳画质,慢)', style: TextStyle(fontSize: 13))),
                    DropdownMenuItem(value: false, child: Text('ProPainter(快)', style: TextStyle(fontSize: 13))),
                  ],
                  onChanged: busy ? null : (v) => setState(() => diffueraser = v ?? true),
                ),
              ],
            ),
            const SizedBox(height: 8),
            _check('创建后立即运行', runNow, (v) => setState(() => runNow = v)),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: busy ? null : () => Navigator.of(context).pop(),
          child: const Text('取消'),
        ),
        FilledButton.icon(
          onPressed: (busy || srcPath == null) ? null : submit,
          icon: const Icon(Icons.rocket_launch, size: 16),
          label: Text(busy ? '创建中…' : '创建任务'),
        ),
      ],
    );
  }

  Widget _check(String label, bool value, ValueChanged<bool> onChanged) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Checkbox(value: value, onChanged: (v) => onChanged(v ?? false)),
        Text(label, style: const TextStyle(fontSize: 13)),
      ],
    );
  }
}
