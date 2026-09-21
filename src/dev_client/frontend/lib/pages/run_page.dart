import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';

import '../app_state.dart';
import '../models.dart';
import '../responsive.dart';
import '../theme.dart';
import '../widgets/param_field.dart';
import '../widgets/top_toast.dart';

/// 运行页:选中任务的实时进度 + 引擎日志 + 完成后的报告/产物入口。
class RunPage extends StatelessWidget {
  final AppState state;
  const RunPage({super.key, required this.state});

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    final tk = state.selectedTask ?? state.runningTask;
    return CenteredContent(
      child: ListView(
        padding: const EdgeInsets.fromLTRB(28, 24, 28, 24),
        children: [
          Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Expanded(
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Text('运行',
                    style: TextStyle(
                        fontSize: 21, fontWeight: FontWeight.w700, color: t.ink)),
                const SizedBox(height: 4),
                Text(
                  state.queuedCount > 0
                      ? '队列中还有 ${state.queuedCount} 个任务'
                      : '实时进度来自引擎事件流',
                  style: TextStyle(fontSize: 12.5, color: t.dim),
                ),
              ]),
            ),
            if (tk != null && (tk.running || tk.queued))
              OutlinedButton.icon(
                onPressed: () => state.stopTask(tk.id).catchError((e) {
                  if (context.mounted) TopToast.show(context, '$e', error: true);
                  return null;
                }),
                icon: const Icon(Icons.stop, size: 15),
                label: const Text('停止'),
              )
            else if (tk != null)
              _EngineRunControls(key: ValueKey(tk.id), task: tk, state: state),
          ]),
          const SizedBox(height: 18),
          if (tk == null)
            Card(
              child: Padding(
                padding: const EdgeInsets.all(36),
                child: Center(
                  child: Text('还没有选中的任务;到任务页点一张卡片',
                      style: TextStyle(fontSize: 13, color: t.dim)),
                ),
              ),
            )
          else ...[
            _RunHeader(task: tk, state: state),
            const SizedBox(height: 14),
            if (tk.status == 'succeeded' || tk.status == 'failed')
              _ReportCard(task: tk),
            if (tk.status == 'succeeded' || tk.status == 'failed')
              const SizedBox(height: 14),
            _LogCard(state: state),
          ],
        ],
      ),
    );
  }
}

/// 重跑控制:三引擎下拉(初值取任务快照)+ 重跑按钮;选定引擎随本次运行覆盖任务快照。
class _EngineRunControls extends StatefulWidget {
  final TaskInfo task;
  final AppState state;
  const _EngineRunControls({super.key, required this.task, required this.state});

  @override
  State<_EngineRunControls> createState() => _EngineRunControlsState();
}

class _EngineRunControlsState extends State<_EngineRunControls> {
  String _engine = 'temporal';

  @override
  void initState() {
    super.initState();
    _engine = _engineOf(widget.task.paramsJson);
  }

  static String _engineOf(String paramsJson) {
    try {
      final v = jsonDecode(paramsJson);
      if (v is Map) return engineOfConfig(v.cast<String, dynamic>());
    } catch (_) {}
    return 'temporal';
  }

  Future<void> _rerun() async {
    try {
      final status = await widget.state.runTask(widget.task.id, engine: _engine);
      if (mounted && status == 'queued') {
        TopToast.show(context, '引擎忙,任务 #${widget.task.id} 已排队');
      }
    } catch (e) {
      if (mounted) TopToast.show(context, '$e', error: true);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Row(mainAxisSize: MainAxisSize.min, children: [
      SizedBox(
        width: 250,
        child: StyledDropdown(
          value: _engine,
          options: devEngineOptions.keys.toList(),
          labelOf: (v) => devEngineOptions[v] ?? v,
          decoration:
              const InputDecoration(labelText: '修复引擎', isDense: true),
          onChanged: (v) {
            if (v != null) setState(() => _engine = v);
          },
        ),
      ),
      const SizedBox(width: 10),
      FilledButton.icon(
        onPressed: _rerun,
        icon: const Icon(Icons.play_arrow, size: 15),
        label: const Text('重跑'),
      ),
    ]);
  }
}

class _RunHeader extends StatelessWidget {
  final TaskInfo task;
  final AppState state;
  const _RunHeader({required this.task, required this.state});

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    final tk = task;
    final live = tk.running;
    final stage = live ? state.stage : tk.stage;
    final done = live ? state.doneFrames : tk.done;
    final total = live ? state.totalFrames : tk.total;
    final stageName = switch (stage) {
      'probe' => '探测',
      'cuts' => '镜头切分',
      'detect' => '检测',
      'ocr' => 'OCR',
      'engine' => '引擎路由',
      'repair' => '修补',
      'verify' => '复检',
      'upload' => '上传',
      'cloud' => '云端处理',
      'download' => '下载',
      _ => stage,
    };
    return Card(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 14, 16, 14),
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Row(children: [
            Expanded(
              child: Text('#${tk.id} ${tk.name}',
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(
                      fontSize: 15, fontWeight: FontWeight.w700, color: t.ink)),
            ),
            Text(tk.statusLabel,
                style: TextStyle(
                    fontSize: 12,
                    fontWeight: FontWeight.w600,
                    color: tk.status == 'failed'
                        ? t.danger
                        : (tk.status == 'succeeded' ? t.success : t.primaryInk))),
          ]),
          const SizedBox(height: 4),
          Text(tk.srcPath,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(fontSize: 11.5, color: t.faint)),
          const SizedBox(height: 12),
          Row(children: [
            Expanded(
              child: ClipRRect(
                borderRadius: BorderRadius.circular(4),
                child: LinearProgressIndicator(
                  value: total > 0 ? (done / total).clamp(0.0, 1.0) : (live ? null : 0),
                  minHeight: 8,
                  backgroundColor: t.primarySoft,
                ),
              ),
            ),
            const SizedBox(width: 12),
            Text(
              total > 0
                  ? '$stageName $done/$total (${(100 * done / total).round()}%)'
                  : (live ? stageName : '—'),
              style: TextStyle(
                  fontSize: 12, color: t.primaryInk, fontWeight: FontWeight.w600),
            ),
          ]),
        ]),
      ),
    );
  }
}

