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

  Future<void> _redeem(BuildContext context) async {
    final ok = await showCardKeyActivateDialog(context, state);
    if (ok == true && state.machineAccount?.linked == true && context.mounted) {
      TopToast.show(context, '充值卡核销成功');
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
    final account = state.machineAccount;
    final linked = account?.linked ?? false;
    final loading = state.machineAccountLoading && account == null;
    final balanceAvailable = account?.balanceAvailable ?? false;
    final error = state.machineAccountError ?? account?.error ?? '';
    final String pill;
    final Color pillColor;
    if (loading) {
      pill = '查询中';
      pillColor = t.faint;
    } else if (!linked) {
      pill = '未激活';
      pillColor = t.faint;
    } else if (!balanceAvailable) {
      pill = '连接失败';
      pillColor = t.danger;
    } else {
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
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Text(
                linked ? '机器账户' : '云端去字幕',
                style: TextStyle(
                  fontSize: 12,
                  fontWeight: FontWeight.w700,
                  color: t.ink,
                ),
              ),
              const Spacer(),
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
                decoration: BoxDecoration(
                  color: pillColor,
                  borderRadius: BorderRadius.circular(99),
                ),
                child: Text(
                  pill,
                  style: const TextStyle(
                    fontSize: 10,
                    fontWeight: FontWeight.w700,
                    color: Colors.white,
                  ),
                ),
              ),
            ],
          ),
          if (loading) ...[
            const SizedBox(height: 4),
            Text('正在查询机器账户…', style: TextStyle(fontSize: 11, color: t.dim)),
          ] else if (linked && account != null) ...[
            const SizedBox(height: 4),
            Text(
              balanceAvailable ? '${account.balance} 点' : '点数待更新',
              style: TextStyle(
                fontSize: 20,
                fontWeight: FontWeight.w700,
                color: balanceAvailable ? t.primaryInk : t.warn,
              ),
            ),
            const SizedBox(height: 2),
            Text(
              '机器码 ${account.machineHash.toUpperCase()}',
              style: TextStyle(fontSize: 11, color: t.dim),
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
            ),
            if (!balanceAvailable) ...[
              const SizedBox(height: 2),
              Text(
                error.isEmpty ? '暂时无法获取机器账户余额' : error,
                style: TextStyle(fontSize: 10.5, color: t.danger),
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
              ),
              const SizedBox(height: 7),
              InkWell(
                onTap: state.refreshMachineAccount,
                child: Text(
                  '重新查询 ›',
                  style: TextStyle(
                    fontSize: 10.5,
                    color: t.primaryInk,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ),
            ],
          ] else ...[
            const SizedBox(height: 4),
            Text(
              error.isEmpty ? '核销充值卡后才能使用在线去字幕' : error,
              style: TextStyle(fontSize: 11, color: t.dim),
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
            ),
          ],
          const SizedBox(height: 9),
          InkWell(
            onTap: () => _redeem(context),
            child: Text(
              linked ? '账户充值 ›' : '立即激活 ›',
              style: TextStyle(
                fontSize: 10.5,
                color: t.primaryInk,
                fontWeight: FontWeight.w600,
              ),
            ),
          ),
        ],
      ),
    );
  }
}
