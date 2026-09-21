import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../app_state.dart';
import '../responsive.dart';
import '../theme.dart';
import '../widgets/top_toast.dart';

/// 「文本配置」页(config-zone-dual-view T8):AppState.config 的全文 JSON 投影。
/// 与表单页共享唯一数据源双向同步(FR-4);编辑防抖 1s 自动保存(FR-8);
/// 「拷贝」导出最终 JSON,供粘贴到管理端核心参数(FR-7/FR-9)。
class JsonPageView extends StatefulWidget {
  final AppState state;
  const JsonPageView({super.key, required this.state});

  @override
  State<JsonPageView> createState() => _JsonPageViewState();
}

class _JsonPageViewState extends State<JsonPageView> {
  final _editor = TextEditingController();

  AppState get st => widget.state;

  @override
  void initState() {
    super.initState();
    st.addListener(_syncFromState);
    _syncFromState(); // 进页立即投影一次(启动时 config 已加载)
  }

  @override
  void dispose() {
    st.removeListener(_syncFromState);
    _editor.dispose();
    super.dispose();
  }

  /// JSON 编辑器文本重写规则(design §2 光标不变式):
  /// lastOrigin==json(用户正在打字)绝不动控制器;否则文本不一致才重写,光标置文末。
  void _syncFromState() {
    if (st.lastOrigin == EditOrigin.json) return;
    final pretty = st.configPrettyJson;
    if (_editor.text == pretty) return;
    _editor.value = TextEditingValue(
        text: pretty,
        selection: TextSelection.collapsed(offset: pretty.length));
  }

  Future<void> _copy() async {
    await Clipboard.setData(ClipboardData(text: st.configPrettyJson));
    if (mounted) TopToast.show(context, '已拷贝最终 JSON,粘贴到管理版「参数版本」页发布');
  }

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    final err = st.jsonError.isNotEmpty
        ? st.jsonError
        : (st.savePhase == SavePhase.failed ? '保存失败:${st.saveError}' : '');
    return CenteredContent(
      child: ListView(
        padding: const EdgeInsets.fromLTRB(28, 24, 28, 24),
        children: [
          Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Expanded(
              child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text('文本配置',
                        style: TextStyle(
                            fontSize: 21,
                            fontWeight: FontWeight.w700,
                            color: t.ink)),
                    const SizedBox(height: 4),
                    Text('与左侧表单页同一份配置;任一改动防抖 1s 自动保存',
                        style: TextStyle(fontSize: 12.5, color: t.dim)),
                  ]),
            ),
            OutlinedButton.icon(
              onPressed: _copy,
              icon: const Icon(Icons.copy_all, size: 15),
              label: const Text('拷贝'),
            ),
          ]),
          const SizedBox(height: 18),
          if (err.isNotEmpty)
            Container(
              margin: const EdgeInsets.only(bottom: 12),
              padding:
                  const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
              decoration: BoxDecoration(
                  color: t.dangerSoft, borderRadius: BorderRadius.circular(8)),
              child:
                  Text(err, style: TextStyle(fontSize: 12.5, color: t.danger)),
            ),
          Card(
            child: Padding(
              padding: const EdgeInsets.all(12),
              child: TextField(
                controller: _editor,
                maxLines: null,
                minLines: 18,
                style: TextStyle(
                    fontSize: 12.5,
                    fontFamily: AppConst.fontMono,
                    color: t.ink),
                decoration: const InputDecoration(
                  border: InputBorder.none,
                  hintText: '全量配置 JSON;与表单页双向同步',
                ),
                onChanged: st.setConfigJson,
              ),
            ),
          ),
        ],
      ),
    );
  }
}
