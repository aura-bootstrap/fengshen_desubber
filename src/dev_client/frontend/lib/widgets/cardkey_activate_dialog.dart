import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../app_state.dart';
import '../models.dart';
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

/// 卡密激活对话框:输卡号 + 计费服务地址(预填全局配置 online.server,
/// 与用户版差异:开发版无二进制内置地址)。激活成功 pop(true),取消 pop(false)。
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
  final _key = TextEditingController();
  final _server = TextEditingController();
  bool _busy = false;
  String _error = '';

  @override
  void initState() {
    super.initState();
    final v = getPath(widget.state.config, 'online.server');
    if (v is String) _server.text = v;
  }

  @override
  void dispose() {
    _key.dispose();
    _server.dispose();
    super.dispose();
  }

  bool get _keyOk => _key.text.trim().replaceAll('-', '').length >= 10;

  Future<void> _activate() async {
    if (!_keyOk || _busy) return;
    setState(() {
      _busy = true;
      _error = '';
    });
    try {
      await widget.state
          .activateCardKey(_key.text.trim(), server: _server.text.trim());
      if (mounted) Navigator.of(context).pop(true);
    } catch (e) {
      if (mounted) {
        setState(() {
          _busy = false;
          _error = '$e'; // ApiException.toString 已含服务端中文错误文案
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    return AlertDialog(
      title: const Text('激活卡密'),
      content: SizedBox(
        width: 420,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('输入发行方提供的卡号(形如 XXXXX-XXXXX-XXXXX-XXXXX)。激活后本机与该卡绑定,'
                '在线去字幕按分钟扣点。',
                style: TextStyle(fontSize: 12.5, color: t.dim)),
            const SizedBox(height: 16),
            TextField(
              controller: _key,
              autofocus: true,
              enabled: !_busy,
              inputFormatters: [CardKeyFormatter()],
              style: const TextStyle(fontSize: 15, letterSpacing: 1.5),
              decoration: InputDecoration(
                labelText: '卡号',
                hintText: 'XXXXX-XXXXX-XXXXX-XXXXX',
                errorText: _key.text.isEmpty || _keyOk ? null : '卡号长度不足',
              ),
              onChanged: (_) => setState(() {}),
              onSubmitted: (_) => _activate(),
            ),
            const SizedBox(height: 10),
            TextField(
              controller: _server,
              enabled: !_busy,
              decoration: const InputDecoration(
                labelText: '计费服务地址',
                hintText: 'http://127.0.0.1:18099',
              ),
            ),
            if (_error.isNotEmpty) ...[
              const SizedBox(height: 14),
              Align(
                alignment: Alignment.centerLeft,
                child: Text(_error,
                    style: TextStyle(fontSize: 12.5, color: t.danger)),
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
          onPressed: _keyOk && !_busy ? _activate : null,
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
