import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:window_manager/window_manager.dart';

import 'app_state.dart';
import 'pages/task_detail_page.dart';
import 'pages/tasks_page.dart';
import 'theme.dart';
import 'widgets/license_card.dart';

class NavItem {
  final String id;
  final String label;
  final IconData icon;
  const NavItem(this.id, this.label, this.icon);
}

/// 应用版本号(侧栏与状态栏共用;发版时与 installer/user_client/setup.iss 的
/// MyAppVersion 同步)。
const kAppVersion = '1.0.4';

/// 用户版标识(标题栏徽章/侧栏,与管理版区分)。
const kAppEdition = '用户版';

/// 工作区导航(侧栏);后续新页在此追加。
const navWorkspace = [NavItem('tasks', '任务', Icons.task_alt)];

/// 品牌 LOGO:与程序图标同一图(dev 构建绿色,Release 蓝色,见 Runner.rc)。
AssetImage brandLogo() =>
    AssetImage(kReleaseMode ? 'assets/logo.png' : 'assets/logo_green.png');

/// 主框架:自定义标题栏 + 左侧导航(品牌头 + 工作区 + 左下角授权卡片)
/// + 内容区 + 底部状态栏。布局仿切片生成器用户版。
class AppShell extends StatefulWidget {
  final AppState state;
  const AppShell({super.key, required this.state});

  @override
  State<AppShell> createState() => _AppShellState();
}

class _AppShellState extends State<AppShell> {
  String _nav = 'tasks';
  // 详情页嵌在壳内(非路由):隐藏原生标题栏后,壳的标题栏/状态栏需在详情页仍可见。
  int? _detailTaskId;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Column(
        children: [
          _TitleBar(state: widget.state),
          Expanded(
            child: Row(
              children: [
                _Sidebar(
                  state: widget.state,
                  current: _nav,
                  onNav: (v) => setState(() {
                    _nav = v;
                    _detailTaskId = null;
                  }),
                ),
                Expanded(child: _page()),
              ],
            ),
          ),
          _StatusBar(state: widget.state),
        ],
      ),
    );
  }

  Widget _page() {
    final id = _detailTaskId;
    if (id != null) {
      return TaskDetailPage(
        taskId: id,
        state: widget.state,
        onBack: () => setState(() => _detailTaskId = null),
      );
    }
    return switch (_nav) {
      _ => TasksPage(
        state: widget.state,
        onOpenDetail: (taskId) => setState(() => _detailTaskId = taskId),
      ),
    };
  }
}

