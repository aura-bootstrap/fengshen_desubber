import 'package:flutter/material.dart';

import '../app_state.dart';
import '../theme.dart';
import 'cardkey_activate_dialog.dart';
import 'top_toast.dart';

/// 侧边栏左下角授权卡片(卡密点数版,仿切片生成器 LicenseCard):
/// 未激活=「立即激活」引导;已激活=剩余点数(大数字)/卡号/服务器三行。
/// 与切片生成器的区别:授权码那一行换成点数额度的剩余值。
class LicenseCard extends StatelessWidget {
  final AppState state;
  const LicenseCard({super.key, required this.state});

  Future<void> _activate(BuildContext context) async {
    final ok = await showCardKeyActivateDialog(context, state);
    if (ok == true && state.cardKey?.activated == true && context.mounted) {
      TopToast.show(context, '卡密激活成功');
    }
  }

  @override
  Widget build(BuildContext context) {
    // 侧栏整体不随 AppState 重建,卡片自监听:激活/余额变化即时刷新。
    return ListenableBuilder(
      listenable: state,
      builder: (context, _) => _buildCard(context),
    );
  }

  Widget _buildCard(BuildContext context) {
    final t = context.tokens;
    final ck = state.cardKey;
    final hasError = state.cardKeyError != null;
    final activated = ck?.activated ?? false;
    return Container(
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        gradient: const LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [Color(0xFFF2F5FF), Color(0xFFEAF7F0)],
        ),
        border: Border.all(color: t.primary.withValues(alpha: .55), width: 1.4),
        borderRadius: BorderRadius.circular(10),
      ),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Row(children: [
          Text(hasError ? '授权状态' : (activated ? '卡密授权' : '未激活'),
              style: TextStyle(
                  fontSize: 12, fontWeight: FontWeight.w700, color: t.ink)),
          const Spacer(),
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
            decoration: BoxDecoration(
              color: hasError ? t.danger : (activated ? t.success : t.faint),
              borderRadius: BorderRadius.circular(99),
            ),
            child: Text(hasError ? '查询失败' : (activated ? '已激活' : '未激活'),
                style: const TextStyle(
                    fontSize: 10,
                    fontWeight: FontWeight.w700,
                    color: Colors.white)),
          ),
        ]),
        const SizedBox(height: 4),
        if (state.cardKeyLoading && ck == null)
          Text('查询中…', style: TextStyle(fontSize: 11, color: t.dim))
        else if (hasError) ...[
          Text(state.cardKeyError!,
              style: TextStyle(fontSize: 11, color: t.danger),
              maxLines: 2,
              overflow: TextOverflow.ellipsis),
          const SizedBox(height: 9),
          InkWell(
            onTap: state.refreshCardKey,
            child: Text('重新查询 ›',
                style: TextStyle(
                    fontSize: 10.5,
                    color: t.primaryInk,
                    fontWeight: FontWeight.w600)),
          ),
        ] else ...[
          if (activated && ck != null) ...[
            Text('剩余点数', style: TextStyle(fontSize: 11, color: t.dim)),
            Text('${ck.credits} 点',
                style: TextStyle(
                    fontSize: 20,
                    fontWeight: FontWeight.w700,
                    color: ck.credits > 0 ? t.primaryInk : t.danger)),
            const SizedBox(height: 2),
            Text('卡号 ${ck.masked}',
                style: TextStyle(fontSize: 11, color: t.dim),
                maxLines: 1,
                overflow: TextOverflow.ellipsis),
            if (ck.degraded || ck.stale) ...[
              const SizedBox(height: 2),
              Text('余额为本地缓存,云端暂不可达',
                  style: TextStyle(fontSize: 10.5, color: t.warn),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis),
            ],
          ] else
            Text('激活卡密后才能使用在线去字幕',
                style: TextStyle(fontSize: 11, color: t.dim)),
          const SizedBox(height: 9),
          InkWell(
            onTap: () => _activate(context),
            child: Text(activated ? '更换卡密 ›' : '立即激活 ›',
                style: TextStyle(
                    fontSize: 10.5,
                    color: t.primaryInk,
                    fontWeight: FontWeight.w600)),
          ),
        ],
      ]),
    );
  }
}
