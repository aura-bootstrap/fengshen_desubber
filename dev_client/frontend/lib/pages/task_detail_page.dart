import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../app_state.dart';
import '../models.dart';
import '../theme.dart';

/// 任务详情:阶段时间线 + 滚动日志 + 报告摘要。
class TaskDetailPage extends StatefulWidget {
  final int taskId;
  final AppState state;
  const TaskDetailPage({super.key, required this.taskId, required this.state});

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
    return Scaffold(
      appBar: AppBar(
        title: Text(tk == null ? '任务 #${widget.taskId}' : '任务 #${tk.id}「${tk.name}」'),
        backgroundColor: t.surface,
      ),
      body: tk == null
          ? const Center(child: CircularProgressIndicator())
          : ListView(
              padding: const EdgeInsets.all(20),
              children: [
                StageTimeline(task: tk),
                const SizedBox(height: 16),
                if (tk.reportJson.isNotEmpty) ReportCard(reportJson: tk.reportJson),
                if (tk.reportJson.isNotEmpty) const SizedBox(height: 16),
                LogConsole(logs: widget.state.logs[widget.taskId] ?? const [], ctrl: logCtrl),
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
    const labels = ['探测', '镜头切分', '字幕检测', '修复', '合成输出', '复检'];
    final cur = task.status == 'succeeded' ? labels.length : task.stageIndex;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Row(
          children: [
            for (var i = 0; i < labels.length; i++) ...[
              _stageDot(context, labels[i],
                  i < cur || (i == cur && task.status == 'succeeded'),
                  i == cur && task.status == 'running'),
              if (i < labels.length - 1)
                Expanded(
                  child: Container(
                    height: 2,
                    color: i < cur ? t.success : t.border,
                  ),
                ),
            ],
          ],
        ),
      ),
    );
  }

  Widget _stageDot(BuildContext context, String label, bool done, bool active) {
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
                ScaffoldMessenger.of(context)
                    .showSnackBar(const SnackBar(content: Text('报告 JSON 已复制')));
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
