import 'package:file_selector/file_selector.dart';
import 'package:flutter/material.dart';

import '../app_state.dart';
import '../theme.dart';
import '../widgets/cardkey_activate_dialog.dart';
import '../widgets/top_toast.dart';

/// 取路径的目录部分(正反斜杠均可);无分隔符时返回 null。
String? dirnameOf(String path) {
  final t = path.replaceAll('/', '\\');
  final i = t.lastIndexOf('\\');
  if (i <= 0) return null;
  final dir = t.substring(0, i);
  return dir.endsWith(':') ? '$dir\\' : dir;
}

/// 新建任务向导(对齐 slicer 分步创建):
/// ① 选择视频 ② 选择输出地址 ③ 选择引擎 ④ 确认开始。
/// 技术参数不暴露:crf 固定 15,grain/ocr/sam2/face_restore 底层固定全开
/// (对齐 slice1-pp-grain tag 参数);生成式三档固定 force_engine=propainter。
Future<void> showNewTaskDialog(BuildContext context, AppState state) {
  return showDialog(
    context: context,
    builder: (ctx) => NewTaskDialog(state: state),
  );
}

class NewTaskDialog extends StatefulWidget {
  final AppState state;
  const NewTaskDialog({super.key, required this.state});

  @override
  State<NewTaskDialog> createState() => _NewTaskDialogState();
}

class _NewTaskDialogState extends State<NewTaskDialog> {
  static const _stepNames = ['选择视频', '选择输出地址', '选择引擎', '确认开始'];

  int _step = 0;
  String? srcPath;
  final outDirCtrl = TextEditingController();
  final outNameCtrl = TextEditingController();
  bool busy = false;

  // 修复引擎六选一(创建时固化进任务快照),技术旗标见文件头注释。
  String engine = 'diffueraser';
  static const _engines = [
    ('diffueraser', Icons.auto_awesome, 'DiffuEraser 扩散', '本地 · 最佳画质,慢'),
    ('propainter', Icons.brush, 'ProPainter', '本地 · 画质与速度均衡'),
    ('temporal', Icons.bolt, '时域迁移', '本地 · 最快'),
    ('delogo', Icons.blur_on, '空间修补', '本地 · 单帧修补'),
    ('wanvace', Icons.science, 'Wan-VACE 视频扩散', '本地 · 实验性,很慢'),
    ('online', Icons.cloud, '在线去字幕', '云端 · 按分钟扣点'),
  ];

  @override
  void dispose() {
    outDirCtrl.dispose();
    outNameCtrl.dispose();
    super.dispose();
  }

  /// 视频文件名主干(去目录、去扩展名)。
  String get _baseName {
    final p = srcPath;
    if (p == null) return '';
    final base = p.split(RegExp(r'[\\/]')).last;
    final dot = base.lastIndexOf('.');
    return dot > 0 ? base.substring(0, dot) : base;
  }

  String get _engineLabel => _engines.firstWhere((e) => e.$1 == engine).$3;

  bool get _canNext => switch (_step) {
    0 => srcPath != null,
    1 =>
      outDirCtrl.text.trim().isNotEmpty && outNameCtrl.text.trim().isNotEmpty,
    2 => true,
    _ => false,
  };

  void _next() {
    if (!_canNext) return;
    if (_step + 1 == 2 && engine == 'online') {
      widget.state.refreshMachineAccount();
    }
    setState(() => _step++);
  }

  Future<void> pickVideo() async {
    const group = XTypeGroup(
      label: '视频',
      extensions: ['mp4', 'mkv', 'mov', 'avi'],
    );
    final file = await openFile(acceptedTypeGroups: [group]);
    if (file == null) return;
    setState(() {
      srcPath = file.path;
      // 输出默认到视频目录下的「去字幕」子目录,文件名 <原名>_fixed.mp4;手改过则保留。
      if (outDirCtrl.text.isEmpty) {
        final dir = dirnameOf(file.path) ?? '';
        outDirCtrl.text = dir.endsWith('\\') ? '$dir去字幕' : '$dir\\去字幕';
      }
      if (outNameCtrl.text.isEmpty) {
        final dot = file.name.lastIndexOf('.');
        final base = dot > 0 ? file.name.substring(0, dot) : file.name;
        outNameCtrl.text = '${base}_fixed.mp4';
      }
    });
  }

