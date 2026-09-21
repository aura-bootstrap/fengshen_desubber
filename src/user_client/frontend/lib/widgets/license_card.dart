import 'package:flutter/material.dart';

import '../app_state.dart';
import '../theme.dart';
import 'cardkey_activate_dialog.dart';
import 'top_toast.dart';

/// 侧边栏左下角余额卡片:只显示剩余点数与机器码,不显示卡号、无「卡密授权」标题。
/// 未激活=「立即激活」引导;机器码为引擎出参的前 16 位分组明文(同 slicer 式样)。
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
    final loading = state.cardKeyLoading && ck == null;
    final cloudDown = activated && ck != null && (ck.degraded || ck.stale);
    // 标题行 + 右上角云端状态胶囊(仿 slicer 授权卡绿底胶囊式样)。
    final String title;
    final String pill;
    final Color pillColor;
    if (hasError) {
      title = '授权状态';
      pill = '连接失败';
      pillColor = t.danger;
    } else if (loading) {
      title = '授权状态';
      pill = '查询中';
      pillColor = t.faint;
    } else if (!activated) {
      title = '云端去字幕';
      pill = '未激活';
      pillColor = t.faint;
    } else if (cloudDown) {
      title = '剩余点数';
      pill = '云离线';
      pillColor = t.warn;
    } else {
      title = '剩余点数';
      pill = '云在线';
      pillColor = t.success;
    }
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
          Text(title,
              style: TextStyle(
                  fontSize: 12, fontWeight: FontWeight.w700, color: t.ink)),
          const Spacer(),
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
            decoration: BoxDecoration(
              color: pillColor,
              borderRadius: BorderRadius.circular(99),
            ),
            child: Text(pill,
                style: const TextStyle(
                    fontSize: 10,
                    fontWeight: FontWeight.w700,
                    color: Colors.white)),
          ),
        ]),
        if (loading) ...[
          const SizedBox(height: 4),
          Text('查询中…', style: TextStyle(fontSize: 11, color: t.dim)),
        ] else if (hasError) ...[
          const SizedBox(height: 4),
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
            const SizedBox(height: 4),
            Text('${ck.credits} 点',
                style: TextStyle(
                    fontSize: 20,
                    fontWeight: FontWeight.w700,
                    color: ck.credits > 0 ? t.primaryInk : t.danger)),
            const SizedBox(height: 2),
            // 机器码为前 16 位分组明文 XXXX-XXXX-XXXX-XXXX(不打码,引擎出口已大写,
            // toUpperCase 幂等)。19 字符 @11px ≈ 125px < 内容宽,单行放得下。
            Text('机器码 ${ck.machineHash.toUpperCase()}',
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
          ] else ...[
            const SizedBox(height: 4),
            Text('激活卡密后才能使用在线去字幕',
                style: TextStyle(fontSize: 11, color: t.dim)),
          ],
          const SizedBox(height: 9),
          InkWell(
            onTap: () => _activate(context),
            child: Text(activated ? '账户充值 ›' : '立即激活 ›',
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
