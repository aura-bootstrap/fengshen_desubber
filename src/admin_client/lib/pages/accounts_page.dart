import 'package:flutter/material.dart';

import '../api.dart';
import '../theme.dart';
import '../widgets/top_toast.dart';
import 'cards_page.dart' show fmtTs;

/// 账号管理页(仅 root):列管理员账号、建号、禁用/启用、重置密码、删除。
/// 账号记录带乐观锁 version:提交时回传,若已被他人改 → 409,自动刷新并提示重试。
/// root 自身行不可操作(改自己密码走「修改密码」页,避免自锁;服务端同样拒改 root)。
class AccountsPage extends StatefulWidget {
  final AdminApi api;
  const AccountsPage({super.key, required this.api});

  @override
  State<AccountsPage> createState() => _AccountsPageState();
}

class _AccountsPageState extends State<AccountsPage> {
  List<AccountInfo> _users = [];
  bool _loading = true;
  bool _busy = false;
  String _error = '';

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = '';
    });
    try {
      final us = await widget.api.listAccounts();
      if (!mounted) return;
      setState(() => _users = us);
    } on ApiException catch (e) {
      if (mounted) setState(() => _error = e.message);
    } catch (e) {
      if (mounted) setState(() => _error = '$e');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _create() async {
    final res = await showDialog<({String user, String pass})>(
      context: context,
      builder: (_) => const _CreateAccountDialog(),
    );
    if (res == null || !mounted) return;
    setState(() => _busy = true);
    try {
      await widget.api.createAccount(res.user, res.pass);
      if (!mounted) return;
      TopToast.show(context, '账号 ${res.user} 已创建');
      await _load();
    } on ApiException catch (e) {
      if (mounted) {
        TopToast.show(context, '创建失败 ${e.message}', error: true);
      }
    } catch (e) {
      if (mounted) TopToast.show(context, '创建失败 $e', error: true);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _toggleStatus(AccountInfo u) async {
    final disabling = !u.disabled;
    final label = disabling ? '禁用' : '启用';
    final ok = await showDialog<bool>(
      context: context,
      builder: (_) => _ConfirmDialog(
        title: '$label账号',
        message: disabling
            ? '将禁用账号 ${u.username}。禁用后其会话立即失效、无法登录。'
            : '将启用账号 ${u.username}。启用后可正常登录管业务。',
        confirmLabel: '确认$label',
        danger: disabling,
      ),
    );
    if (ok != true || !mounted) return;
    await _userOp(
        () => widget.api.setAccountStatus(
            u.username, disabling ? 'disabled' : 'active', u.version),
        label);
  }

  Future<void> _resetPw(AccountInfo u) async {
    final pass = await showDialog<String>(
      context: context,
      builder: (_) => _ResetPasswordDialog(username: u.username),
    );
    if (pass == null || !mounted) return;
    await _userOp(
        () => widget.api.resetAccountPassword(u.username, pass, u.version),
        '重置');
  }

  Future<void> _delete(AccountInfo u) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (_) => _ConfirmDialog(
        title: '删除账号',
        message: '将永久删除账号 ${u.username}。删除后其会话立即失效、无法登录;'
            '该操作会写入审计且不可恢复。',
        confirmLabel: '确认删除',
        danger: true,
      ),
    );
    if (ok != true || !mounted) return;
    await _userOp(() => widget.api.deleteAccount(u.username, u.version), '删除');
  }

  /// 账号写操作统一处理:乐观锁冲突 → 刷新 + 提示重试;其余错误 → toast。
  Future<void> _userOp(Future<void> Function() op, String label) async {
    setState(() => _busy = true);
    try {
      await op();
      if (!mounted) return;
      TopToast.show(context, '$label成功');
      await _load();
    } on ApiException catch (e) {
      if (!mounted) return;
      if (e.conflict) {
        TopToast.show(context, '账号不存在或已被他人修改,已刷新,请重试', error: true);
        await _load();
      } else {
        TopToast.show(context, '$label失败 ${e.message}', error: true);
      }
    } catch (e) {
      if (mounted) TopToast.show(context, '$label失败 $e', error: true);
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
        Row(children: [
          Text('账号管理',
              style: TextStyle(
                  fontSize: 21, fontWeight: FontWeight.w700, color: t.ink)),
          const Spacer(),
          OutlinedButton.icon(
            onPressed: _loading ? null : _load,
            icon: const Icon(Icons.refresh, size: 15),
            label: const Text('刷新'),
          ),
          const SizedBox(width: 10),
          FilledButton.icon(
            onPressed: _busy ? null : _create,
            icon: const Icon(Icons.person_add_alt, size: 16),
            label: const Text('新建管理员'),
          ),
        ]),
        const SizedBox(height: 6),
        Text('root 只管账号:创建 admin、禁用/启用、重置密码、删除。业务数据 admin 账号即可管理。',
            style: TextStyle(fontSize: 12, color: t.dim)),
        const SizedBox(height: 14),
        if (_error.isNotEmpty)
          Container(
            margin: const EdgeInsets.only(bottom: 12),
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
            decoration: BoxDecoration(
                color: t.dangerSoft, borderRadius: BorderRadius.circular(8)),
            child: Text(_error,
                style: TextStyle(fontSize: 12.5, color: t.danger)),
          ),
        if (_loading && _users.isEmpty)
          const Padding(
              padding: EdgeInsets.all(40),
              child: Center(child: CircularProgressIndicator()))
        else if (_users.isEmpty)
          Card(
            child: Padding(
              padding: const EdgeInsets.symmetric(vertical: 48),
              child: Center(
                  child: Text('暂无账号',
                      style: TextStyle(fontSize: 13, color: t.faint))),
            ),
          )
        else ...[
          _userHeader(t),
          for (final u in _users) _userRow(t, u),
        ],
      ],
    );
  }

  /// 账号表列宽:_userHeader 与 _userRow 共用,保证表头与每行逐列对齐。
  static const double _colUser = 160;
  static const double _colRole = 96;
  static const double _colStatus = 80;
  static const double _colVersion = 44;
  static const double _colOps = 236;

  Widget _userHeader(AppTokens t) => DefaultTextStyle.merge(
        style: TextStyle(
          fontSize: 11.5,
          fontWeight: FontWeight.w600,
          fontFamily: 'Consolas',
          color: t.faint,
        ),
        child: const Padding(
          padding: EdgeInsets.fromLTRB(16, 0, 16, 5),
          child: Row(children: [
            SizedBox(
                width: _colUser,
                child: Text('账号', textAlign: TextAlign.center)),
            SizedBox(width: 8),
            SizedBox(
                width: _colRole,
                child: Text('角色', textAlign: TextAlign.center)),
            SizedBox(width: 8),
            SizedBox(
                width: _colStatus,
                child: Text('状态', textAlign: TextAlign.center)),
            SizedBox(width: 8),
            Expanded(child: Text('创建时间', textAlign: TextAlign.center)),
            SizedBox(width: 8),
            SizedBox(
                width: _colVersion,
                child: Text('版本', textAlign: TextAlign.center)),
            SizedBox(width: 8),
            SizedBox(
                width: _colOps, child: Text('操作', textAlign: TextAlign.center)),
          ]),
        ),
      );

  Widget _userRow(AppTokens t, AccountInfo u) {
    final isRoot = u.isRoot;
    final (roleLabel, fg, bg) = isRoot
        ? ('超级管理员', t.primaryInk, t.primarySoft)
        : ('管理员', t.dim, t.bg);
    return Card(
      margin: const EdgeInsets.only(bottom: 10),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 13),
        child: Row(children: [
          SizedBox(
            width: _colUser,
            child: Text(u.username,
                textAlign: TextAlign.center,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(
                    fontSize: 13,
                    fontFamily: AppConst.fontMono,
                    color: t.ink)),
          ),
          const SizedBox(width: 8),
          SizedBox(
              width: _colRole, child: Center(child: _badge(roleLabel, fg, bg))),
          const SizedBox(width: 8),
          SizedBox(
              width: _colStatus,
              child: Center(
                  child: _badge(
                      u.disabled ? '已禁用' : '启用中',
                      u.disabled ? t.danger : t.success,
                      u.disabled ? t.dangerSoft : t.successSoft))),
          const SizedBox(width: 8),
          Expanded(
            child: Center(
              child: Text(fmtTs(u.createdAt),
                  style: TextStyle(fontSize: 12, color: t.faint),
                  overflow: TextOverflow.ellipsis),
            ),
          ),
          const SizedBox(width: 8),
          SizedBox(
              width: _colVersion,
              child: Center(
                  child: Text('v${u.version}',
                      style: TextStyle(fontSize: 12, color: t.faint)))),
          const SizedBox(width: 8),
          SizedBox(
            width: _colOps,
            child: isRoot
                ? Center(
                    child: Text('本人·不可操作',
                        style: TextStyle(fontSize: 12, color: t.faint)))
                : Row(
                    mainAxisAlignment: MainAxisAlignment.center,
                    children: [
                      OutlinedButton(
                        onPressed: _busy ? null : () => _resetPw(u),
                        child: const Text('重置密码'),
                      ),
                      const SizedBox(width: 8),
                      OutlinedButton(
                        onPressed: _busy ? null : () => _toggleStatus(u),
                        style: OutlinedButton.styleFrom(
                            foregroundColor:
                                u.disabled ? t.success : t.danger),
                        child: Text(u.disabled ? '启用' : '禁用'),
                      ),
                      const SizedBox(width: 8),
                      OutlinedButton(
                        onPressed: _busy ? null : () => _delete(u),
                        style: OutlinedButton.styleFrom(
                            foregroundColor: t.danger),
                        child: const Text('删除'),
                      ),
                    ],
                  ),
          ),
        ]),
      ),
    );
  }

  Widget _badge(String text, Color fg, Color bg) => Container(
        padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 3),
        decoration:
            BoxDecoration(color: bg, borderRadius: BorderRadius.circular(99)),
        child: Text(text,
            style:
                TextStyle(fontSize: 11, fontWeight: FontWeight.w700, color: fg)),
      );
}

