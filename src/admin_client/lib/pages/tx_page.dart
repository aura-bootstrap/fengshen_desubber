import 'package:flutter/material.dart';

import '../api.dart';
import '../theme.dart';

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

  @override
  void dispose() {
    _cardId.dispose();
    super.dispose();
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
    final t = context.tokens;
    return Padding(
      padding: const EdgeInsets.fromLTRB(24, 20, 24, 24),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Row(children: [
          Text('交易流水',
              style: TextStyle(
                  fontSize: 21, fontWeight: FontWeight.w700, color: t.ink)),
        ]),
        const SizedBox(height: 14),
        Row(children: [
          SizedBox(
            width: 160,
            child: TextField(
              controller: _cardId,
              keyboardType: TextInputType.number,
              style: TextStyle(fontSize: 13, color: t.ink),
              decoration: const InputDecoration(labelText: '卡 ID'),
              onSubmitted: (_) => _load(),
            ),
          ),
          const SizedBox(width: 10),
          FilledButton(onPressed: _load, child: const Text('查询')),
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
          child: _txs == null
              ? Center(
                  child: Text('输入卡 ID 查询流水',
                      style: TextStyle(fontSize: 13, color: t.faint)))
              : _txs!.isEmpty
                  ? Card(
                      child: Padding(
                        padding: const EdgeInsets.symmetric(vertical: 48),
                        child: Center(
                            child: Text('无流水',
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
                              DataColumn(label: Text('ID')),
                              DataColumn(label: Text('类型')),
                              DataColumn(label: Text('金额')),
                              DataColumn(label: Text('余额')),
                              DataColumn(label: Text('任务')),
                              DataColumn(label: Text('时间')),
                            ],
                            rows: [
                              for (final tx in _txs!)
                                DataRow(cells: [
                                  DataCell(Text('${tx.id}')),
                                  DataCell(Text(tx.kind)),
                                  DataCell(Text('${tx.amount}',
                                      style: TextStyle(
                                          color: tx.amount < 0
                                              ? t.danger
                                              : t.success))),
                                  DataCell(Text('${tx.balanceAfter}')),
                                  DataCell(Text(tx.taskId,
                                      style: const TextStyle(
                                          fontFamily: AppConst.fontMono,
                                          fontSize: 11))),
                                  DataCell(Text(tx.createdAt
                                      .replaceFirst('T', ' ')
                                      .split('.')
                                      .first)),
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
