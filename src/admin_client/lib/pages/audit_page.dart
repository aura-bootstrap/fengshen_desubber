import 'package:flutter/material.dart';

import '../api.dart';
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
    return Padding(
      padding: const EdgeInsets.all(16),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        FilledButton.tonalIcon(
            onPressed: _load,
            icon: const Icon(Icons.refresh, size: 16),
            label: const Text('刷新')),
        const SizedBox(height: 12),
        if (_error.isNotEmpty)
          Text(_error, style: TextStyle(color: Theme.of(context).colorScheme.error)),
        Expanded(
          child: _entries == null
              ? const Center(child: CircularProgressIndicator())
              : _entries!.isEmpty
                  ? const Center(child: Text('无审计记录'))
                  : SingleChildScrollView(
                      child: SizedBox(
                        width: double.infinity,
                        child: DataTable(
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
                                DataCell(Text(e.detail)),
                                DataCell(Icon(
                                    e.ok ? Icons.check_circle : Icons.cancel,
                                    size: 16,
                                    color: e.ok ? Colors.green : Colors.red)),
                              ]),
                          ],
                        ),
                      ),
                    ),
        ),
      ]),
    );
  }
}
