import 'package:flutter/material.dart';

import '../app_state.dart';
import '../models.dart';
import '../theme.dart';

/// 云端账号登录对话框:账号 + 密码 + 云端服务地址(预填全局配置 online.server)。
/// 开发版内部通道,登录成功才落盘凭据;成功 pop(true),取消 pop(false)。
Future<bool?> showCloudLoginDialog(BuildContext context, AppState state) {
  return showDialog<bool>(
    context: context,
    barrierDismissible: false,
    builder: (_) => _CloudLoginDialog(state: state),
  );
}

class _CloudLoginDialog extends StatefulWidget {
  final AppState state;
  const _CloudLoginDialog({required this.state});

  @override
  State<_CloudLoginDialog> createState() => _CloudLoginDialogState();
}

class _CloudLoginDialogState extends State<_CloudLoginDialog> {
  final _username = TextEditingController();
  final _password = TextEditingController();
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
    _username.dispose();
    _password.dispose();
    _server.dispose();
    super.dispose();
  }

  bool get _inputOk =>
      _username.text.trim().isNotEmpty && _password.text.isNotEmpty;

  Future<void> _login() async {
    if (!_inputOk || _busy) return;
    setState(() {
      _busy = true;
      _error = '';
    });
    try {
      await widget.state.loginCloud(
        _username.text.trim(),
        _password.text,
        server: _server.text.trim(),
      );
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
      title: const Text('登录云端账号'),
      content: SizedBox(
        width: 420,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('输入云端服务的管理员账号。开发版走内部通道,在线去字幕不计点数。',
                style: TextStyle(fontSize: 12.5, color: t.dim)),
            const SizedBox(height: 16),
            TextField(
              controller: _username,
              autofocus: true,
              enabled: !_busy,
              decoration: const InputDecoration(labelText: '账号'),
              onChanged: (_) => setState(() {}),
              onSubmitted: (_) => _login(),
            ),
            const SizedBox(height: 10),
            TextField(
              controller: _password,
              enabled: !_busy,
              obscureText: true,
              decoration: const InputDecoration(labelText: '密码'),
              onChanged: (_) => setState(() {}),
              onSubmitted: (_) => _login(),
            ),
            const SizedBox(height: 10),
            TextField(
              controller: _server,
              enabled: !_busy,
              decoration: const InputDecoration(
                labelText: '云端服务地址',
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
          onPressed: _inputOk && !_busy ? _login : null,
          icon: _busy
              ? const SizedBox(
                  width: 14,
                  height: 14,
                  child: CircularProgressIndicator(strokeWidth: 2))
              : const Icon(Icons.login, size: 16),
          label: Text(_busy ? '登录中…' : '登录'),
        ),
      ],
    );
  }
}
