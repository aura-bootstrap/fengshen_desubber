import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';

import 'app_state.dart';
import 'pages/tasks_page.dart';
import 'theme.dart';
import 'widgets/license_card.dart';

class NavItem {
  final String id;
  final String label;
  final IconData icon;
  const NavItem(this.id, this.label, this.icon);
}

/// 工作区导航(侧栏);后续新页在此追加。
const navWorkspace = [
  NavItem('tasks', '任务', Icons.task_alt),
];

/// 应用壳:顶栏(品牌+引擎状态) + 左侧导航(工作区 + 左下角授权卡片) + 内容区。
/// 布局仿切片生成器;授权卡片显示卡密剩余点数(见 widgets/license_card.dart)。
class AppShell extends StatefulWidget {
  final AppState state;
  const AppShell({super.key, required this.state});

  @override
  State<AppShell> createState() => _AppShellState();
}

class _AppShellState extends State<AppShell> {
  String _nav = 'tasks';

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    return Scaffold(
      body: Column(
        children: [
          Container(
            height: 56,
            padding: const EdgeInsets.symmetric(horizontal: 20),
            decoration: BoxDecoration(
              color: t.surface,
              border: Border(bottom: BorderSide(color: t.border)),
            ),
            child: Row(
              children: [
                // 品牌 LOGO:与程序图标同一图(dev 构建绿色,Release 蓝色,见 Runner.rc)
                ClipRRect(
                  borderRadius: BorderRadius.circular(8),
                  child: Image.asset(
                    kReleaseMode ? 'assets/logo.png' : 'assets/logo_green.png',
                    width: 30,
                    height: 30,
                  ),
                ),
                const SizedBox(width: 10),
                const Text('峰神·去字幕',
                    style: TextStyle(fontSize: 15, fontWeight: FontWeight.w700)),
                const Spacer(),
                ListenableBuilder(
                  listenable: widget.state,
                  builder: (context, _) {
                    if (widget.state.fatalError != null) {
                      return Text('引擎异常: ${widget.state.fatalError}',
                          style: TextStyle(fontSize: 12, color: t.danger));
                    }
                    return Row(
                      children: [
                        Icon(Icons.circle,
                            size: 8, color: widget.state.engineReady ? t.success : t.warn),
                        const SizedBox(width: 6),
                        Text(widget.state.engineReady ? '引擎已连接' : '引擎启动中…',
                            style: TextStyle(fontSize: 12, color: t.dim)),
                      ],
                    );
                  },
                ),
              ],
            ),
          ),
          Expanded(
            child: Row(
              children: [
                _Sidebar(
                  state: widget.state,
                  current: _nav,
                  onNav: (v) => setState(() => _nav = v),
                ),
                Expanded(child: _page()),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _page() => switch (_nav) {
        _ => TasksPage(state: widget.state),
      };
}

class _Sidebar extends StatelessWidget {
  final AppState state;
  final String current;
  final ValueChanged<String> onNav;
  const _Sidebar({required this.state, required this.current, required this.onNav});

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
          Expanded(
            child: ListView(
              padding: EdgeInsets.zero,
              children: [
                _SectionLabel('工作区'),
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
      padding: const EdgeInsets.fromLTRB(12, 2, 12, 8),
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
