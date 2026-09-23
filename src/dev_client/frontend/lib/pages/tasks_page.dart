import 'package:file_selector/file_selector.dart';
import 'package:flutter/material.dart';

import '../app_state.dart';
import '../models.dart';
import '../responsive.dart';
import '../theme.dart';
import '../widgets/cloud_login_dialog.dart';
import '../widgets/top_toast.dart';

/// 任务页:任务卡片列表 + 四步新建任务向导。
class TasksPage extends StatelessWidget {
  final AppState state;
  const TasksPage({super.key, required this.state});

  Future<void> _newTask(BuildContext context) async {
    final created = await showDialog<TaskInfo>(
      context: context,
      builder: (_) => NewTaskDialog(state: state),
    );
    if (created != null && context.mounted) {
      TopToast.show(context, '任务 #${created.id}「${created.name}」已创建');
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    final tasks = state.taskList;
    return CenteredContent(
      child: ListView(
        padding: const EdgeInsets.fromLTRB(28, 24, 28, 24),
        children: [
          Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Expanded(
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Text('任务',
                    style: TextStyle(
                        fontSize: 21, fontWeight: FontWeight.w700, color: t.ink)),
                const SizedBox(height: 4),
                Text('共 ${state.taskTotal} 个任务;同一时刻只跑一个,忙时自动排队',
                    style: TextStyle(fontSize: 12.5, color: t.dim)),
              ]),
            ),
            FilledButton.icon(
              onPressed: () => _newTask(context),
              icon: const Icon(Icons.add, size: 16),
              label: const Text('新建任务'),
            ),
          ]),
          const SizedBox(height: 18),
          if (tasks.isEmpty)
            Card(
              child: Padding(
                padding: const EdgeInsets.all(36),
                child: Center(
                  child: Text('还没有任务,点右上角「新建任务」选一个视频开始',
                      style: TextStyle(fontSize: 13, color: t.dim)),
                ),
              ),
            ),
          for (final tk in tasks) ...[
            _TaskCard(task: tk, state: state),
            const SizedBox(height: 10),
          ],
        ],
      ),
    );
  }
}

class _TaskCard extends StatelessWidget {
  final TaskInfo task;
  final AppState state;
  const _TaskCard({required this.task, required this.state});

  Future<void> _run(BuildContext context) async {
    try {
      final status = await state.runTask(task.id);
      if (!context.mounted) return;
      if (status == 'queued') {
        TopToast.show(context, '引擎忙,任务 #${task.id} 已排队');
      }
      state.openTask(task.id);
    } catch (e) {
      if (context.mounted) TopToast.show(context, '$e', error: true);
    }
  }

  Future<void> _stop(BuildContext context) async {
    try {
      await state.stopTask(task.id);
    } catch (e) {
      if (context.mounted) TopToast.show(context, '$e', error: true);
    }
  }

  Future<void> _delete(BuildContext context) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('删除任务'),
        content: Text('删除任务 #${task.id}「${task.name}」及其工作区产物?'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('取消')),
          FilledButton(
              onPressed: () => Navigator.pop(ctx, true), child: const Text('删除')),
        ],
      ),
    );
    if (ok != true) return;
    try {
      await state.deleteTask(task.id);
    } catch (e) {
      if (context.mounted) TopToast.show(context, '$e', error: true);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    final tk = task;
    final active = tk.running || tk.queued;
    return Card(
      child: InkWell(
        borderRadius: BorderRadius.circular(AppConst.radiusCard),
        onTap: () => state.openTask(tk.id),
        child: Padding(
          padding: const EdgeInsets.fromLTRB(16, 13, 12, 13),
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Row(children: [
              Expanded(
                child: Text('#${tk.id} ${tk.name}',
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                        fontSize: 14, fontWeight: FontWeight.w600, color: t.ink)),
              ),
              _StatusChip(task: tk),
            ]),
            const SizedBox(height: 5),
            Text(tk.srcPath,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(fontSize: 11.5, color: t.faint)),
            if (tk.running) ...[
              const SizedBox(height: 9),
              Row(children: [
                Expanded(
                  child: ClipRRect(
                    borderRadius: BorderRadius.circular(4),
                    child: LinearProgressIndicator(
                      value: tk.total > 0 ? tk.progress : null,
                      minHeight: 6,
                      backgroundColor: t.primarySoft,
                    ),
                  ),
                ),
                const SizedBox(width: 10),
                Text(
                  tk.total > 0
                      ? '${tk.stageLabel} ${tk.done}/${tk.total}'
                      : tk.stageLabel,
                  style: TextStyle(fontSize: 11.5, color: t.primaryInk),
                ),
              ]),
            ],
            if (tk.status == 'failed' && tk.error.isNotEmpty) ...[
              const SizedBox(height: 6),
              Text(tk.error,
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(fontSize: 11.5, color: t.danger)),
            ],
            const SizedBox(height: 9),
            Row(children: [
              Text(tk.createdLabel,
                  style: TextStyle(fontSize: 11, color: t.faint)),
              const Spacer(),
              if (active)
                TextButton.icon(
                  onPressed: () => _stop(context),
                  icon: const Icon(Icons.stop, size: 15),
                  label: const Text('停止'),
                )
              else
                TextButton.icon(
                  onPressed: () => _run(context),
                  icon: const Icon(Icons.play_arrow, size: 15),
                  label: Text(tk.status == 'pending' ? '运行' : '重跑'),
                ),
              const SizedBox(width: 4),
              if (!active)
                IconButton(
                  onPressed: () => _delete(context),
                  icon: Icon(Icons.delete_outline, size: 17, color: t.danger),
                  tooltip: '删除任务',
                ),
            ]),
          ]),
        ),
      ),
    );
  }
}