/// 通用二次确认框。
class _ConfirmDialog extends StatelessWidget {
  final String title;
  final String message;
  final String confirmLabel;
  final bool danger;
  const _ConfirmDialog({
    required this.title,
    required this.message,
    required this.confirmLabel,
    this.danger = false,
  });

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    return AlertDialog(
      backgroundColor: t.surface,
      title: Text(title, style: TextStyle(fontSize: 16, color: t.ink)),
      content: SizedBox(
        width: 400,
        child: Text(message, style: TextStyle(fontSize: 12.5, color: t.dim)),
      ),
      actions: [
        TextButton(
            onPressed: () => Navigator.of(context).pop(),
            child: const Text('取消')),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(true),
          style:
              danger ? FilledButton.styleFrom(backgroundColor: t.danger) : null,
          child: Text(confirmLabel),
        ),
      ],
    );
  }
}

/// 新建 admin 账号:用户名 + 初始密码 + 确认(本地校验镜像服务端规则)。
class _CreateAccountDialog extends StatefulWidget {
  const _CreateAccountDialog();

  @override
  State<_CreateAccountDialog> createState() => _CreateAccountDialogState();
}

class _CreateAccountDialogState extends State<_CreateAccountDialog> {
  static final _userOk = RegExp(r'^[a-zA-Z0-9_.-]{3,32}$');
  final _user = TextEditingController();
  final _pass = TextEditingController();
  final _pass2 = TextEditingController();

