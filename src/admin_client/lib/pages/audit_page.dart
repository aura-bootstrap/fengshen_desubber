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

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final list = await widget.api.audit();
      list.sort((a, b) => b.ts.compareTo(a.ts));
      setState(() {
        _entries = list;
        _error = '';
      });
    } catch (e) {
      setState(() => _error = '$e');
    }
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
                            columnSpacing: 32,
                            horizontalMargin: 16,
                            columns: const [
                              DataColumn(label: Text('时间')),
                              DataColumn(label: Text('操作者')),
                              DataColumn(label: Text('动作')),
                              DataColumn(label: Text('对象')),
                              DataColumn(label: Text('详情')),
                              DataColumn(label: Text('结果')),
                            ],
                            rows: [
                              for (final e in _entries!)
                                DataRow(cells: [
                                  DataCell(Text(fmtTs(e.ts))),
                                  DataCell(Text(e.actor)),
                                  DataCell(Text(e.action)),
                                  DataCell(Text(e.target)),
                                  // 长串(machine=...)限宽省略,保住结果列不出视口
                                  DataCell(SizedBox(
                                    width: 380,
                                    child: Text(e.detail,
                                        overflow: TextOverflow.ellipsis),
                                  )),
                                  DataCell(Icon(
                                      e.ok ? Icons.check_circle : Icons.cancel,
                                      size: 16,
                                      color: e.ok ? t.success : t.danger)),
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