class _ReportCard extends StatelessWidget {
  final TaskInfo task;
  const _ReportCard({required this.task});

  Future<void> _openWorkDir(BuildContext context) async {
    if (task.workDir.isEmpty) return;
    try {
      await Process.run('explorer.exe', [task.workDir]);
    } catch (e) {
      if (context.mounted) TopToast.show(context, '打开目录失败:$e', error: true);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    String report = task.reportJson;
    if (report.isNotEmpty) {
      try {
        report = const JsonEncoder.withIndent('  ')
            .convert(jsonDecode(report));
      } catch (_) {}
    }
    return Card(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 14, 16, 14),
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Row(children: [
            Text('结果',
                style: TextStyle(
                    fontSize: 14, fontWeight: FontWeight.w700, color: t.ink)),
            const Spacer(),
            if (task.status == 'succeeded' && task.workDir.isNotEmpty)
              OutlinedButton.icon(
                onPressed: () => _openWorkDir(context),
                icon: const Icon(Icons.folder_open, size: 15),
                label: const Text('打开产物目录'),
              ),
          ]),
          const SizedBox(height: 8),
          if (task.status == 'failed')
            Text(task.error.isEmpty ? '任务失败' : task.error,
                style: TextStyle(fontSize: 12.5, color: t.danger))
          else if (report.isNotEmpty)
            Container(
              width: double.infinity,
              padding: const EdgeInsets.all(10),
              decoration: BoxDecoration(
                color: t.consoleBg,
                borderRadius: BorderRadius.circular(8),
              ),
              child: SelectableText(report,
                  style: TextStyle(
                      fontSize: 11.5,
                      fontFamily: AppConst.fontMono,
                      color: t.dim)),
            )
          else
            Text('产物:${task.outName}',
                style: TextStyle(fontSize: 12.5, color: t.dim)),
        ]),
      ),
    );
  }
}

class _LogCard extends StatelessWidget {
  final AppState state;
  const _LogCard({required this.state});

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    final logs = state.logs;
    return Card(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 14, 16, 14),
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Text('引擎日志',
              style: TextStyle(
                  fontSize: 14, fontWeight: FontWeight.w700, color: t.ink)),
          const SizedBox(height: 8),
          Container(
            width: double.infinity,
            constraints: const BoxConstraints(minHeight: 180, maxHeight: 320),
            padding: const EdgeInsets.all(10),
            decoration: BoxDecoration(
              color: t.consoleBg,
              borderRadius: BorderRadius.circular(8),
            ),
            child: logs.isEmpty
                ? Text('暂无日志', style: TextStyle(fontSize: 12, color: t.faint))
                : ListView.builder(
                    shrinkWrap: true,
                    reverse: true,
                    itemCount: logs.length,
                    itemBuilder: (_, i) {
                      final line = logs[logs.length - 1 - i];
                      final color = switch (line.kind) {
                        LogKind.stage => t.primaryInk,
                        LogKind.ok => t.success,
                        LogKind.err => t.danger,
                        LogKind.plain => t.dim,
                      };
                      return Text('${line.ts}  ${line.text}',
                          style: TextStyle(
                              fontSize: 11.5,
                              fontFamily: AppConst.fontMono,
                              color: color));
                    },
                  ),
          ),
        ]),
      ),
    );
  }
}
