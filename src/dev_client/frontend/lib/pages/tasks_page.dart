import 'package:file_selector/file_selector.dart';
import 'package:flutter/material.dart';

import '../app_state.dart';
import '../models.dart';
import '../responsive.dart';
import '../theme.dart';
import '../widgets/top_toast.dart';

/// 任务页:任务卡片列表 + 新建任务向导(选视频 → 命名 → 创建/立即运行)。
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

/// 新建任务向导:选视频 + 命名 + 输出名;「创建」或「创建并运行」。
class NewTaskDialog extends StatefulWidget {
  final AppState state;
  const NewTaskDialog({super.key, required this.state});

  @override
  State<NewTaskDialog> createState() => _NewTaskDialogState();
}

class _NewTaskDialogState extends State<NewTaskDialog> {
  final _name = TextEditingController();
  final _outName = TextEditingController();
  String _srcPath = '';
  String? _error;
  bool _busy = false;

  @override
  void dispose() {
    _name.dispose();
    _outName.dispose();
    super.dispose();
  }

  Future<void> _pickVideo() async {
    final file = await openFile(acceptedTypeGroups: const [
      XTypeGroup(label: '视频', extensions: ['mp4', 'mov', 'mkv', 'avi', 'm4v', 'webm', 'ts', 'm2ts']),
    ]);
    if (file == null) return;
    setState(() {
      _srcPath = file.path;
      final base = file.name.replaceAll(RegExp(r'\.[^.]+$'), '');
      if (_name.text.trim().isEmpty) _name.text = base;
      if (_outName.text.trim().isEmpty) _outName.text = '${base}_fixed.mp4';
    });
  }

  Future<void> _submit(bool runNow) async {
    if (_srcPath.isEmpty) {
      setState(() => _error = '请先选择视频文件');
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final tk = await widget.state.createTask(
        name: _name.text.trim(),
        srcPath: _srcPath,
        outName: _outName.text.trim(),
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
      title: const Text('新建任务'),
      content: SizedBox(
        width: 460,
        child: Column(mainAxisSize: MainAxisSize.min, children: [
          Row(children: [
            Expanded(
              child: Text(
                _srcPath.isEmpty ? '未选择视频' : _srcPath,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(
                    fontSize: 12.5,
                    color: _srcPath.isEmpty ? t.faint : t.ink),
              ),
            ),
            const SizedBox(width: 10),
            OutlinedButton.icon(
              onPressed: _busy ? null : _pickVideo,
              icon: const Icon(Icons.folder_open, size: 15),
              label: const Text('选择视频'),
            ),
          ]),
          const SizedBox(height: 14),
          TextField(
            controller: _name,
            decoration: const InputDecoration(
                labelText: '任务名', hintText: '留空取视频文件名'),
          ),
          const SizedBox(height: 10),
          TextField(
            controller: _outName,
            decoration: const InputDecoration(
                labelText: '产出文件名', hintText: '留空取 任务名_fixed.mp4'),
          ),
          if (_error != null) ...[
            const SizedBox(height: 10),
            Align(
              alignment: Alignment.centerLeft,
              child: Text(_error!,
                  style: TextStyle(fontSize: 12, color: t.danger)),
            ),
          ],
        ]),
      ),
      actions: [
        TextButton(
            onPressed: _busy ? null : () => Navigator.pop(context),
            child: const Text('取消')),
        OutlinedButton(
            onPressed: _busy ? null : () => _submit(false),
            child: const Text('创建')),
        FilledButton(
            onPressed: _busy ? null : () => _submit(true),
            child: const Text('创建并运行')),
      ],
    );
  }
}