/// 自定义标题栏:融合主题色,品牌 + 授权徽标 + 版本徽章 + 窗口按钮,
/// 可拖拽/双击最大化(原生标题栏已在 main 隐藏)。
class _TitleBar extends StatelessWidget {
  final AppState state;
  const _TitleBar({required this.state});

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    return Container(
      height: 44,
      decoration: BoxDecoration(
        color: t.surface,
        border: Border(bottom: BorderSide(color: t.border)),
      ),
      child: Row(
        children: [
          Expanded(
            child: DragToMoveArea(
              child: Padding(
                padding: const EdgeInsets.symmetric(horizontal: 14),
                child: Row(
                  children: [
                    ClipRRect(
                      borderRadius: BorderRadius.circular(6),
                      child: Image(image: brandLogo(), width: 20, height: 20),
                    ),
                    const SizedBox(width: 9),
                    Text(
                      '峰神·去字幕',
                      style: TextStyle(
                        fontSize: 12.5,
                        fontWeight: FontWeight.w600,
                        color: t.ink,
                      ),
                    ),
                    const SizedBox(width: 10),
                    ListenableBuilder(
                      listenable: state,
                      builder: (context, _) => _CardKeyBadge(state: state),
                    ),
                    const SizedBox(width: 8),
                    Container(
                      padding: const EdgeInsets.symmetric(
                        horizontal: 8,
                        vertical: 2,
                      ),
                      decoration: BoxDecoration(
                        color: t.primarySoft,
                        borderRadius: BorderRadius.circular(99),
                      ),
                      child: Text(
                        kAppEdition,
                        style: TextStyle(
                          fontSize: 10.5,
                          fontWeight: FontWeight.w700,
                          color: t.primaryInk,
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
          _WinBtn(
            icon: Icons.remove,
            tooltip: '最小化',
            onTap: () => windowManager.minimize(),
          ),
          _WinBtn(
            icon: Icons.crop_square,
            onTap: () async {
              if (await windowManager.isMaximized()) {
                windowManager.unmaximize();
              } else {
                windowManager.maximize();
              }
            },
          ),
          _WinBtn(
            icon: Icons.close,
            danger: true,
            onTap: () => windowManager.close(),
          ),
        ],
      ),
    );
  }
}

/// 机器账户徽标：已关联时展示权威余额，查询失败时不沿用旧数字。
class _CardKeyBadge extends StatelessWidget {
  final AppState state;
  const _CardKeyBadge({required this.state});

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    final account = state.machineAccount;
    final failed = account == null && state.machineAccountError != null;
    final linked = account?.linked ?? false;
    final text = failed
        ? '机器账户查询失败'
        : linked
        ? account!.balanceAvailable
              ? '机器账户 · ${account.balance} 点'
              : '机器账户 · 点数待更新'
        : '未激活';
    final ok = !failed && linked;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
      decoration: BoxDecoration(
        color: ok ? t.successSoft : t.dangerSoft,
        borderRadius: BorderRadius.circular(99),
      ),
      child: Text(
        text,
        style: TextStyle(
          fontSize: 10.5,
          fontWeight: FontWeight.w700,
          color: ok ? t.success : t.danger,
        ),
      ),
    );
  }
}

class _WinBtn extends StatefulWidget {
  final IconData icon;
  final VoidCallback onTap;
  final bool danger;
  final String? tooltip;
  const _WinBtn({
    required this.icon,
    required this.onTap,
    this.danger = false,
    this.tooltip,
  });

  @override
  State<_WinBtn> createState() => _WinBtnState();
}

class _WinBtnState extends State<_WinBtn> {
  bool _hover = false;

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    final dangerHover = widget.danger && _hover;
    final btn = Container(
      width: 46,
      height: 44,
      color: dangerHover
          ? const Color(0xFFE81123)
          : (_hover ? t.primarySoft : Colors.transparent),
      child: Icon(
        widget.icon,
        size: 16,
        color: dangerHover ? Colors.white : t.dim,
      ),
    );
    return MouseRegion(
      onEnter: (_) => setState(() => _hover = true),
      onExit: (_) => setState(() => _hover = false),
      child: GestureDetector(
        onTap: widget.onTap,
        child: widget.tooltip != null
            ? Tooltip(message: widget.tooltip!, child: btn)
            : btn,
      ),
    );
  }
}

class _Sidebar extends StatelessWidget {
  final AppState state;
  final String current;
  final ValueChanged<String> onNav;
  const _Sidebar({
    required this.state,
    required this.current,
    required this.onNav,
  });

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
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
                  child: Image(image: brandLogo(), width: 36, height: 36),
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
                _SectionLabel('流程'),
                for (final item in navWorkspace)
                  _NavTile(
                    item: item,
                    on: current == item.id,
                    onTap: () => onNav(item.id),
                  ),
              ],
            ),
          ),
          LicenseCard(state: state),
        ],
      ),
    );
  }
}

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

class _NavTile extends StatelessWidget {
  final NavItem item;
  final bool on;
  final VoidCallback onTap;
  const _NavTile({required this.item, required this.on, required this.onTap});

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    // 不用 AnimatedContainer:切换时新旧两项同时渐变会双双闪动
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 1),
      child: InkWell(
        borderRadius: BorderRadius.circular(8),
        onTap: onTap,
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

/// 底部状态栏:引擎连接状态(异常时红字) + 右侧版本号。
class _StatusBar extends StatelessWidget {
  final AppState state;
  const _StatusBar({required this.state});

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    return Container(
      height: 34,
      decoration: BoxDecoration(
        color: t.surface,
        border: Border(top: BorderSide(color: t.border)),
      ),
      padding: const EdgeInsets.symmetric(horizontal: 20),
      child: ListenableBuilder(
        listenable: state,
        builder: (context, _) {
          if (state.fatalError != null) {
            return Row(
              children: [
                _Dot(ok: false),
                Expanded(
                  child: Text(
                    '引擎异常: ${state.fatalError}',
                    style: TextStyle(fontSize: 11.5, color: t.danger),
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                const _VersionLabel(),
              ],
            );
          }
          return Row(
            children: [
              _Dot(ok: state.engineReady, warn: !state.engineReady),
              Text(
                state.engineReady ? '引擎已连接' : '引擎启动中…',
                style: TextStyle(fontSize: 11.5, color: t.dim),
              ),
              const Spacer(),
              const _VersionLabel(),
            ],
          );
        },
      ),
    );
  }
}

class _VersionLabel extends StatelessWidget {
  const _VersionLabel();

  @override
  Widget build(BuildContext context) {
    return Text(
      'v$kAppVersion',
      style: TextStyle(fontSize: 11.5, color: context.tokens.faint),
    );
  }
}

class _Dot extends StatelessWidget {
  final bool ok;
  final bool warn; // 中间态(启动中):黄点
  const _Dot({required this.ok, this.warn = false});
  @override
  Widget build(BuildContext context) {
    return Container(
      width: 7,
      height: 7,
      margin: const EdgeInsets.only(right: 6),
      decoration: BoxDecoration(
        shape: BoxShape.circle,
        color: ok
            ? context.tokens.success
            : (warn ? context.tokens.warn : context.tokens.danger),
      ),
    );
  }
}