  @override
  void dispose() {
    _user.dispose();
    _pass.dispose();
    _pass2.dispose();
    super.dispose();
  }

  String? get _userErr {
    final u = _user.text.trim();
    if (u.isEmpty) return null;
    if (u == 'root') return '不能使用保留名 root';
    if (!_userOk.hasMatch(u)) return '3..32 位,仅字母/数字/._-';
    return null;
  }

  String? get _passErr {
    final p = _pass.text;
    if (p.isEmpty) return null;
    if (p.length < 8 || p.length > 128) return '密码须 8..128 位';
    return null;
  }

  String? get _pass2Err {
    if (_pass2.text.isEmpty) return null;
    if (_pass2.text != _pass.text) return '两次输入不一致';
    return null;
  }

  bool get _valid =>
      _user.text.trim().isNotEmpty &&
      _pass.text.isNotEmpty &&
      _pass2.text.isNotEmpty &&
      _userErr == null &&
      _passErr == null &&
      _pass2Err == null;

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    return AlertDialog(
      backgroundColor: t.surface,
      title: Text('新建管理员账号', style: TextStyle(fontSize: 16, color: t.ink)),
      content: SizedBox(
        width: 420,
        child: Column(mainAxisSize: MainAxisSize.min, children: [
          TextField(
            controller: _user,
            autofocus: true,
            decoration: InputDecoration(
                labelText: '用户名',
                hintText: '3..32 位,字母/数字/._-',
                errorText: _userErr),
            onChanged: (_) => setState(() {}),
          ),
          const SizedBox(height: 12),
          TextField(
            controller: _pass,
            obscureText: true,
            decoration: InputDecoration(
                labelText: '初始密码', hintText: '≥8 位', errorText: _passErr),
            onChanged: (_) => setState(() {}),
          ),
          const SizedBox(height: 12),
          TextField(
            controller: _pass2,
            obscureText: true,
            decoration:
                InputDecoration(labelText: '确认密码', errorText: _pass2Err),
            onChanged: (_) => setState(() {}),
          ),
          const SizedBox(height: 10),
          Text('新账号角色为 admin(只管业务,不能开账号);请让其首次登录后自行改密。',
              style: TextStyle(fontSize: 11.5, color: t.dim)),
        ]),
      ),
      actions: [
        TextButton(
            onPressed: () => Navigator.of(context).pop(),
            child: const Text('取消')),
        FilledButton(
          onPressed: _valid
              ? () => Navigator.of(context)
                  .pop((user: _user.text.trim(), pass: _pass.text))
              : null,
          child: const Text('创建'),
        ),
      ],
    );
  }
}

