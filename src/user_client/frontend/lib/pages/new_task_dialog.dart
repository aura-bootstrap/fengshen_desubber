import 'package:file_selector/file_selector.dart';
import 'package:flutter/material.dart';

import '../app_state.dart';
import '../theme.dart';
import '../widgets/cardkey_activate_dialog.dart';
import '../widgets/styled_dropdown.dart';

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
  // 修复引擎三选一:diffueraser(DiffuEraser 扩散,本地,画质最好,最慢)/
  // propainter(ProPainter,本地,快)/ online(在线去字幕,云端,按分钟扣点)。
  // 本地引擎路由只留 painter 档:自动路由会把慢动事件分进 motion 档留残影,
  // 质量优先于耗时,固定强制(force_engine 对齐 slice1-pp-grain tag 参数)
  String engine = 'diffueraser';
  static const forceEngine = 'propainter';
  // SAM2 像素级掩码 + GFPGAN 人脸先验:已验证(v14 管线),质量优先默认开。
  bool sam2 = true;
  bool faceRestore = true;
  static const engineLabels = {
    'diffueraser': 'DiffuEraser 扩散(本地·最佳画质,慢)',
    'propainter': 'ProPainter(本地·快)',
    'online': '在线去字幕(云端·按分钟扣点)',
  };
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
          'sam2': sam2,
          'face_restore': faceRestore,
          // 向后兼容:本地两档照旧按选择写 diffueraser;选在线引擎时
          // 额外写 online: true,diffueraser 落为 false。
          'diffueraser': engine == 'diffueraser',
          if (engine == 'online') 'online': true,
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
                // 照抄 slicer 的自绘 StyledDropdown(查询旁状态筛选同款):
                // 触发器复用 TextField 外壳,label 浮在框缘。
                Expanded(
                  child: StyledDropdown(
                    value: engine,
                    options: engineLabels.keys.toList(),
                    labelOf: (v) => engineLabels[v] ?? v,
                    decoration: const InputDecoration(labelText: '引擎'),
                    onChanged: (v) {
                      if (busy || v == null) return;
                      setState(() => engine = v);
                      // 切到在线引擎时顺带拉一次卡密状态。
                      if (v == 'online') widget.state.refreshCardKey();
                    },
                  ),
                ),
              ],
            ),
            // 仅在线引擎展示卡密状态区(激活/余额/换卡/解绑)。
            if (engine == 'online') ...[
              const SizedBox(height: 10),
              _cardKeySection(t),
            ],
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
          // 在线引擎未激活或余额为 0 时禁用创建(卡密区有对应提示)。
          onPressed: (busy || srcPath == null || _onlineBlocked) ? null : submit,
          icon: const Icon(Icons.rocket_launch, size: 16),
          label: Text(busy ? '创建中…' : '创建任务'),
        ),
      ],
    );
  }

  /// 在线引擎下是否禁止创建:状态未取到/未激活/余额为 0。
  bool get _onlineBlocked {
    if (engine != 'online') return false;
    final ck = widget.state.cardKey;
    return ck == null || !ck.activated || ck.credits <= 0;
  }

  /// 卡密状态区(仅在线引擎展示),跟随 AppState 通知刷新。
  Widget _cardKeySection(AppTokens t) {
    return AnimatedBuilder(
      animation: widget.state,
      builder: (context, _) {
        final st = widget.state;
        Widget child;
        if (st.cardKeyLoading && st.cardKey == null) {
          child = Row(children: [
            const SizedBox(
                width: 14,
                height: 14,
                child: CircularProgressIndicator(strokeWidth: 2)),
            const SizedBox(width: 8),
            Text('正在查询卡密状态…',
                style: TextStyle(fontSize: 12.5, color: t.dim)),
          ]);
        } else if (st.cardKey == null) {
          // 状态查询失败(引擎未就绪或版本过旧):给重试入口。
          child = Row(children: [
            Icon(Icons.error_outline, size: 15, color: t.danger),
            const SizedBox(width: 6),
            Expanded(
              child: Text('无法获取卡密状态:${st.cardKeyError ?? '未知错误'}',
                  style: TextStyle(fontSize: 12.5, color: t.danger),
                  overflow: TextOverflow.ellipsis),
            ),
            TextButton(
                onPressed: st.refreshCardKey, child: const Text('重试')),
          ]);
        } else if (!st.cardKey!.activated) {
          child = Row(children: [
            Icon(Icons.key_off, size: 15, color: t.warn),
            const SizedBox(width: 6),
            Text('未激活卡密', style: TextStyle(fontSize: 12.5, color: t.dim)),
            const Spacer(),
            FilledButton.icon(
              onPressed: _activateCard,
              icon: const Icon(Icons.key, size: 15),
              label: const Text('激活'),
            ),
          ]);
        } else {
          final ck = st.cardKey!;
          child = Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(children: [
                Icon(Icons.key, size: 15, color: t.success),
                const SizedBox(width: 6),
                Expanded(
                  child: Text(
                    '卡 ${ck.masked} · 余额 ${ck.credits} 点'
                    '${ck.degraded || ck.stale ? '(状态为缓存,以服务端为准)' : ''}',
                    style: TextStyle(fontSize: 12.5, color: t.ink),
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                TextButton(onPressed: _activateCard, child: const Text('换卡')),
                TextButton(
                  onPressed: _confirmDeactivate,
                  child: Text('解绑', style: TextStyle(color: t.danger)),
                ),
              ]),
              if (ck.credits <= 0)
                Padding(
                  padding: const EdgeInsets.only(top: 6),
                  child: Text('余额不足,请充值后再创建在线任务',
                      style: TextStyle(fontSize: 12.5, color: t.danger)),
                ),
            ],
          );
        }
        return Container(
          width: double.infinity,
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
          decoration: BoxDecoration(
            color: t.primarySoft.withValues(alpha: 0.35),
            borderRadius: BorderRadius.circular(8),
            border: Border.all(color: t.border),
          ),
          child: child,
        );
      },
    );
  }

  /// 弹激活对话框(激活/换卡共用),成功后状态由 AppState 通知刷新。
  Future<void> _activateCard() async {
    await showCardKeyActivateDialog(context, widget.state);
  }

  /// 解绑卡密,二次确认。
  Future<void> _confirmDeactivate() async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('解绑卡密'),
        content: const Text('解绑后本机将不能再使用在线去字幕,确定解绑吗?'),
        actions: [
          TextButton(
              onPressed: () => Navigator.of(ctx).pop(false),
              child: const Text('取消')),
          FilledButton(
              onPressed: () => Navigator.of(ctx).pop(true),
              child: const Text('解绑')),
        ],
      ),
    );
    if (ok != true) return;
    try {
      await widget.state.deactivateCardKey();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context)
            .showSnackBar(SnackBar(content: Text('$e')));
      }
    }
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
