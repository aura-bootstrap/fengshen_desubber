import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../app_state.dart';
import '../models.dart';
import '../responsive.dart';
import '../theme.dart';
import '../widgets/top_toast.dart';

/// 任务详情:阶段时间线 + 滚动日志 + 报告摘要。
/// 嵌在 AppShell 内(非路由),标题栏/状态栏保持可见;onBack 返回列表。
class TaskDetailPage extends StatefulWidget {
  final int taskId;
  final AppState state;
  final VoidCallback onBack;
  const TaskDetailPage(
      {super.key, required this.taskId, required this.state, required this.onBack});

  @override
  State<TaskDetailPage> createState() => _TaskDetailPageState();
}

class _TaskDetailPageState extends State<TaskDetailPage> {
  DesubTask? task;
  final logCtrl = ScrollController();

  @override
  void initState() {
    super.initState();
    widget.state.watchLogs(widget.taskId);
    widget.state.addListener(_sync);
    _sync();
  }

  Future<void> _sync() async {
    try {
      final t = await widget.state.client.taskDetail(widget.taskId);
      if (mounted) setState(() => task = t);
    } catch (_) {}
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (logCtrl.hasClients) logCtrl.jumpTo(logCtrl.position.maxScrollExtent);
    });
  }

  @override
  void dispose() {
    widget.state.removeListener(_sync);
    logCtrl.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    final tk = task;
    return CenteredContent(
      child: Column(
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(12, 10, 20, 6),
            child: Row(
              children: [
                IconButton(
                  tooltip: '返回列表',
                  icon: Icon(Icons.arrow_back, size: 19, color: t.dim),
                  onPressed: widget.onBack,
                ),
                const SizedBox(width: 4),
                Expanded(
                  child: Text(
                    tk == null ? '任务 #${widget.taskId}' : '任务 #${tk.id}「${tk.name}」',
                    style: TextStyle(
                        fontSize: 16, fontWeight: FontWeight.w700, color: t.ink),
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
              ],
            ),
          ),
          Expanded(
            child: tk == null
                ? const Center(child: CircularProgressIndicator())
                : ListView(
                    padding: const EdgeInsets.fromLTRB(20, 4, 20, 20),
                    children: [
                      StageTimeline(task: tk),
                      const SizedBox(height: 16),
                      if (tk.reportJson.isNotEmpty) ReportCard(reportJson: tk.reportJson),
                      if (tk.reportJson.isNotEmpty) const SizedBox(height: 16),
                      LogConsole(
                          logs: widget.state.logs[widget.taskId] ?? const [],
                          ctrl: logCtrl),
                    ],
                  ),
          ),
        ],
      ),
    );
  }
}

class StageTimeline extends StatelessWidget {
  final DesubTask task;
  const StageTimeline({super.key, required this.task});

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    final labels = task.stages.map(DesubTask.labelOf).toList();
    final cur = task.status == 'succeeded' ? labels.length : task.stageIndex;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            for (var i = 0; i < labels.length; i++) ...[
              _stageDot(context, labels[i],
                  i < cur || (i == cur && task.status == 'succeeded'),
                  i == cur && task.status == 'running', _stagePct(i, cur)),
              if (i < labels.length - 1)
                Expanded(
                  child: Container(
                    height: 2,
                    margin: const EdgeInsets.only(top: 11),
                    color: i < cur ? t.success : t.border,
                  ),
                ),
            ],
          ],
        ),
      ),
    );
  }

  /// 每个阶段的百分比:已完成 100%,进行中按阶段内计数(无计数的阶段
  /// 不显示数字,转圈即进行中),未开始 0%。
  String _stagePct(int i, int cur) {
    if (i < cur || task.status == 'succeeded') return '100%';
    if (i > cur) return '0%';
    if (task.status == 'running' && task.total > 0) {
      return '${(task.stageFraction * 100).round()}%';
    }
    return '';
  }

  Widget _stageDot(BuildContext context, String label, bool done, bool active, String pct) {
    final t = context.tokens;
    final color = done ? t.success : active ? t.primary : t.faint;
    return Column(
      children: [
        Container(
          width: 22,
          height: 22,
          decoration: BoxDecoration(
            shape: BoxShape.circle,
            color: done ? t.success : active ? t.primarySoft : t.surface,
            border: Border.all(color: color, width: 2),
          ),
          child: done
              ? const Icon(Icons.check, size: 12, color: Colors.white)
              : active
                  ? const Padding(
                      padding: EdgeInsets.all(4),
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : null,
        ),
        const SizedBox(height: 4),
        Text(label, style: TextStyle(fontSize: 11, color: color)),
        Text(pct,
            style: TextStyle(fontSize: 10, color: t.faint, fontFamily: AppConst.fontMono)),
      ],
    );
  }
}

class ReportCard extends StatelessWidget {
  final String reportJson;
  const ReportCard({super.key, required this.reportJson});

  @override
  Widget build(BuildContext context) {
    Map<String, dynamic> j = {};
    try {
      j = jsonDecode(reportJson) as Map<String, dynamic>;
    } catch (_) {}
    final elapsed = (j['elapsed_sec'] as num?)?.toDouble();
    final events = (j['events'] as List?)?.length ?? 0;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Row(
          children: [
            if (elapsed != null)
              _kv(context, '耗时', '${(elapsed / 60).toStringAsFixed(1)} 分钟'),
            _kv(context, '事件数', '$events'),
            const Spacer(),
            OutlinedButton.icon(
              onPressed: () {
                Clipboard.setData(ClipboardData(text: reportJson));
                TopToast.show(context, '报告 JSON 已复制');
              },
              icon: const Icon(Icons.copy, size: 16),
              label: const Text('复制报告'),
            ),
          ],
        ),
      ),
    );
  }

  Widget _kv(BuildContext context, String k, String v) {
    final t = context.tokens;
    return Padding(
      padding: const EdgeInsets.only(right: 28),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(k, style: TextStyle(fontSize: 11, color: t.faint)),
          Text(v, style: TextStyle(fontSize: 16, fontWeight: FontWeight.w700, color: t.ink)),
        ],
      ),
    );
  }
}

class LogConsole extends StatelessWidget {
  final List<String> logs;
  final ScrollController ctrl;
  const LogConsole({super.key, required this.logs, required this.ctrl});

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    return Container(
      height: 320,
      decoration: BoxDecoration(
        color: t.consoleBg,
        borderRadius: BorderRadius.circular(AppConst.radiusCard),
        border: Border.all(color: t.border),
      ),
      child: logs.isEmpty
          ? Center(child: Text('暂无日志', style: TextStyle(color: t.faint, fontSize: 12)))
          : ListView.builder(
              controller: ctrl,
              padding: const EdgeInsets.all(12),
              itemCount: logs.length,
              itemBuilder: (context, i) => Text(
                logs[i],
                style: const TextStyle(
                    fontSize: 11.5, color: Color(0xFFD5DCE9), fontFamily: AppConst.fontMono),
              ),
            ),
    );
  }
}