  /// 按引擎选择生成任务参数快照(六选一互斥);crf 固定 15 不暴露。
  Map<String, dynamic> engineParams() {
    final p = <String, dynamic>{
      'grain': true,
      'ocr': true,
      'crf': 15,
      'sam2': true,
      'face_restore': true,
      'propainter': true,
      'force_engine': 'propainter',
      'diffueraser': false,
      'out_dir': outDirCtrl.text.trim(),
    };
    switch (engine) {
      case 'temporal':
        // 纯时域迁移:不开生成式旁车,强制全部事件走 motion 档
        // (否则路由把 T3+ 事件分进 painter 档而无旁车可用)。
        p['engine'] = 'temporal';
        p['propainter'] = false;
        p['force_engine'] = 'motion';
      case 'delogo':
        p['engine'] = 'delogo';
        p['propainter'] = false;
        p['force_engine'] = '';
      case 'propainter':
        break; // 生成式默认值即 ProPainter
      case 'wanvace':
        p['wanvace'] = true;
      case 'online':
        // 在线引擎:本地旗标无意义,runner 见 online 直接走云端管线。
        p['online'] = true;
      default: // diffueraser
        p['diffueraser'] = true;
    }
    return p;
  }

  Future<void> submit(bool runNow) async {
    setState(() => busy = true);
    try {
      await widget.state.client.createTask(
        name: _baseName,
        srcPath: srcPath!,
        outName: outNameCtrl.text.trim(),
        params: engineParams(),
        runNow: runNow,
      );
      await widget.state.refresh();
      if (mounted) {
        TopToast.show(context, '任务「$_baseName」已创建');
        Navigator.of(context).pop();
      }
    } catch (e) {
      if (mounted) {
        setState(() => busy = false);
        TopToast.show(context, '$e', error: true);
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    return AlertDialog(
      backgroundColor: t.surface,
      titlePadding: const EdgeInsets.fromLTRB(24, 18, 24, 0),
      title: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Text(
                '新建去字幕任务',
                style: TextStyle(
                  fontSize: 16,
                  fontWeight: FontWeight.w700,
                  color: t.ink,
                ),
              ),
              const SizedBox(width: 10),
              Text(
                '第 ${_step + 1} 步 / 共 4 步 · ${_stepNames[_step]}',
                style: TextStyle(fontSize: 12, color: t.faint),
              ),
            ],
          ),
          const SizedBox(height: 12),
          // 分段进度指示(对齐 slicer 向导)
          Row(
            children: [
              for (var i = 0; i < 4; i++) ...[
                Expanded(
                  child: Container(
                    height: 4,
                    decoration: BoxDecoration(
                      color: i <= _step ? t.primary : t.border,
                      borderRadius: BorderRadius.circular(2),
                    ),
                  ),
                ),
                if (i < 3) const SizedBox(width: 6),
              ],
            ],
          ),
        ],
      ),
      content: SizedBox(
        width: 560,
        height: 340,
        child: switch (_step) {
          0 => _stepVideo(t),
          1 => _stepOutput(t),
          2 => _stepEngine(t),
          _ => _stepConfirm(t),
        },
      ),
      actionsPadding: const EdgeInsets.fromLTRB(24, 8, 24, 16),
      // 「取消居左,操作居右」(对齐 slicer 向导,OverflowBar 不能放 Spacer)。
      actionsAlignment: MainAxisAlignment.spaceBetween,
      actions: [
        TextButton(
          onPressed: busy ? null : () => Navigator.of(context).pop(),
          child: const Text('取消'),
        ),
        Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (_step > 0) ...[
              OutlinedButton(
                onPressed: busy ? null : () => setState(() => _step--),
                child: const Text('上一步'),
              ),
              const SizedBox(width: 10),
            ],
            if (_step < 3)
              FilledButton(
                onPressed: _canNext && !busy ? _next : null,
                child: const Text('下一步'),
              )
            else ...[
              OutlinedButton(
                onPressed: (busy || _onlineBlocked)
                    ? null
                    : () => submit(false),
                child: const Text('暂不运行,仅创建'),
              ),
              const SizedBox(width: 10),
              FilledButton.icon(
                onPressed: (busy || _onlineBlocked) ? null : () => submit(true),
                icon: const Icon(Icons.play_arrow, size: 17),
                label: Text(busy ? '创建中…' : '立即开始运行'),
              ),
            ],
          ],
        ),
      ],
    );
  }

  // ---- ① 选择视频 ----
  Widget _stepVideo(AppTokens t) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          '选择要去字幕的视频文件(mp4 / mkv / mov / avi),原文件不会被修改。',
          style: TextStyle(fontSize: 12.5, color: t.dim),
        ),
        const SizedBox(height: 14),
        InkWell(
          borderRadius: BorderRadius.circular(10),
          onTap: busy ? null : pickVideo,
          child: Container(
            width: double.infinity,
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 22),
            decoration: BoxDecoration(
              color: srcPath != null ? t.primarySoft : t.bg,
              border: Border.all(
                color: srcPath != null ? t.primary : t.border,
                width: srcPath != null ? 1.5 : 1,
              ),
              borderRadius: BorderRadius.circular(10),
            ),
            child: Row(
              children: [
                Icon(
                  Icons.video_file,
                  size: 26,
                  color: srcPath != null ? t.primaryInk : t.faint,
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        srcPath == null ? '点击选择视频' : _baseName,
                        style: TextStyle(
                          fontSize: 13.5,
                          fontWeight: FontWeight.w700,
                          color: srcPath != null ? t.primaryInk : t.ink,
                        ),
                      ),
                      Text(
                        srcPath ?? '支持 mp4 / mkv / mov / avi',
                        style: TextStyle(fontSize: 11, color: t.faint),
                        overflow: TextOverflow.ellipsis,
                      ),
                    ],
                  ),
                ),
                if (srcPath != null)
                  Icon(Icons.check_circle, size: 18, color: t.primary),
              ],
            ),
          ),
        ),
      ],
    );
  }

  // ---- ② 选择输出地址 ----
  Widget _stepOutput(AppTokens t) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          '处理完成的视频保存到哪里。默认保存到源视频目录下的「去字幕」文件夹。',
          style: TextStyle(fontSize: 12.5, color: t.dim),
        ),
        const SizedBox(height: 14),
        Row(
          children: [
            Expanded(
              child: TextField(
                controller: outDirCtrl,
                style: TextStyle(fontSize: 13, color: t.ink),
                decoration: const InputDecoration(labelText: '输出目录'),
                onChanged: (_) => setState(() {}),
              ),
            ),
            const SizedBox(width: 10),
            OutlinedButton.icon(
              onPressed: busy
                  ? null
                  : () async {
                      final dir = await getDirectoryPath(
                        confirmButtonText: '选择输出目录',
                      );
                      if (dir != null) setState(() => outDirCtrl.text = dir);
                    },
              icon: const Icon(Icons.folder_open, size: 15),
              label: const Text('浏览'),
            ),
          ],
        ),
        const SizedBox(height: 14),
        TextField(
          controller: outNameCtrl,
          style: TextStyle(fontSize: 13, color: t.ink),
          decoration: const InputDecoration(labelText: '输出文件名'),
          onChanged: (_) => setState(() {}),
        ),
      ],
    );
  }

  // ---- ③ 选择引擎 ----
  Widget _stepEngine(AppTokens t) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          '选择去字幕方式,拿不准就保持默认。',
          style: TextStyle(fontSize: 12.5, color: t.dim),
        ),
        const SizedBox(height: 12),
        Expanded(
          child: ListView.separated(
            itemCount: _engines.length,
            separatorBuilder: (_, _) => const SizedBox(height: 8),
            itemBuilder: (_, i) {
              final (value, icon, title, desc) = _engines[i];
              final on = engine == value;
              return InkWell(
                borderRadius: BorderRadius.circular(10),
                onTap: busy
                    ? null
                    : () {
                        setState(() => engine = value);
                        // 切到在线引擎时主动拉取机器账户权威余额。
                        if (value == 'online') {
                          widget.state.refreshMachineAccount();
                        }
                      },
                child: Container(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 14,
                    vertical: 10,
                  ),
                  decoration: BoxDecoration(
                    color: on ? t.primarySoft : t.bg,
                    border: Border.all(
                      color: on ? t.primary : t.border,
                      width: on ? 1.5 : 1,
                    ),
                    borderRadius: BorderRadius.circular(10),
                  ),
                  child: Row(
                    children: [
                      Icon(icon, size: 20, color: on ? t.primaryInk : t.faint),
                      const SizedBox(width: 10),
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              title,
                              style: TextStyle(
                                fontSize: 13,
                                fontWeight: FontWeight.w700,
                                color: on ? t.primaryInk : t.ink,
                              ),
                            ),
                            Text(
                              desc,
                              style: TextStyle(fontSize: 11, color: t.faint),
                            ),
                          ],
                        ),
                      ),
                      if (on)
                        Icon(Icons.check_circle, size: 16, color: t.primary),
                    ],
                  ),
                ),
              );
            },
          ),
        ),
        // 仅在线引擎展示卡密状态区(激活/余额/换卡/解绑)。
        if (engine == 'online') ...[
          const SizedBox(height: 8),
          _machineAccountSection(t),
        ],
      ],
    );
  }

  // ---- ④ 确认开始 ----
  Widget _stepConfirm(AppTokens t) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text('确认任务信息,选择创建方式:', style: TextStyle(fontSize: 12.5, color: t.dim)),
        const SizedBox(height: 14),
        _summaryRow(t, '视频', srcPath ?? ''),
        _summaryRow(t, '输出目录', outDirCtrl.text.trim()),
        _summaryRow(t, '输出文件', outNameCtrl.text.trim()),
        _summaryRow(t, '去字幕方式', _engineLabel),
        const Spacer(),
        Text(
          '「立即开始运行」将创建任务并马上开跑(引擎忙则自动排队);「仅创建」加入任务列表稍后再跑。',
          style: TextStyle(fontSize: 11.5, color: t.faint),
        ),
      ],
    );
  }

  Widget _summaryRow(AppTokens t, String label, String value) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 4),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 76,
            child: Text(
              label,
              style: TextStyle(
                fontSize: 12.5,
                fontWeight: FontWeight.w600,
                color: t.dim,
              ),
            ),
          ),
          Expanded(
            child: Text(
              value,
              style: TextStyle(fontSize: 12.5, color: t.ink),
              overflow: TextOverflow.ellipsis,
            ),
          ),
        ],
      ),
    );
  }

  /// 在线引擎仅在机器账户已关联且拿到权威正余额时允许创建。
  bool get _onlineBlocked {
    if (engine != 'online') return false;
    final account = widget.state.machineAccount;
    return account == null ||
        !account.linked ||
        !account.balanceAvailable ||
        (account.balance ?? 0) <= 0;
  }

  Widget _machineAccountSection(AppTokens t) {
    return AnimatedBuilder(
      animation: widget.state,
      builder: (context, _) {
        final st = widget.state;
        final account = st.machineAccount;
        Widget child;
        if (st.machineAccountLoading && account == null) {
          child = Row(
            children: [
              const SizedBox(
                width: 14,
                height: 14,
                child: CircularProgressIndicator(strokeWidth: 2),
              ),
              const SizedBox(width: 8),
              Text('正在查询机器账户…', style: TextStyle(fontSize: 12.5, color: t.dim)),
            ],
          );
        } else if (account == null) {
          child = Row(
            children: [
              Icon(Icons.error_outline, size: 15, color: t.danger),
              const SizedBox(width: 6),
              Expanded(
                child: Text(
                  '无法获取机器账户:${st.machineAccountError ?? '未知错误'}',
                  style: TextStyle(fontSize: 12.5, color: t.danger),
                  overflow: TextOverflow.ellipsis,
                ),
              ),
              TextButton(
                onPressed: st.refreshMachineAccount,
                child: const Text('重试'),
              ),
            ],
          );
        } else if (!account.linked) {
          child = Row(
            children: [
              Icon(
                Icons.account_balance_wallet_outlined,
                size: 15,
                color: t.warn,
              ),
              const SizedBox(width: 6),
              Text('本机尚未关联账户', style: TextStyle(fontSize: 12.5, color: t.dim)),
              const Spacer(),
              FilledButton.icon(
                onPressed: _redeemCard,
                icon: const Icon(Icons.add_card, size: 15),
                label: const Text('激活'),
              ),
            ],
          );
        } else {
          child = Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Icon(
                    Icons.account_balance_wallet_outlined,
                    size: 15,
                    color: t.success,
                  ),
                  const SizedBox(width: 6),
                  Expanded(
                    child: Text(
                      account.balanceAvailable
                          ? '机器账户 · ${account.balance} 点'
                          : '机器账户 · 点数待更新',
                      style: TextStyle(fontSize: 12.5, color: t.ink),
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                  TextButton(onPressed: _redeemCard, child: const Text('充值')),
                  TextButton(
                    onPressed: _confirmClearCredential,
                    child: Text('退出', style: TextStyle(color: t.danger)),
                  ),
                ],
              ),
              if (!account.balanceAvailable)
                Padding(
                  padding: const EdgeInsets.only(top: 6),
                  child: Row(
                    children: [
                      Expanded(
                        child: Text(
                          account.error.isEmpty
                              ? '暂时无法获取机器账户余额'
                              : account.error,
                          style: TextStyle(fontSize: 12.5, color: t.warn),
                        ),
                      ),
                      TextButton(
                        onPressed: st.refreshMachineAccount,
                        child: const Text('重试'),
                      ),
                    ],
                  ),
                )
              else if ((account.balance ?? 0) <= 0)
                Padding(
                  padding: const EdgeInsets.only(top: 6),
                  child: Text(
                    '余额不足,请充值后再创建在线任务',
                    style: TextStyle(fontSize: 12.5, color: t.danger),
                  ),
                ),
            ],
          );
        }
        return Container(
          width: double.infinity,
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
          decoration: BoxDecoration(
            color: t.primarySoft.withValues(alpha: 0.35),
            borderRadius: BorderRadius.circular(8),
            border: Border.all(color: t.border),
          ),
          child: child,
        );
      },
    );
  }

  Future<void> _redeemCard() async {
    await showCardKeyActivateDialog(context, widget.state);
  }

  Future<void> _confirmClearCredential() async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('退出机器账户'),
        content: const Text('退出后将删除本机访问凭据，不会清除机器账户余额。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(ctx).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(ctx).pop(true),
            child: const Text('退出'),
          ),
        ],
      ),
    );
    if (ok != true) return;
    try {
      await widget.state.clearAccountCredential();
    } catch (e) {
      if (mounted) {
        TopToast.show(context, '$e', error: true);
      }
    }
  }
}