/// 重置他人密码:新密码 + 确认。
class _ResetPasswordDialog extends StatefulWidget {
  final String username;
  const _ResetPasswordDialog({required this.username});

  @override
  State<_ResetPasswordDialog> createState() => _ResetPasswordDialogState();
}

class _ResetPasswordDialogState extends State<_ResetPasswordDialog> {
  final _pass = TextEditingController();
  final _pass2 = TextEditingController();

  @override
  void dispose() {
    _pass.dispose();
    _pass2.dispose();
    super.dispose();
  }

  String? get _passErr {
    final p = _pass.text;
    if (p.isEmpty) return null;
    if (p.length < 8 || p.length > 128) return '密码须 8..128 位';
    return null;
  }

  String? get _pass2Err {
    if (_pass2.text.isEmpty) return null;
    if (_pass2.text != _pass.text) return '两次输入不一致';
    return null;
  }

  bool get _valid =>
      _pass.text.isNotEmpty &&
      _pass2.text.isNotEmpty &&
      _passErr == null &&
      _pass2Err == null;

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    return AlertDialog(
      backgroundColor: t.surface,
      title: Text('重置 ${widget.username} 的密码',
          style: TextStyle(fontSize: 16, color: t.ink)),
      content: SizedBox(
        width: 420,
        child: Column(mainAxisSize: MainAxisSize.min, children: [
          TextField(
            controller: _pass,
            obscureText: true,
            autofocus: true,
            decoration: InputDecoration(
                labelText: '新密码', hintText: '≥8 位', errorText: _passErr),
            onChanged: (_) => setState(() {}),
          ),
          const SizedBox(height: 12),
          TextField(
            controller: _pass2,
            obscureText: true,
            decoration:
                InputDecoration(labelText: '确认新密码', errorText: _pass2Err),
            onChanged: (_) => setState(() {}),
          ),
          const SizedBox(height: 10),
          Text('重置后该管理员的现有会话立即失效,需用新密码重新登录。',
              style: TextStyle(fontSize: 11.5, color: t.dim)),
        ]),
      ),
      actions: [
        TextButton(
            onPressed: () => Navigator.of(context).pop(),
            child: const Text('取消')),
        FilledButton(
          onPressed:
              _valid ? () => Navigator.of(context).pop(_pass.text) : null,
          child: const Text('确认重置'),
        ),
      ],
    );
  }
}
