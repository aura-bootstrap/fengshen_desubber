import 'package:flutter/material.dart';

import '../app_state.dart';
import '../models.dart';
import '../responsive.dart';
import '../theme.dart';
import '../widgets/top_toast.dart';
import 'new_task_dialog.dart';

class TasksPage extends StatelessWidget {
  final AppState state;
  final ValueChanged<int> onOpenDetail;
  const TasksPage({super.key, required this.state, required this.onOpenDetail});

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    return CenteredContent(
      child: ListenableBuilder(
        listenable: state,
        builder: (context, _) {
          return Column(
            children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(20, 16, 20, 8),
                child: Row(
                  children: [
                    Text('任务', style: TextStyle(fontSize: 18, fontWeight: FontWeight.w700, color: t.ink)),
                    const SizedBox(width: 8),
                    Text('${state.tasks.length}', style: TextStyle(fontSize: 13, color: t.faint)),
                    const Spacer(),
                    FilledButton.icon(
                      onPressed: state.engineReady
                          ? () => showNewTaskDialog(context, state)
                          : null,
                      icon: const Icon(Icons.add, size: 18),
                      label: const Text('新建任务'),
                    ),
                  ],
                ),
              ),
              Expanded(
                child: state.tasks.isEmpty
                    ? Center(
                        child: Text('暂无任务,点右上角「新建任务」选择视频开始',
                            style: TextStyle(color: t.faint)))
                    : ListView.separated(
                        padding: const EdgeInsets.fromLTRB(20, 4, 20, 20),
                        itemCount: state.tasks.length,
                        separatorBuilder: (_, _) => const SizedBox(height: 10),
                        itemBuilder: (context, i) => TaskCard(
                            task: state.tasks[i], state: state, onOpenDetail: onOpenDetail),
                      ),
              ),
            ],
          );
        },
      ),
    );
  }
}

class TaskCard extends StatelessWidget {
  final DesubTask task;
  final AppState state;
  final ValueChanged<int> onOpenDetail;
  const TaskCard({super.key, required this.task, required this.state, required this.onOpenDetail});

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    final (badgeColor, badgeBg, statusText) = switch (task.status) {
      'running' => (t.primary, t.primarySoft, '运行中'),
      'queued' => (t.warn, t.primarySoft, '排队中'),
      'succeeded' => (t.success, t.successSoft, '已完成'),
      'failed' => (t.danger, t.dangerSoft, '失败'),
      'stopped' => (t.dim, t.surface, '已停止'),
      _ => (t.faint, t.surface, '待运行'),
    };
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(task.name,
                      style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600, color: t.ink),
                      overflow: TextOverflow.ellipsis),
                ),
                Container(
                  padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
                  decoration: BoxDecoration(
                    color: badgeBg,
                    borderRadius: BorderRadius.circular(999),
                    border: Border.all(color: badgeColor.withValues(alpha: .4)),
                  ),
                  child: Text(statusText, style: TextStyle(fontSize: 11, color: badgeColor)),
                ),
                const SizedBox(width: 8),
                if (task.active)
                  Text(task.stageLabel, style: TextStyle(fontSize: 12, color: t.dim)),
              ],
            ),
            const SizedBox(height: 6),
            Text(task.srcPath,
                style: TextStyle(fontSize: 11, color: t.faint, fontFamily: AppConst.fontMono),
                overflow: TextOverflow.ellipsis),
            if (task.error.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 4),
                child: Text(task.error,
                    style: TextStyle(fontSize: 11, color: t.danger),
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis),
              ),
            const SizedBox(height: 10),
            Row(
              children: [
                Expanded(
                  child: LinearProgressIndicator(
                    // 总进度 = (已过阶段 + 阶段内进度)/阶段数,每个阶段都有确定值;
                    // 仅刚启动(未入任何阶段)时走不确定动画。
                    value: task.status == 'running' && task.stageIndex < 0
                        ? null
                        : task.progress,
                    minHeight: 6,
                    borderRadius: BorderRadius.circular(3),
                    valueColor: AlwaysStoppedAnimation(switch (task.status) {
                      'succeeded' => t.success,
                      'failed' => t.danger,
                      'stopped' => t.dim,
                      _ => t.primary,
                    }),
                  ),
                ),
                const SizedBox(width: 10),
                Text(
                  _progressText(task),
                  style: TextStyle(fontSize: 11, color: t.dim, fontFamily: AppConst.fontMono),
                ),
              ],
            ),
            const SizedBox(height: 10),
            Row(
              children: [
                if (task.status != 'running') ...[
                  OutlinedButton.icon(
                    onPressed: () => _act(context, () => state.action(() => state.client.runTask(task.id))),
                    icon: const Icon(Icons.play_arrow, size: 16),
                    label: Text(task.status == 'stopped' || task.status == 'failed' ? '重跑' : '运行'),
                  ),
                  const SizedBox(width: 8),
                ],
                if (task.active)
                  OutlinedButton.icon(
                    onPressed: () => _act(context, () => state.action(() => state.client.stopTask(task.id))),
                    icon: const Icon(Icons.stop, size: 16),
                    label: const Text('停止'),
                  ),
                const SizedBox(width: 8),
                OutlinedButton.icon(
                  onPressed: () => onOpenDetail(task.id),
                  icon: const Icon(Icons.notes, size: 16),
                  label: const Text('详情'),
                ),
                if (task.status == 'succeeded') ...[
                  const SizedBox(width: 8),
                  OutlinedButton.icon(
                    onPressed: () => state.openWorkDir(task),
                    icon: const Icon(Icons.folder_open, size: 16),
                    label: const Text('打开产物'),
                  ),
                ],
                const Spacer(),
                IconButton(
                  tooltip: '删除任务',
                  icon: Icon(Icons.delete_outline, size: 18, color: t.faint),
                  onPressed: task.active ? null : () => _confirmDelete(context),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }

  /// 进度文本:阶段内可计数时附计数(repair=帧,upload/download=MB),否则只给总百分比。
  String _progressText(DesubTask task) {
    final pct = '${(task.progress * 100).round()}%';
    if (task.total > 0) {
      if (task.stage == 'repair') return '$pct(${task.done}/${task.total} 帧)';
      String mb(int v) => (v / 1048576).toStringAsFixed(1);
      return '$pct(${mb(task.done)}/${mb(task.total)} MB)';
    }
    if (task.status == 'running' || task.status == 'succeeded') return pct;
    return '';
  }

  Future<void> _act(BuildContext context, Future<String?> Function() op) async {
    final err = await op();
    if (err != null && context.mounted) {
      TopToast.show(context, err, error: true);
    }
  }

  Future<void> _confirmDelete(BuildContext context) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('删除任务'),
        content: Text('删除「${task.name}」及其工作区?产物文件也会一并删除。'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('取消')),
          FilledButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('删除')),
        ],
      ),
    );
    if (ok == true && context.mounted) {
      _act(context, () => state.action(() => state.client.deleteTask(task.id)));
    }
  }
}
