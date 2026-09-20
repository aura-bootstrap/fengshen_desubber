import 'package:flutter/material.dart';

import 'api.dart';
import 'pages/audit_page.dart';
import 'pages/cards_page.dart';
import 'pages/tx_page.dart';

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
      theme: ThemeData(
        colorScheme: ColorScheme.fromSeed(seedColor: const Color(0xFF3B5BFF)),
        useMaterial3: true,
      ),
      home: const LoginPage(),
    );
  }
}

class LoginPage extends StatefulWidget {
  const LoginPage({super.key});

  @override
  State<LoginPage> createState() => _LoginPageState();
}

class _LoginPageState extends State<LoginPage> {
  final _server = TextEditingController();
  final _token = TextEditingController();
  bool _busy = false;
  String _error = '';

  bool get _serverOk {
    final s = _server.text.trim();
    return s.startsWith('http://') || s.startsWith('https://');
  }

  Future<void> _login() async {
    if (!_serverOk || _token.text.trim().isEmpty || _busy) return;
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
    return Scaffold(
      body: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 380),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const Text('峰神·去字幕(管理版)',
                  textAlign: TextAlign.center,
                  style: TextStyle(fontSize: 20, fontWeight: FontWeight.w700)),
              const SizedBox(height: 8),
              Text('输入计费服务器地址与管理 token',
                  textAlign: TextAlign.center,
                  style: TextStyle(fontSize: 12.5, color: Colors.grey[600])),
              const SizedBox(height: 24),
              TextField(
                controller: _server,
                enabled: !_busy,
                style: const TextStyle(fontSize: 13, fontFamily: 'monospace'),
                decoration: InputDecoration(
                  labelText: '服务器地址',
                  hintText: 'http://host:18080',
                  border: const OutlineInputBorder(),
                  errorText: _server.text.isEmpty || _serverOk
                      ? null
                      : '需以 http:// 或 https:// 开头',
                ),
                onChanged: (_) => setState(() {}),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: _token,
                enabled: !_busy,
                obscureText: true,
                style: const TextStyle(fontSize: 13, fontFamily: 'monospace'),
                decoration: const InputDecoration(
                  labelText: '管理 token',
                  border: OutlineInputBorder(),
                ),
                onChanged: (_) => setState(() {}),
                onSubmitted: (_) => _login(),
              ),
              if (_error.isNotEmpty) ...[
                const SizedBox(height: 12),
                Text(_error,
                    style: TextStyle(
                        fontSize: 12.5, color: Theme.of(context).colorScheme.error)),
              ],
              const SizedBox(height: 20),
              FilledButton(
                onPressed:
                    _serverOk && _token.text.trim().isNotEmpty && !_busy ? _login : null,
                child: Text(_busy ? '验证中…' : '登录'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class AdminShell extends StatefulWidget {
  final AdminApi api;
  const AdminShell({super.key, required this.api});

  @override
  State<AdminShell> createState() => _AdminShellState();
}

class _AdminShellState extends State<AdminShell> {
  int _index = 0;
  final _txCardId = ValueNotifier<int>(0);

  @override
  Widget build(BuildContext context) {
    final pages = [
      CardsPage(
          api: widget.api,
          onShowTx: (id) {
            _txCardId.value = id;
            setState(() => _index = 2);
          }),
      AuditPage(api: widget.api),
      TxPage(api: widget.api, initialCardId: _txCardId),
    ];
    return Scaffold(
      appBar: AppBar(title: const Text('峰神·去字幕(管理版)')),
      body: Row(
        children: [
          NavigationRail(
            selectedIndex: _index,
            onDestinationSelected: (i) => setState(() => _index = i),
            labelType: NavigationRailLabelType.all,
            destinations: const [
              NavigationRailDestination(
                  icon: Icon(Icons.key_outlined), label: Text('卡密')),
              NavigationRailDestination(
                  icon: Icon(Icons.fact_check_outlined), label: Text('审计')),
              NavigationRailDestination(
                  icon: Icon(Icons.receipt_long_outlined), label: Text('交易流水')),
            ],
          ),
          const VerticalDivider(width: 1),
          Expanded(child: pages[_index]),
        ],
      ),
    );
  }
}
