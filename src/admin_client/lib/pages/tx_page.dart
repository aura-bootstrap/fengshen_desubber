import 'package:flutter/material.dart';

import '../api.dart';

class TxPage extends StatefulWidget {
  final AdminApi api;
  final ValueNotifier<int> initialCardId;
  const TxPage({super.key, required this.api, required this.initialCardId});

  @override
  State<TxPage> createState() => _TxPageState();
}

class _TxPageState extends State<TxPage> {
  late final TextEditingController _cardId;
  List<CreditTx>? _txs;
  String _error = '';

  @override
  void initState() {
    super.initState();
    _cardId = TextEditingController(
        text: widget.initialCardId.value == 0 ? '' : '${widget.initialCardId.value}');
    if (widget.initialCardId.value != 0) _load();
  }

  Future<void> _load() async {
    final id = int.tryParse(_cardId.text.trim()) ?? 0;
    if (id == 0) return;
    setState(() {
      _error = '';
      _txs = null;
    });
    try {
      final list = await widget.api.transactions(id);
      setState(() => _txs = list.reversed.toList());
    } catch (e) {
      setState(() => _error = '$e');
    }
  }

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.all(16),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Row(children: [
          SizedBox(
            width: 160,
            child: TextField(
              controller: _cardId,
              keyboardType: TextInputType.number,
              style: const TextStyle(fontSize: 13),
              decoration: const InputDecoration(
                  labelText: '卡 ID', border: OutlineInputBorder(), isDense: true),
              onSubmitted: (_) => _load(),
            ),
          ),
          const SizedBox(width: 8),
          FilledButton.tonal(onPressed: _load, child: const Text('查询')),
        ]),
        const SizedBox(height: 12),
        if (_error.isNotEmpty)
          Text(_error, style: TextStyle(color: Theme.of(context).colorScheme.error)),
        Expanded(
          child: _txs == null
              ? const Center(child: Text('输入卡 ID 查询流水'))
              : _txs!.isEmpty
                  ? const Center(child: Text('无流水'))
                  : SingleChildScrollView(
                      child: SizedBox(
                        width: double.infinity,
                        child: DataTable(
                          columns: const [
                            DataColumn(label: Text('ID')),
                            DataColumn(label: Text('类型')),
                            DataColumn(label: Text('金额')),
                            DataColumn(label: Text('余额')),
                            DataColumn(label: Text('任务')),
                            DataColumn(label: Text('时间')),
                          ],
                          rows: [
                            for (final t in _txs!)
                              DataRow(cells: [
                                DataCell(Text('${t.id}')),
                                DataCell(Text(t.kind)),
                                DataCell(Text('${t.amount}',
                                    style: TextStyle(
                                        color: t.amount < 0 ? Colors.red : Colors.green))),
                                DataCell(Text('${t.balanceAfter}')),
                                DataCell(Text(t.taskId,
                                    style: const TextStyle(
                                        fontFamily: 'monospace', fontSize: 11))),
                                DataCell(Text(t.createdAt.replaceFirst('T', ' ').split('.').first)),
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
