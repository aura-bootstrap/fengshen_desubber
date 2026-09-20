import 'package:flutter/material.dart';

import 'api.dart';
import 'pages/audit_page.dart';
import 'pages/cards_page.dart';
import 'pages/tx_page.dart';
import 'theme.dart';

/// 应用版本号(侧栏展示;发版时与 pubspec version 同步)。
const kAppVersion = '1.0.0';

/// 管理版标识(三端统一命名:开发版/用户版/管理版)。
const kAppEdition = '管理版';

void main() {
  runApp(const AdminApp());
}

class AdminApp extends StatelessWidget {
  const AdminApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: '峰神·去字幕(管理版)',
      debugShowCheckedModeBanner: false,
      theme: buildAppTheme(),
      darkTheme: buildAppDarkTheme(),
      home: const LoginPage(),
    );
  }
}

/// 登录页:计费服务器地址 + 管理 token(仿 slicer 登录页卡片式布局)。
class LoginPage extends StatefulWidget {
  const LoginPage({super.key});

  @override
  State<LoginPage> createState() => _LoginPageState();
}

class _LoginPageState extends State<LoginPage> {
  final _server = TextEditingController();
  final _token = TextEditingController();
  bool _busy = false;
  bool _obscure = true;
  String _error = '';

  @override
  void dispose() {
    _server.dispose();
    _token.dispose();
    super.dispose();
  }

  bool get _serverOk {
    final s = _server.text.trim();
    return s.startsWith('http://') || s.startsWith('https://');
  }

  bool get _canSubmit =>
      _serverOk && _token.text.trim().isNotEmpty && !_busy;