class _StatusChip extends StatelessWidget {
  final TaskInfo task;
  const _StatusChip({required this.task});

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    final (bg, fg) = switch (task.status) {
      'running' => (t.primarySoft, t.primaryInk),
      'queued' => (t.primarySoft, t.primaryInk),
      'succeeded' => (t.successSoft, t.success),
      'failed' => (t.dangerSoft, t.danger),
      _ => (t.primarySoft, t.dim),
    };
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 3),
      decoration:
          BoxDecoration(color: bg, borderRadius: BorderRadius.circular(99)),
      child: Text(task.statusLabel,
          style: TextStyle(fontSize: 11, fontWeight: FontWeight.w600, color: fg)),
    );
  }
}

/// 新建任务向导:① 选择视频 ② 选择输出地址 ③ 选择引擎 ④ 确认开始。
class NewTaskDialog extends StatefulWidget {
  final AppState state;
  const NewTaskDialog({super.key, required this.state});

  @override
  State<NewTaskDialog> createState() => _NewTaskDialogState();
}

class _NewTaskDialogState extends State<NewTaskDialog> {
  static const _stepNames = ['选择视频', '选择输出地址', '选择引擎', '确认开始'];
  static const _engines = [
    ('diffueraser', Icons.auto_awesome, 'DiffuEraser 扩散', '本地 · 最佳画质,慢'),
    ('propainter', Icons.brush, 'ProPainter', '本地 · 画质与速度均衡'),
    ('temporal', Icons.bolt, '时域迁移', '本地 · 最快'),
    ('delogo', Icons.blur_on, '空间修补', '本地 · 单帧修补'),
    ('wanvace', Icons.science, 'Wan-VACE 视频扩散', '本地 · 实验性,很慢'),
    ('online', Icons.cloud, '在线去字幕', '云端 · 按分钟扣点'),
  ];

