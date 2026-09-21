import 'package:flutter/material.dart';

import '../api.dart';
import '../theme.dart';
import '../widgets/top_toast.dart';

/// 修改自己密码(root/admin 皆可):当前 + 新 + 确认。
/// 成功后服务端 bump 口令纪元,当前会话立即失效 → 经 onChanged 回登录页。
class PasswordPage extends StatefulWidget {
  final AdminApi api;
  final VoidCallback onChanged;
  const PasswordPage({super.key, required this.api, required this.onChanged});

  @override
  State<PasswordPage> createState() => _PasswordPageState();
}

class _PasswordPageState extends State<PasswordPage> {
  final _cur = TextEditingController();
  final _next = TextEditingController();
  final _next2 = TextEditingController();
  bool _busy = false;
  bool _obscure = true;
  String _error = '';

  @override
  void dispose() {
    _cur.dispose();
    _next.dispose();
    _next2.dispose();
    super.dispose();
  }

  String? get _nextErr {
    final p = _next.text;
    if (p.isEmpty) return null;
    if (p.length < 8 || p.length > 128) return '密码须 8..128 位';
    return null;
  }

  String? get _next2Err {
    if (_next2.text.isEmpty) return null;
    if (_next2.text != _next.text) return '两次输入不一致';
    return null;
  }

  bool get _valid =>
      _cur.text.isNotEmpty &&
      _next.text.isNotEmpty &&
      _next2.text.isNotEmpty &&
      _nextErr == null &&
      _next2Err == null;

  Future<void> _submit() async {
    if (!_valid || _busy) return;
    setState(() {
      _busy = true;
      _error = '';
    });
    try {
      await widget.api.changePassword(_cur.text, _next.text);
      if (!mounted) return;
      TopToast.show(context, '密码已修改,请用新密码重新登录');
      // 服务端已随改密 bump 口令纪元,当前会话已死,直接回登录页
      widget.onChanged();
      return;
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() => _error = e.forbidden ? '当前密码错误' : e.message);
    } catch (e) {
      if (!mounted) return;
      setState(() => _error = '$e');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    return ListView(
      padding: const EdgeInsets.fromLTRB(24, 20, 24, 24),
      children: [
        Text('修改密码',
            style: TextStyle(
                fontSize: 21, fontWeight: FontWeight.w700, color: t.ink)),
        const SizedBox(height: 6),
        Text('修改成功后当前会话立即失效,需用新密码重新登录。',
            style: TextStyle(fontSize: 12, color: t.dim)),
        const SizedBox(height: 16),
        ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 420),
          child: Card(
            child: Padding(
              padding: const EdgeInsets.all(20),
              child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    TextField(
                      controller: _cur,
                      obscureText: _obscure,
                      autofocus: true,
                      decoration: const InputDecoration(labelText: '当前密码'),
                      onChanged: (_) => setState(() {}),
                    ),
                    const SizedBox(height: 12),
                    TextField(
                      controller: _next,
                      obscureText: _obscure,
                      decoration: InputDecoration(
                          labelText: '新密码',
                          hintText: '≥8 位',
                          errorText: _nextErr),
                      onChanged: (_) => setState(() {}),
                    ),
                    const SizedBox(height: 12),
                    TextField(
                      controller: _next2,
                      obscureText: _obscure,
                      decoration: InputDecoration(
                          labelText: '确认新密码', errorText: _next2Err),
                      onChanged: (_) => setState(() {}),
                      onSubmitted: (_) => _submit(),
                    ),
                    const SizedBox(height: 6),
                    Row(children: [
                      Text('显示密码',
                          style: TextStyle(fontSize: 12, color: t.dim)),
                      Switch(
                          value: !_obscure,
                          onChanged: (v) => setState(() => _obscure = !v)),
                    ]),
                    if (_error.isNotEmpty) ...[
                      const SizedBox(height: 8),
                      Container(
                        width: double.infinity,
                        padding: const EdgeInsets.symmetric(
                            horizontal: 12, vertical: 9),
                        decoration: BoxDecoration(
                            color: t.dangerSoft,
                            borderRadius: BorderRadius.circular(8)),
                        child: Text(_error,
                            style: TextStyle(fontSize: 12.5, color: t.danger)),
                      ),
                    ],
                    const SizedBox(height: 14),
                    FilledButton(
                      onPressed: _valid && !_busy ? _submit : null,
                      child: _busy
                          ? const SizedBox(
                              width: 16,
                              height: 16,
                              child:
                                  CircularProgressIndicator(strokeWidth: 2))
                          : const Text('确认修改'),
                    ),
                  ]),
            ),
          ),
        ),
      ],
    );
  }
}