  Future<void> _login() async {
    if (!_canSubmit) return;
    setState(() {
      _busy = true;
      _error = '';
    });
    final api = AdminApi(
        _server.text.trim().replaceAll(RegExp(r'/+$'), ''), _token.text.trim());
    try {
      await api.listCards(); // 验证 token 有效
      if (!mounted) return;
      Navigator.of(context).pushReplacement(
          MaterialPageRoute(builder: (_) => AdminShell(api: api)));
    } catch (e) {
      api.dispose();
      setState(() {
        _busy = false;
        _error = '$e';
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    return Scaffold(
      body: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 400),
          child: Card(
            child: Padding(
              padding: const EdgeInsets.all(28),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(children: [
                    ClipRRect(
                      borderRadius: BorderRadius.circular(6),
                      child: Image.asset('assets/logo.png', width: 24, height: 24,
                          errorBuilder: (context, error, stackTrace) => Icon(
                              Icons.admin_panel_settings, size: 24, color: t.primary)),
                    ),
                    const SizedBox(width: 10),
                    Text('峰神·去字幕(管理版)',
                        style: TextStyle(
                            fontSize: 17, fontWeight: FontWeight.w700, color: t.ink)),
                  ]),
                  const SizedBox(height: 6),
                  Text('输入计费服务器地址与管理 token',
                      style: TextStyle(fontSize: 12.5, color: t.dim)),
                  const SizedBox(height: 20),
                  TextField(
                    controller: _server,
                    enabled: !_busy,
                    autofocus: true,
                    style: TextStyle(
                        fontSize: 13, color: t.ink, fontFamily: AppConst.fontMono),
                    decoration: InputDecoration(
                      labelText: '服务器地址',
                      hintText: 'http://host:18080',
                      errorText: _server.text.isEmpty || _serverOk
                          ? null
                          : '需以 http:// 或 https:// 开头',
                    ),
                    onChanged: (_) => setState(() {}),
                  ),
                  const SizedBox(height: 14),
                  TextField(
                    controller: _token,
                    enabled: !_busy,
                    obscureText: _obscure,
                    style: TextStyle(
                        fontSize: 13, color: t.ink, fontFamily: AppConst.fontMono),
                    decoration: InputDecoration(
                      labelText: '管理 token',
                      suffixIcon: IconButton(
                        icon: Icon(
                            _obscure ? Icons.visibility_off : Icons.visibility,
                            size: 17),
                        onPressed: () => setState(() => _obscure = !_obscure),
                      ),
                    ),
                    onChanged: (_) => setState(() {}),
                    onSubmitted: (_) => _login(),
                  ),
                  if (_error.isNotEmpty) ...[
                    const SizedBox(height: 12),
                    Container(
                      width: double.infinity,
                      padding:
                          const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
                      decoration: BoxDecoration(
                        color: t.dangerSoft,
                        borderRadius: BorderRadius.circular(8),
                      ),
                      child: Text(_error,
                          style: TextStyle(fontSize: 12.5, color: t.danger)),
                    ),
                  ],
                  const SizedBox(height: 18),
                  SizedBox(
                    width: double.infinity,
                    child: FilledButton(
                      onPressed: _canSubmit ? _login : null,
                      child: _busy
                          ? const SizedBox(
                              width: 16,
                              height: 16,
                              child: CircularProgressIndicator(strokeWidth: 2))
                          : const Text('登 录'),
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// 主壳:仿 slicer 管理版侧栏(品牌 + 工作区分组导航 + 底部退出登录);
/// ≥900 侧栏双栏,<900 AppBar+Drawer(同款分组)。
class AdminShell extends StatefulWidget {
  final AdminApi api;
  const AdminShell({super.key, required this.api});

  @override
  State<AdminShell> createState() => _AdminShellState();
}

class _AdminShellState extends State<AdminShell> {
  String _page = 'cards';
  final _txCardId = ValueNotifier<int>(0);

  List<_NavItem> get _items => [
        _NavItem('cards', Icons.key, '卡密',
            () => CardsPage(
                api: widget.api,
                onShowTx: (id) {
                  _txCardId.value = id;
                  _onNav('tx');
                })),
        _NavItem('audit', Icons.receipt_long, '审计',
            () => AuditPage(api: widget.api)),
        _NavItem('tx', Icons.payments_outlined, '交易流水',
            () => TxPage(api: widget.api, initialCardId: _txCardId)),
      ];

  void _onNav(String id) {
    setState(() => _page = id);
    // 窄屏从 Drawer 选择后收起抽屉。
    if (MediaQuery.of(context).size.width < 900 && Navigator.canPop(context)) {
      Navigator.pop(context);
    }
  }

  void _logout() {
    widget.api.dispose();
    Navigator.of(context).pushReplacement(
        MaterialPageRoute(builder: (_) => const LoginPage()));
  }

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    final wide = MediaQuery.of(context).size.width >= 900;
    final items = _items;
    final cur = items.firstWhere((i) => i.id == _page, orElse: () => items[0]);
    final body = SafeArea(child: cur.build());
    if (!wide) {
      return Scaffold(
        appBar: AppBar(
          title: Text(cur.label,
              style:
                  const TextStyle(fontSize: 15, fontWeight: FontWeight.w700)),
          actions: [
            IconButton(
              icon: const Icon(Icons.logout, size: 18),
              tooltip: '退出登录',
              onPressed: _logout,
            ),
          ],
        ),
        drawer: Drawer(
          width: 236,
          child: SafeArea(child: _sidebar(t, items, cur.id)),
        ),
        body: body,
      );
    }
    return Scaffold(
      body: Row(
        children: [
          _sidebar(t, items, cur.id),
          Expanded(child: body),
        ],
      ),
    );
  }

  Widget _sidebar(AppTokens t, List<_NavItem> items, String currentId) {
    return Container(
      width: 236,
      decoration: BoxDecoration(
        color: t.surface,
        border: Border(right: BorderSide(color: t.border)),
      ),
      padding: const EdgeInsets.fromLTRB(12, 18, 12, 14),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(10, 2, 10, 16),
            child: Row(
              children: [
                ClipRRect(
                  borderRadius: BorderRadius.circular(10),
                  child: Image.asset(
                    'assets/logo.png',
                    width: 36,
                    height: 36,
                    errorBuilder: (context, error, stackTrace) => Icon(
                      Icons.admin_panel_settings,
                      size: 36,
                      color: t.primary,
                    ),
                  ),
                ),
                const SizedBox(width: 11),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        '峰神·去字幕',
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(
                          fontSize: 15,
                          fontWeight: FontWeight.w700,
                          color: t.ink,
                        ),
                      ),
                      Text(
                        '$kAppEdition v$kAppVersion',
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(fontSize: 11, color: t.faint),
                      ),
                    ],
                  ),
                ),
              ],
            ),
          ),
          Expanded(
            child: ListView(
              padding: EdgeInsets.zero,
              children: [
                const _SectionLabel('工作区'),
                for (final it in items) _navItem(t, it, currentId == it.id),
              ],
            ),
          ),
          Center(
            child: OutlinedButton.icon(
              onPressed: _logout,
              icon: const Icon(Icons.logout, size: 15),
              label: const Text('退出登录'),
            ),
          ),
        ],
      ),
    );
  }

  Widget _navItem(AppTokens t, _NavItem item, bool on) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 1),
      child: InkWell(
        borderRadius: BorderRadius.circular(8),
        onTap: () => _onNav(item.id),
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
          decoration: BoxDecoration(
            color: on ? t.primarySoft : Colors.transparent,
            borderRadius: BorderRadius.circular(8),
            border: on
                ? Border(left: BorderSide(color: t.primary, width: 3))
                : null,
          ),
          child: Row(
            children: [
              Icon(item.icon, size: 17, color: on ? t.primaryInk : t.faint),
              const SizedBox(width: 11),
              Text(
                item.label,
                style: TextStyle(
                  fontSize: 13.5,
                  fontWeight: on ? FontWeight.w600 : FontWeight.w500,
                  color: on ? t.primaryInk : t.dim,
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _NavItem {
  final String id;
  final IconData icon;
  final String label;
  final Widget Function() build;
  const _NavItem(this.id, this.icon, this.label, this.build);
}

/// 侧栏节标题(slicer 管理版同款):主色指示条 + 加粗墨色 + 延伸分隔线。
class _SectionLabel extends StatelessWidget {
  final String text;
  const _SectionLabel(this.text);

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    return Padding(
      padding: const EdgeInsets.fromLTRB(12, 14, 12, 8),
      child: Row(
        children: [
          Container(
            width: 3,
            height: 13,
            decoration: BoxDecoration(
              color: t.primary,
              borderRadius: BorderRadius.circular(2),
            ),
          ),
          const SizedBox(width: 7),
          Text(
            text,
            style: TextStyle(
              fontSize: 12.5,
              fontWeight: FontWeight.w700,
              letterSpacing: 1.5,
              color: t.ink,
            ),
          ),
          const SizedBox(width: 10),
          Expanded(child: Container(height: 1, color: t.border)),
        ],
      ),
    );
  }
}
