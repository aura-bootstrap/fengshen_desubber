import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../api.dart';
import '../theme.dart';
import '../widgets/top_toast.dart';

String fmtTs(int unix) {
  if (unix == 0) return '';
  final d = DateTime.fromMillisecondsSinceEpoch(unix * 1000).toLocal();
  String two(int n) => n.toString().padLeft(2, '0');
  return '${d.year}-${two(d.month)}-${two(d.day)} ${two(d.hour)}:${two(d.minute)}';
}

class CardsPage extends StatefulWidget {
  final AdminApi api;
  final ValueChanged<int> onShowTx;
  const CardsPage({super.key, required this.api, required this.onShowTx});

  @override
  State<CardsPage> createState() => _CardsPageState();
}

class _CardsPageState extends State<CardsPage> {
  final _batch = TextEditingController();
  List<CardInfo>? _cards;
  String _error = '';
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _batch.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    setState(() {
      _busy = true;
      _error = '';
    });
    try {
      final cards = await widget.api.listCards(batch: _batch.text.trim());
      setState(() => _cards = cards);
    } catch (e) {
      setState(() => _error = '$e');
    } finally {
      setState(() => _busy = false);
    }
  }

  Future<void> _run(Future<void> Function() op, String okMsg) async {
    try {
      await op();
      if (!mounted) return;
      TopToast.show(context, okMsg);
      _load();
    } catch (e) {
      if (!mounted) return;
      TopToast.show(context, '$e', error: true);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    return Padding(
      padding: const EdgeInsets.fromLTRB(24, 20, 24, 24),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(children: [
            Text('授权码',
                style: TextStyle(
                    fontSize: 21, fontWeight: FontWeight.w700, color: t.ink)),
            const Spacer(),
            OutlinedButton.icon(
                onPressed: _busy ? null : _load,
                icon: const Icon(Icons.refresh, size: 15),
                label: const Text('刷新')),
            const SizedBox(width: 10),
            FilledButton.icon(
                onPressed: () => _generateDialog(context),
                icon: const Icon(Icons.add, size: 16),
                label: const Text('发卡')),
          ]),
          const SizedBox(height: 14),
          Wrap(spacing: 10, runSpacing: 10, crossAxisAlignment: WrapCrossAlignment.center, children: [
            SizedBox(
              width: 180,
              child: TextField(
                controller: _batch,
                style: TextStyle(fontSize: 13, color: t.ink),
                decoration: const InputDecoration(labelText: '批次过滤(空=全部)'),
                onSubmitted: (_) => _load(),
              ),
            ),
            OutlinedButton(
                onPressed: () => _rechargeDialog(context),
                child: const Text('充值')),
            OutlinedButton(
                onPressed: () => _statusDialog(context, 'revoke'),
                child: const Text('吊销')),
            OutlinedButton(
                onPressed: () => _statusDialog(context, 'unrevoke'),
                child: const Text('恢复')),
            OutlinedButton(
                onPressed: () => _queryDialog(context),
                child: const Text('卡状态查询')),
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
              child: Text(_error,
                  style: TextStyle(fontSize: 12.5, color: t.danger)),
            ),
          Expanded(
            child: _cards == null
                ? const Center(child: CircularProgressIndicator())
                : _cards!.isEmpty
                    ? Card(
                        child: Padding(
                          padding: const EdgeInsets.symmetric(vertical: 48),
                          child: Center(
                              child: Text('无卡',
                                  style: TextStyle(fontSize: 13, color: t.faint))),
                        ),
                      )
                    : Card(
                        clipBehavior: Clip.antiAlias,
                        child: LayoutBuilder(
                          // 表宽撑满卡片;列总宽超出时横向滚动,不裁操作列
                          builder: (context, bc) => SingleChildScrollView(
                            scrollDirection: Axis.horizontal,
                            child: ConstrainedBox(
                              constraints:
                                  BoxConstraints(minWidth: bc.maxWidth),
                              child: SingleChildScrollView(
                                child: DataTable(
                              // 压缩默认列距(56/24),1280 宽窗口下 8 列刚好铺满不滚动
                              columnSpacing: 28,
                              horizontalMargin: 16,
                              columns: const [
                                DataColumn(label: Text('ID')),
                                DataColumn(label: Text('名称')),
                                DataColumn(label: Text('卡面(脱敏)')),
                                DataColumn(label: Text('批次')),
                                DataColumn(label: Text('余额')),
                                DataColumn(label: Text('状态')),
                                DataColumn(label: Text('创建时间')),
                                DataColumn(label: Text('操作')),
                              ],
                              rows: [
                                for (final c in _cards!)
                                  DataRow(cells: [
                                    DataCell(Text('${c.id}')),
                                    DataCell(Text(c.name)),
                                    DataCell(Text(c.codeMasked,
                                        style: const TextStyle(
                                            fontFamily: AppConst.fontMono))),
                                    DataCell(Text(c.batch)),
                                    DataCell(Text('${c.balance}')),
                                    DataCell(_StatusChip(status: c.status)),
                                    DataCell(Text(fmtTs(c.createdAt))),
                                    DataCell(Row(children: [
                                      TextButton(
                                          onPressed: c.status == 'revoked'
                                              ? null
                                              : () => _confirm(
                                                  '解绑卡 #${c.id}?',
                                                  () => _run(
                                                      () => widget.api.unbind(cardId: c.id),
                                                      '已解绑')),
                                          child: const Text('解绑')),
                                      TextButton(
                                          onPressed: () => widget.onShowTx(c.id),
                                          child: const Text('流水')),
                                    ])),
                                  ]),
                              ],
                                ),
                              ),
                            ),
                          ),
                        ),
                      ),
          ),
        ],
      ),
    );
  }

  void _confirm(String msg, VoidCallback onOk) {
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        content: Text(msg),
        actions: [
          TextButton(
              onPressed: () => Navigator.pop(ctx), child: const Text('取消')),
          FilledButton(
              onPressed: () {
                Navigator.pop(ctx);
                onOk();
              },
              child: const Text('确认')),
        ],
      ),
    );
  }

  void _generateDialog(BuildContext context) {
    final count = TextEditingController(text: '1');
    final credits = TextEditingController(text: '600');
    final name = TextEditingController();
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('批量发卡'),
        content: SizedBox(
          width: 320,
          child: Column(mainAxisSize: MainAxisSize.min, children: [
            TextField(
                controller: count,
                keyboardType: TextInputType.number,
                decoration: const InputDecoration(labelText: '数量(1-1000)')),
            const SizedBox(height: 12),
            TextField(
                controller: credits,
                keyboardType: TextInputType.number,
                decoration: const InputDecoration(labelText: '每张点数')),
            const SizedBox(height: 12),
            TextField(
                controller: name,
                decoration: const InputDecoration(labelText: '备注（可选）')),
          ]),
        ),
        actions: [
          TextButton(
              onPressed: () => Navigator.pop(ctx), child: const Text('取消')),
          FilledButton(
            onPressed: () async {
              final n = int.tryParse(count.text) ?? 0;
              final cr = int.tryParse(credits.text) ?? -1;
              if (n <= 0 || n > 1000 || cr < 0) return;
              Navigator.pop(ctx);
              try {
                final cards = await widget.api
                    .generate(n, cr, name.text.trim());
                if (!mounted) return;
                _issuedDialog(cards);
                _load();
              } catch (e) {
                if (!context.mounted) return;
                TopToast.show(context, '$e', error: true);
              }
            },
            child: const Text('生成'),
          ),
        ],
      ),
    );
  }

  void _issuedDialog(List<Map<String, dynamic>> cards) {
    final text = cards.map((c) => c['code'] as String).join('\n');
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('已签发 ${cards.length} 张(明文仅此一次)'),
        content: SizedBox(
          width: 420,
          child: SingleChildScrollView(
            child: SelectableText(text,
                style: const TextStyle(
                    fontFamily: AppConst.fontMono, fontSize: 13)),
          ),
        ),
        actions: [
          TextButton(
            onPressed: () {
              Clipboard.setData(ClipboardData(text: text));
              TopToast.show(context, '已复制全部卡面');
            },
            child: const Text('复制全部'),
          ),
          FilledButton(
              onPressed: () => Navigator.pop(ctx), child: const Text('关闭')),
        ],
      ),
    );
  }

  void _rechargeDialog(BuildContext context) {
    final card = TextEditingController();
    final credits = TextEditingController();
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('卡面充值'),
        content: SizedBox(
          width: 320,
          child: Column(mainAxisSize: MainAxisSize.min, children: [
            TextField(
                controller: card,
                style: const TextStyle(fontFamily: AppConst.fontMono),
                decoration: const InputDecoration(labelText: '卡面(明文)')),
            const SizedBox(height: 12),
            TextField(
                controller: credits,
                keyboardType: TextInputType.number,
                decoration: const InputDecoration(labelText: '加点数')),
          ]),
        ),
        actions: [
          TextButton(
              onPressed: () => Navigator.pop(ctx), child: const Text('取消')),
          FilledButton(
            onPressed: () async {
              final cr = int.tryParse(credits.text) ?? 0;
              if (card.text.trim().isEmpty || cr <= 0) return;
              Navigator.pop(ctx);
              try {
                final bal =
                    await widget.api.recharge(card.text.trim(), cr);
                if (!context.mounted) return;
                TopToast.show(context, '充值后余额 $bal');
                _load();
              } catch (e) {
                if (!context.mounted) return;
                TopToast.show(context, '$e', error: true);
              }
            },
            child: const Text('充值'),
          ),
        ],
      ),
    );
  }

  void _statusDialog(BuildContext context, String action) {
    final card = TextEditingController();
    final batch = TextEditingController();
    final title = action == 'revoke' ? '吊销(单卡或整批)' : '恢复(单卡或整批)';
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(title),
        content: SizedBox(
          width: 320,
          child: Column(mainAxisSize: MainAxisSize.min, children: [
            TextField(
                controller: card,
                style: const TextStyle(fontFamily: AppConst.fontMono),
                decoration: const InputDecoration(labelText: '卡面(明文,优先)')),
            const SizedBox(height: 12),
            TextField(
                controller: batch,
                decoration: const InputDecoration(labelText: '批次(卡面为空时整批)')),
          ]),
        ),
        actions: [
          TextButton(
              onPressed: () => Navigator.pop(ctx), child: const Text('取消')),
          FilledButton(
            onPressed: () {
              if (card.text.trim().isEmpty && batch.text.trim().isEmpty) return;
              Navigator.pop(ctx);
              _run(
                  () => widget.api.setStatus(action,
                      card: card.text.trim(), batch: batch.text.trim()),
                  '已执行');
            },
            child: const Text('执行'),
          ),
        ],
      ),
    );
  }

  void _queryDialog(BuildContext context) {
    final card = TextEditingController();
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('卡状态查询'),
        content: SizedBox(
          width: 320,
          child: TextField(
              controller: card,
              autofocus: true,
              style: const TextStyle(fontFamily: AppConst.fontMono),
              decoration: const InputDecoration(labelText: '卡面(明文)')),
        ),
        actions: [
          TextButton(
              onPressed: () => Navigator.pop(ctx), child: const Text('取消')),
          FilledButton(
            onPressed: () async {
              if (card.text.trim().isEmpty) return;
              try {
                final d = await widget.api.cardStatus(card.text.trim());
                if (!ctx.mounted) return;
                Navigator.pop(ctx);
                showDialog(
                  context: context,
                  builder: (c2) => AlertDialog(
                    title: const Text('卡状态'),
                    content: Text(
                        '状态: ${d['status']}\n余额: ${d['balance']}\n名称: ${d['name']}\n批次: ${d['batch']}'),
                    actions: [
                      FilledButton(
                          onPressed: () => Navigator.pop(c2),
                          child: const Text('关闭'))
                    ],
                  ),
                );
              } catch (e) {
                if (!ctx.mounted) return;
                Navigator.pop(ctx);
                TopToast.show(context, '$e', error: true);
              }
            },
            child: const Text('查询'),
          ),
        ],
      ),
    );
  }
}

class _StatusChip extends StatelessWidget {
  final String status;
  const _StatusChip({required this.status});

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    final (label, color) = switch (status) {
      'active' => ('已激活', t.success),
      'inactive' => ('未激活', t.faint),
      'revoked' => ('已吊销', t.danger),
      _ => (status, t.violet),
    };
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
      decoration: BoxDecoration(
          color: color.withValues(alpha: 0.12),
          borderRadius: BorderRadius.circular(10)),
      child: Text(label, style: TextStyle(fontSize: 12, color: color)),
    );
  }
}
