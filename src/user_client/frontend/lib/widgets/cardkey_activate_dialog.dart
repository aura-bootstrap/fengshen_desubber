import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../app_state.dart';
import '../theme.dart';

/// 卡号输入格式化:大写 + 每 5 位加横(XXXXX-XXXXX-…),只允许字母数字。
class CardKeyFormatter extends TextInputFormatter {
  @override
  TextEditingValue formatEditUpdate(
      TextEditingValue oldValue, TextEditingValue newValue) {
    final raw =
        newValue.text.toUpperCase().replaceAll(RegExp(r'[^A-Z0-9]'), '');
    final buf = StringBuffer();
    for (var i = 0; i < raw.length; i++) {
      if (i > 0 && i % 5 == 0) buf.write('-');
      buf.write(raw[i]);
    }
    final text = buf.toString();
    return TextEditingValue(
      text: text,
      selection: TextSelection.collapsed(offset: text.length),
    );
  }
}

/// 卡密激活对话框:服务器地址 + 卡号输入,激活中 spinner,
/// 失败直接展示服务端返回的中文错误文案。激活成功 pop(true),取消 pop(false)。
Future<bool?> showCardKeyActivateDialog(BuildContext context, AppState state) {
  return showDialog<bool>(
    context: context,
    barrierDismissible: false,
    builder: (_) => _CardKeyActivateDialog(state: state),
  );
}

class _CardKeyActivateDialog extends StatefulWidget {
  final AppState state;
  const _CardKeyActivateDialog({required this.state});

  @override
  State<_CardKeyActivateDialog> createState() => _CardKeyActivateDialogState();
}

class _CardKeyActivateDialogState extends State<_CardKeyActivateDialog> {
  final _server = TextEditingController();
  final _key = TextEditingController();
  bool _busy = false;
  String _error = '';

  @override
  void initState() {
    super.initState();
    // 默认回填状态里的服务器地址;无则留空走 hint 示例。
    _server.text = widget.state.cardKey?.server ?? '';
  }

  @override
  void dispose() {
    _server.dispose();
    _key.dispose();
    super.dispose();
  }

  bool get _serverOk {
    final s = _server.text.trim();
    return s.startsWith('http://') || s.startsWith('https://');
  }

  bool get _keyOk => _key.text.trim().replaceAll('-', '').length >= 10;

  Future<void> _activate() async {
    if (!_serverOk || !_keyOk || _busy) return;
    setState(() {
      _busy = true;
      _error = '';
    });
    try {
      await widget.state.activateCardKey(_server.text.trim(), _key.text.trim());
      if (mounted) Navigator.of(context).pop(true);
    } catch (e) {
      if (mounted) {
        setState(() {
          _busy = false;
          _error = '$e'; // ApiException.toString 已提取服务端中文错误文案
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    return AlertDialog(
      backgroundColor: t.surface,
      title: Text('激活卡密',
          style: TextStyle(
              fontSize: 16, fontWeight: FontWeight.w700, color: t.ink)),
      content: SizedBox(
        width: 420,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('输入计费服务器地址与卡号。激活后本机与该卡绑定,在线去字幕按分钟扣点。',
                style: TextStyle(fontSize: 12.5, color: t.dim)),
            const SizedBox(height: 16),
            TextField(
              controller: _server,
              enabled: !_busy,
              style: TextStyle(fontSize: 13, fontFamily: 'monospace', color: t.ink),
              decoration: InputDecoration(
                labelText: '服务器地址',
                hintText: 'http://host:18080',
                errorText: _server.text.isEmpty || _serverOk
                    ? null
                    : '需以 http:// 或 https:// 开头',
              ),
              onChanged: (_) => setState(() {}),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _key,
              autofocus: true,
              enabled: !_busy,
              inputFormatters: [CardKeyFormatter()],
              style: TextStyle(
                  fontSize: 15,
                  letterSpacing: 1.5,
                  fontFamily: 'monospace',
                  color: t.ink),
              decoration: InputDecoration(
                labelText: '卡号',
                hintText: 'XXXXX-XXXXX-XXXXX-XXXXX',
                errorText: _key.text.isEmpty || _keyOk ? null : '卡号长度不足',
              ),
              onChanged: (_) => setState(() {}),
              onSubmitted: (_) => _activate(),
            ),
            if (_error.isNotEmpty) ...[
              const SizedBox(height: 14),
              Container(
                width: double.infinity,
                padding:
                    const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
                decoration: BoxDecoration(
                  color: t.dangerSoft,
                  borderRadius: BorderRadius.circular(8),
                ),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Icon(Icons.error_outline, size: 15, color: t.danger),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(_error,
                          style: TextStyle(fontSize: 12.5, color: t.danger)),
                    ),
                  ],
                ),
              ),
            ],
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: _busy ? null : () => Navigator.of(context).pop(false),
          child: const Text('取消'),
        ),
        FilledButton.icon(
          onPressed: _serverOk && _keyOk && !_busy ? _activate : null,
          icon: _busy
              ? const SizedBox(
                  width: 14,
                  height: 14,
                  child: CircularProgressIndicator(strokeWidth: 2))
              : const Icon(Icons.key, size: 16),
          label: Text(_busy ? '激活中…' : '激活'),
        ),
      ],
    );
  }
}
