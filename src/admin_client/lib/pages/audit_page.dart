import 'dart:async';

import 'package:flutter/material.dart';

import '../api.dart';
import '../theme.dart';
import 'cards_page.dart' show fmtTs;

class AuditPage extends StatefulWidget {
  final AdminApi api;
  const AuditPage({super.key, required this.api});

  @override
  State<AuditPage> createState() => _AuditPageState();
}

class _AuditPageState extends State<AuditPage> {
  List<AuditEntry>? _entries;
  String _error = '';
  Timer? _refreshTimer;
  bool _loading = false;

  @override
  void initState() {
    super.initState();
    _load();
    _refreshTimer = Timer.periodic(const Duration(seconds: 3), (_) => _load());
  }

  @override
  void dispose() {
    _refreshTimer?.cancel();
    super.dispose();
  }

  Future<void> _load() async {
    if (_loading) return;
    _loading = true;
    try {
      final list = await widget.api.audit();
      list.sort((a, b) {
        final aTime = a.updatedAt == 0 ? a.ts : a.updatedAt;
        final bTime = b.updatedAt == 0 ? b.ts : b.updatedAt;
        return bTime.compareTo(aTime);
      });
      if (!mounted) return;
      setState(() {
        _entries = list;
        _error = '';
      });
    } catch (e) {
      if (mounted) setState(() => _error = '$e');
    } finally {
      _loading = false;
    }
  }

  String _actionLabel(String action) => switch (action) {
        'task_created' => '创建任务',
        'task_submitting' => '准备扣费',
        'task_debit_failed' => '扣费失败',
        'task_debited' => '已扣费提交',
        'task_completed' => '处理完成',
        'task_failed' => '处理失败/退款',
        _ => action,
      };

  String _duration(int seconds) {
    if (seconds <= 0) return '';
    final m = seconds ~/ 60;
    final s = seconds % 60;
    return m == 0 ? '$s秒' : '$m分$s秒';
  }

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    return Padding(
      padding: const EdgeInsets.fromLTRB(24, 20, 24, 24),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Row(children: [
          Text('审计',
              style: TextStyle(
                  fontSize: 21, fontWeight: FontWeight.w700, color: t.ink)),
          const Spacer(),
          OutlinedButton.icon(
              onPressed: _load,
              icon: const Icon(Icons.refresh, size: 15),
              label: const Text('刷新')),
        ]),
        const SizedBox(height: 14),
        if (_error.isNotEmpty)
          Container(
            margin: const EdgeInsets.only(bottom: 12),
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
            decoration: BoxDecoration(
              color: t.dangerSoft,
              borderRadius: BorderRadius.circular(8),
            ),
            child:
                Text(_error, style: TextStyle(fontSize: 12.5, color: t.danger)),
          ),
        Expanded(
          child: _entries == null
              ? const Center(child: CircularProgressIndicator())
              : _entries!.isEmpty
                  ? Card(
                      child: Padding(
                        padding: const EdgeInsets.symmetric(vertical: 48),
                        child: Center(
                            child: Text('无审计记录',
                                style:
                                    TextStyle(fontSize: 13, color: t.faint))),
                      ),
                    )
                  : Card(
                      clipBehavior: Clip.antiAlias,
                      child: LayoutBuilder(
                        // 表宽撑满卡片;列总宽超出时横向滚动
                        builder: (context, bc) => SingleChildScrollView(
                          scrollDirection: Axis.horizontal,
                          child: ConstrainedBox(
                            constraints: BoxConstraints(minWidth: bc.maxWidth),
                            child: SingleChildScrollView(
                              child: DataTable(
                                columnSpacing: 24,
                                horizontalMargin: 16,
                                columns: const [
                                  DataColumn(label: Text('发生时间')),
                                  DataColumn(label: Text('更新时间')),
                                  DataColumn(label: Text('事件')),
                                  DataColumn(label: Text('任务状态')),
                                  DataColumn(label: Text('任务 ID')),
                                  DataColumn(label: Text('文件路径')),
                                  DataColumn(label: Text('时长')),
                                  DataColumn(label: Text('拟扣费')),
                                  DataColumn(label: Text('是否扣费')),
                                  DataColumn(label: Text('实扣点数')),
                                  DataColumn(label: Text('扣后余额')),
                                  DataColumn(label: Text('机器号')),
                                  DataColumn(label: Text('卡号')),
                                  DataColumn(label: Text('平台')),
                                  DataColumn(label: Text('详情/错误')),
                                  DataColumn(label: Text('结果')),
                                ],
                                rows: [
                                  for (final e in _entries!)
                                    DataRow(cells: [
                                      DataCell(Text(fmtTs(e.ts))),
                                      DataCell(Text(e.updatedAt == 0
                                          ? ''
                                          : fmtTs(e.updatedAt))),
                                      DataCell(Text(_actionLabel(e.action))),
                                      DataCell(Text(e.status)),
                                      DataCell(SelectableText(e.taskId.isEmpty
                                          ? e.target
                                          : e.taskId)),
                                      DataCell(SizedBox(
                                        width: 320,
                                        child: SelectableText(e.sourcePath.isEmpty
                                            ? e.sourceName
                                            : e.sourcePath),
                                      )),
                                      DataCell(Text(_duration(e.durationSec))),
                                      DataCell(Text(e.estimatedCost == 0
                                          ? ''
                                          : '${e.estimatedCost} 点')),
                                      DataCell(Text(e.scope == 'task'
                                          ? (e.charged ? '是' : '否')
                                          : '')),
                                      DataCell(Text(e.cost == 0 ? '' : '${e.cost} 点')),
                                      DataCell(Text(e.balanceAfter?.toString() ?? '')),
                                      DataCell(SizedBox(
                                        width: 260,
                                        child: SelectableText(e.machineHash),
                                      )),
                                      DataCell(Text(e.cardMasked.isEmpty
                                          ? (e.cardId == 0 ? '' : '#${e.cardId}')
                                          : e.cardMasked)),
                                      DataCell(Text(e.provider)),
                                      DataCell(SizedBox(
                                        width: 280,
                                        child: Text(
                                            e.error.isEmpty ? e.detail : e.error,
                                            overflow: TextOverflow.ellipsis),
                                      )),
                                      DataCell(Icon(
                                          e.ok
                                              ? Icons.check_circle
                                              : Icons.cancel,
                                          size: 16,
                                          color:
                                              e.ok ? t.success : t.danger)),
                                    ]),
                                ],
                              ),
                            ),
                          ),
                        ),
                      ),
                    ),
        ),
      ]),
    );
  }
}
