import 'package:flutter/material.dart';

import '../app_state.dart';
import '../models.dart';
import '../responsive.dart';
import '../theme.dart';
import '../widgets/top_toast.dart';

/// 版本管理页:配置历史(每次自动保存落一版)+ 查看/恢复。
/// 「发布」走文本配置页的「拷贝」→ 粘贴到管理版参数页。
class VersionsPage extends StatefulWidget {
  final AppState state;
  const VersionsPage({super.key, required this.state});

  @override
  State<VersionsPage> createState() => _VersionsPageState();
}

class _VersionsPageState extends State<VersionsPage> {
  List<ConfigHistEntry>? _history;
  int _selected = 0;
  Map<String, dynamic>? _payload;
  bool _busy = false;

  AppState get st => widget.state;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final h = await st.client.configHistory();
      if (!mounted) return;
      setState(() {
        _history = h;
        if (_selected == 0 && h.isNotEmpty) _selected = h.first.version;
      });
      if (_selected != 0) await _loadPayload(_selected);
    } catch (e) {
      if (mounted) TopToast.show(context, '配置历史读取失败:$e', error: true);
    }
  }

  Future<void> _loadPayload(int version) async {
    try {
      final p = await st.client.configHistoryPayload(version);
      if (!mounted) return;
      setState(() => _payload = p);
    } catch (e) {
      if (mounted) TopToast.show(context, '版本内容读取失败:$e', error: true);
    }
  }

  Future<void> _restore() async {
    final p = _payload;
    if (p == null || _busy) return;
    setState(() => _busy = true);
    try {
      await st.replaceConfigAndSave(p);
      if (!mounted) return;
      if (st.savePhase == SavePhase.failed) {
        TopToast.show(context, '恢复失败:${st.saveError}', error: true);
      } else {
        TopToast.show(context, '已恢复版本 $_selected 并落盘为新版本');
        await _load();
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    final history = _history;
    return CenteredContent(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(28, 24, 28, 24),
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Expanded(
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Text('版本管理',
                    style: TextStyle(
                        fontSize: 21, fontWeight: FontWeight.w700, color: t.ink)),
                const SizedBox(height: 4),
                Text('每次配置自动保存落一版,保留最近 50 版;发布到管理版走「文本配置 → 拷贝」',
                    style: TextStyle(fontSize: 12.5, color: t.dim)),
              ]),
            ),
            OutlinedButton.icon(
              onPressed: _payload == null || _busy ? null : _restore,
              icon: const Icon(Icons.restore, size: 15),
              label: Text(_busy ? '恢复中…' : '恢复此版本'),
            ),
          ]),
          const SizedBox(height: 18),
          Expanded(
            child: history == null
                ? const Center(child: CircularProgressIndicator())
                : history.isEmpty
                    ? Card(
                        child: Padding(
                          padding: const EdgeInsets.all(36),
                          child: Center(
                            child: Text('还没有配置历史;改过任意参数自动落第一版',
                                style: TextStyle(fontSize: 13, color: t.dim)),
                          ),
                        ),
                      )
                    : Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
                        SizedBox(
                          width: 240,
                          child: Card(
                            child: ListView.builder(
                              padding: const EdgeInsets.all(8),
                              itemCount: history.length,
                              itemBuilder: (_, i) {
                                final e = history[i];
                                final on = e.version == _selected;
                                return Padding(
                                  padding: const EdgeInsets.symmetric(vertical: 1),
                                  child: InkWell(
                                    borderRadius: BorderRadius.circular(8),
                                    onTap: () {
                                      setState(() {
                                        _selected = e.version;
                                        _payload = null;
                                      });
                                      _loadPayload(e.version);
                                    },
                                    child: Container(
                                      padding: const EdgeInsets.symmetric(
                                          horizontal: 12, vertical: 9),
                                      decoration: BoxDecoration(
                                        color: on ? t.primarySoft : Colors.transparent,
                                        borderRadius: BorderRadius.circular(8),
                                      ),
                                      child: Column(
                                        crossAxisAlignment: CrossAxisAlignment.start,
                                        children: [
                                          Text('v${e.version}',
                                              style: TextStyle(
                                                  fontSize: 13,
                                                  fontWeight: FontWeight.w600,
                                                  color: on ? t.primaryInk : t.ink)),
                                          const SizedBox(height: 2),
                                          Text(e.savedLabel,
                                              style: TextStyle(
                                                  fontSize: 11,
                                                  color: on ? t.primaryInk : t.faint)),
                                        ],
                                      ),
                                    ),
                                  ),
                                );
                              },
                            ),
                          ),
                        ),
                        const SizedBox(width: 12),
                        Expanded(
                          child: Card(
                            child: Padding(
                              padding: const EdgeInsets.all(14),
                              child: _payload == null
                                  ? Center(
                                      child: Text('加载中…',
                                          style: TextStyle(
                                              fontSize: 12.5, color: t.faint)))
                                  : SingleChildScrollView(
                                      child: SelectableText(
                                        st.prettyJsonOf(_payload!),
                                        style: TextStyle(
                                            fontSize: 12,
                                            fontFamily: AppConst.fontMono,
                                            color: t.ink),
                                      ),
                                    ),
                            ),
                          ),
                        ),
                      ]),
          ),
        ]),
      ),
    );
  }
}