  final _outDir = TextEditingController();
  final _outName = TextEditingController();
  int _step = 0;
  String _srcPath = '';
  String _engine = 'temporal';
  String? _error;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _engine = engineOfConfig(widget.state.config);
  }

  @override
  void dispose() {
    _outDir.dispose();
    _outName.dispose();
    super.dispose();
  }

  String get _baseName {
    if (_srcPath.isEmpty) return '';
    final base = _srcPath.split(RegExp(r'[\\/]')).last;
    final dot = base.lastIndexOf('.');
    return dot > 0 ? base.substring(0, dot) : base;
  }

  String get _engineLabel =>
      _engines.firstWhere((item) => item.$1 == _engine).$3;

  bool get _canNext => switch (_step) {
        0 => _srcPath.isNotEmpty,
        1 => _outDir.text.trim().isNotEmpty && _outName.text.trim().isNotEmpty,
        2 => true,
        _ => false,
      };

  void _next() {
    if (!_canNext) return;
    if (_step + 1 == 2 && _engine == 'online') {
      widget.state.refreshCloud();
    }
    setState(() => _step++);
  }

  Future<void> _pickVideo() async {
    final file = await openFile(acceptedTypeGroups: const [
      XTypeGroup(label: '视频', extensions: ['mp4', 'mov', 'mkv', 'avi', 'm4v', 'webm', 'ts', 'm2ts']),
    ]);
    if (file == null) return;
    setState(() {
      _srcPath = file.path;
      if (_outDir.text.isEmpty) {
        final normalized = file.path.replaceAll('/', '\\');
        final index = normalized.lastIndexOf('\\');
        final dir = index > 0 ? normalized.substring(0, index) : '';
        _outDir.text = dir.endsWith('\\') ? '$dir去字幕' : '$dir\\去字幕';
      }
      if (_outName.text.isEmpty) {
        final base = file.name.replaceAll(RegExp(r'\.[^.]+$'), '');
        _outName.text = '${base}_fixed.mp4';
      }
    });
  }

  Future<void> _submit(bool runNow) async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final tk = await widget.state.createTask(
        name: _baseName,
        srcPath: _srcPath,
        outName: _outName.text.trim(),
        outDir: _outDir.text.trim(),
        engine: _engine,
        runNow: runNow,
      );
      if (mounted) {
        Navigator.pop(context, tk);
        if (runNow) widget.state.openTask(tk.id);
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _busy = false;
          _error = '$e';
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    return AlertDialog(
      titlePadding: const EdgeInsets.fromLTRB(24, 18, 24, 0),
      title: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Row(children: [
          Text('新建去字幕任务',
              style: TextStyle(
                  fontSize: 16, fontWeight: FontWeight.w700, color: t.ink)),
          const SizedBox(width: 10),
          Text('第 ${_step + 1} 步 / 共 4 步 · ${_stepNames[_step]}',
              style: TextStyle(fontSize: 12, color: t.faint)),
        ]),
        const SizedBox(height: 12),
        Row(children: [
          for (var i = 0; i < 4; i++) ...[
            Expanded(
              child: Container(
                height: 4,
                decoration: BoxDecoration(
                  color: i <= _step ? t.primary : t.border,
                  borderRadius: BorderRadius.circular(2),
                ),
              ),
            ),
            if (i < 3) const SizedBox(width: 6),
          ],
        ]),
      ]),
      content: SizedBox(
        width: 560,
        height: 340,
        child: switch (_step) {
          0 => _stepVideo(t),
          1 => _stepOutput(t),
          2 => _stepEngine(t),
          _ => _stepConfirm(t),
        },
      ),
      actionsPadding: const EdgeInsets.fromLTRB(24, 8, 24, 16),
      actionsAlignment: MainAxisAlignment.spaceBetween,
      actions: [
        TextButton(
          onPressed: _busy ? null : () => Navigator.pop(context),
          child: const Text('取消'),
        ),
        Row(mainAxisSize: MainAxisSize.min, children: [
          if (_step > 0) ...[
            OutlinedButton(
              onPressed: _busy ? null : () => setState(() => _step--),
              child: const Text('上一步'),
            ),
            const SizedBox(width: 10),
          ],
          if (_step < 3)
            FilledButton(
              onPressed: _canNext && !_busy ? _next : null,
              child: const Text('下一步'),
            )
          else ...[
            OutlinedButton(
              onPressed: (_busy || _onlineBlocked) ? null : () => _submit(false),
              child: const Text('暂不运行,仅创建'),
            ),
            const SizedBox(width: 10),
            FilledButton.icon(
              onPressed: (_busy || _onlineBlocked) ? null : () => _submit(true),
              icon: const Icon(Icons.play_arrow, size: 17),
              label: Text(_busy ? '创建中…' : '立即开始运行'),
            ),
          ],
        ]),
      ],
    );
  }

  Widget _stepVideo(AppTokens t) {
    return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
      Text('选择要去字幕的视频文件,原文件不会被修改。',
          style: TextStyle(fontSize: 12.5, color: t.dim)),
      const SizedBox(height: 14),
      InkWell(
        borderRadius: BorderRadius.circular(10),
        onTap: _busy ? null : _pickVideo,
        child: Container(
          width: double.infinity,
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 22),
          decoration: BoxDecoration(
            color: _srcPath.isNotEmpty ? t.primarySoft : t.bg,
            border: Border.all(
                color: _srcPath.isNotEmpty ? t.primary : t.border,
                width: _srcPath.isNotEmpty ? 1.5 : 1),
            borderRadius: BorderRadius.circular(10),
          ),
          child: Row(children: [
            Icon(Icons.video_file,
                size: 26, color: _srcPath.isNotEmpty ? t.primaryInk : t.faint),
            const SizedBox(width: 12),
            Expanded(
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Text(_srcPath.isEmpty ? '点击选择视频' : _baseName,
                    style: TextStyle(
                        fontSize: 13.5,
                        fontWeight: FontWeight.w700,
                        color: _srcPath.isNotEmpty ? t.primaryInk : t.ink)),
                Text(_srcPath.isEmpty ? '支持 mp4 / mkv / mov / avi 等格式' : _srcPath,
                    style: TextStyle(fontSize: 11, color: t.faint),
                    overflow: TextOverflow.ellipsis),
              ]),
            ),
            if (_srcPath.isNotEmpty)
              Icon(Icons.check_circle, size: 18, color: t.primary),
          ]),
        ),
      ),
    ]);
  }

  Widget _stepOutput(AppTokens t) {
    return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
      Text('处理完成的视频默认保存到源视频目录下的「去字幕」文件夹。',
          style: TextStyle(fontSize: 12.5, color: t.dim)),
      const SizedBox(height: 14),
      Row(children: [
        Expanded(
          child: TextField(
            controller: _outDir,
            decoration: const InputDecoration(labelText: '输出目录'),
            onChanged: (_) => setState(() {}),
          ),
        ),
        const SizedBox(width: 10),
        OutlinedButton.icon(
          onPressed: _busy
              ? null
              : () async {
                  final dir = await getDirectoryPath(confirmButtonText: '选择输出目录');
                  if (dir != null) setState(() => _outDir.text = dir);
                },
          icon: const Icon(Icons.folder_open, size: 15),
          label: const Text('浏览'),
        ),
      ]),
      const SizedBox(height: 14),
      TextField(
        controller: _outName,
        decoration: const InputDecoration(labelText: '输出文件名'),
        onChanged: (_) => setState(() {}),
      ),
    ]);
  }

  Widget _stepEngine(AppTokens t) {
    return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
      Text('选择去字幕方式,拿不准就保持当前配置。',
          style: TextStyle(fontSize: 12.5, color: t.dim)),
      const SizedBox(height: 12),
      Expanded(
        child: ListView.separated(
          itemCount: _engines.length,
          separatorBuilder: (_, _) => const SizedBox(height: 8),
          itemBuilder: (_, i) {
            final (value, icon, title, desc) = _engines[i];
            final selected = _engine == value;
            return InkWell(
              borderRadius: BorderRadius.circular(10),
              onTap: _busy
                  ? null
                  : () {
                      setState(() => _engine = value);
                      if (value == 'online') widget.state.refreshCloud();
                    },
              child: Container(
                padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
                decoration: BoxDecoration(
                  color: selected ? t.primarySoft : t.bg,
                  border: Border.all(
                      color: selected ? t.primary : t.border,
                      width: selected ? 1.5 : 1),
                  borderRadius: BorderRadius.circular(10),
                ),
                child: Row(children: [
                  Icon(icon, size: 20, color: selected ? t.primaryInk : t.faint),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                      Text(title,
                          style: TextStyle(
                              fontSize: 13,
                              fontWeight: FontWeight.w700,
                              color: selected ? t.primaryInk : t.ink)),
                      Text(desc, style: TextStyle(fontSize: 11, color: t.faint)),
                    ]),
                  ),
                  if (selected)
                    Icon(Icons.check_circle, size: 16, color: t.primary),
                ]),
              ),
            );
          },
        ),
      ),
      if (_engine == 'online') ...[
        const SizedBox(height: 8),
        _cloudSection(t),
      ],
    ]);
  }

  Widget _stepConfirm(AppTokens t) {
    return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
      Text('确认任务信息,选择创建方式:',
          style: TextStyle(fontSize: 12.5, color: t.dim)),
      const SizedBox(height: 14),
      _summaryRow(t, '视频', _srcPath),
      _summaryRow(t, '输出目录', _outDir.text.trim()),
      _summaryRow(t, '输出文件', _outName.text.trim()),
      _summaryRow(t, '去字幕方式', _engineLabel),
      if (_error != null) ...[
        const SizedBox(height: 10),
        Text(_error!, style: TextStyle(fontSize: 12, color: t.danger)),
      ],
      const Spacer(),
      Text('「立即开始运行」将创建任务并马上开跑(引擎忙则自动排队);「仅创建」加入任务列表稍后再跑。',
          style: TextStyle(fontSize: 11.5, color: t.faint)),
    ]);
  }

  Widget _summaryRow(AppTokens t, String label, String value) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 4),
      child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
        SizedBox(
          width: 76,
          child: Text(label,
              style: TextStyle(
                  fontSize: 12.5, fontWeight: FontWeight.w600, color: t.dim)),
        ),
        Expanded(
          child: Text(value,
              style: TextStyle(fontSize: 12.5, color: t.ink),
              overflow: TextOverflow.ellipsis),
        ),
      ]),
    );
  }

  /// 在线引擎下是否禁止创建:状态未取到/未配置凭据/凭据未通过远端验证。
  bool get _onlineBlocked {
    if (_engine != 'online') return false;
    final cs = widget.state.cloud;
    return cs == null || !cs.linked || !cs.online;
  }

  /// 云端账号状态区(仅在线引擎展示),跟随 AppState 通知刷新。
  Widget _cloudSection(AppTokens t) {
    return AnimatedBuilder(
      animation: widget.state,
      builder: (context, _) {
        final st = widget.state;
        Widget child;
        if (st.cloudLoading && st.cloud == null) {
          child = Row(children: [
            const SizedBox(
                width: 14,
                height: 14,
                child: CircularProgressIndicator(strokeWidth: 2)),
            const SizedBox(width: 8),
            Text('正在查询云端账号状态…',
                style: TextStyle(fontSize: 12.5, color: t.dim)),
          ]);
        } else if (st.cloud == null) {
          // 状态查询失败(引擎未就绪):给重试入口。
          child = Row(children: [
            Icon(Icons.error_outline, size: 15, color: t.danger),
            const SizedBox(width: 6),
            Expanded(
              child: Text('无法获取云端账号状态:${st.cloudError ?? '未知错误'}',
                  style: TextStyle(fontSize: 12.5, color: t.danger),
                  overflow: TextOverflow.ellipsis),
            ),
            TextButton(
                onPressed: st.refreshCloud, child: const Text('重试')),
          ]);
        } else if (!st.cloud!.linked) {
          child = Row(children: [
            Icon(Icons.cloud_off, size: 15, color: t.warn),
            const SizedBox(width: 6),
            Expanded(
              child: Text('未登录云端账号(开发版内部通道,不计点数)',
                  style: TextStyle(fontSize: 12.5, color: t.dim),
                  overflow: TextOverflow.ellipsis),
            ),
            FilledButton.icon(
              onPressed: _loginCloud,
              icon: const Icon(Icons.login, size: 15),
              label: const Text('登录'),
            ),
          ]);
        } else {
          final cs = st.cloud!;
          child = Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(children: [
                Icon(cs.online ? Icons.cloud_done : Icons.cloud_off,
                    size: 15, color: cs.online ? t.success : t.warn),
                const SizedBox(width: 6),
                Expanded(
                  child: Text(
                    '${cs.username} · ${cs.server}'
                    '${cs.online ? '' : '(凭据未通过远端验证)'}',
                    style: TextStyle(fontSize: 12.5, color: t.ink),
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                TextButton(onPressed: _loginCloud, child: const Text('换账号')),
                TextButton(
                  onPressed: _confirmClearCredential,
                  child: Text('清除', style: TextStyle(color: t.danger)),
                ),
              ]),
              if (cs.error.isNotEmpty)
                Padding(
                  padding: const EdgeInsets.only(top: 6),
                  child: Text(cs.error,
                      style: TextStyle(fontSize: 12.5, color: t.danger)),
                ),
              if (cs.degraded)
                Padding(
                  padding: const EdgeInsets.only(top: 6),
                  child: Text('机器码采集降级(仅凭网卡 MAC)',
                      style: TextStyle(fontSize: 12.5, color: t.warn)),
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

  /// 弹登录对话框(登录/换账号共用),成功后状态由 AppState 通知刷新。
  Future<void> _loginCloud() async {
    await showCloudLoginDialog(context, widget.state);
  }

  /// 清除本机云端凭据,二次确认。
  Future<void> _confirmClearCredential() async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('清除云端凭据'),
        content: const Text('清除后本机将不能再使用在线去字幕(远端会话不吊销,可重新登录),确定清除吗?'),
        actions: [
          TextButton(
              onPressed: () => Navigator.of(ctx).pop(false),
              child: const Text('取消')),
          FilledButton(
              onPressed: () => Navigator.of(ctx).pop(true),
              child: const Text('清除')),
        ],
      ),
    );
    if (ok != true) return;
    try {
      await widget.state.clearCloudCredential();
    } catch (e) {
      if (mounted) TopToast.show(context, '$e', error: true);
    }
  }
}
