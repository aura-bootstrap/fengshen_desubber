import 'package:flutter/material.dart';
import 'package:window_manager/window_manager.dart';

import '../app_state.dart';
import '../theme.dart';

class NavItem {
  final String id;
  final String label;
  final IconData icon;
  const NavItem(this.id, this.label, this.icon);
}

/// 应用版本号(侧栏与状态栏共用;发版时与 installer/setup.iss 的 MyAppVersion 同步)。
const kAppVersion = '1.0.4';

/// 开发版标识(标题栏/侧栏,与用户版/管理版区分)。
const kAppEdition = '开发版';

/// 流程:任务/运行。
const navWorkspace = [
  NavItem('tasks', '任务', Icons.task_alt),
  NavItem('run', '运行', Icons.play_circle_outline),
];

/// 配置:五域参数页 + 在线(云端去字幕) + 文本配置 + 版本管理。
const navConfig = [
  NavItem('detect', '检测', Icons.subtitles_outlined),
  NavItem('repair', '修复', Icons.healing_outlined),
  NavItem('enhance', '增强', Icons.auto_awesome_outlined),
  NavItem('encode', '编码', Icons.memory),
  NavItem('output', '产出', Icons.straighten),
  NavItem('online', '在线', Icons.cloud_outlined),
  NavItem('json', '文本配置', Icons.code),
  NavItem('versions', '版本管理', Icons.history),
];

/// 主框架:自定义标题栏 + 左侧导航 + 内容区 + 底部状态栏。
/// 开发版:无授权卡片/激活徽标,全部参数面板可调;在线引擎时才连计费服务。
class AppShell extends StatelessWidget {
  final AppState state;
  final String currentNav;
  final ValueChanged<String> onNav;
  final Widget child;
  final ThemeMode themeMode;
  final VoidCallback onCycleTheme;
  const AppShell({super.key, required this.state, required this.currentNav,
      required this.onNav, required this.child,
      required this.themeMode, required this.onCycleTheme});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Column(children: [
        _TitleBar(state: state, themeMode: themeMode, onCycleTheme: onCycleTheme),
        Expanded(
          child: Row(children: [
            _Sidebar(current: currentNav, onNav: onNav),
            Expanded(child: child),
          ]),
        ),
        _StatusBar(state: state),
      ]),
    );
  }
}

/// 自定义标题栏:融合主题色,品牌 + 开发版徽标 + 窗口按钮,可拖拽/双击最大化。
class _TitleBar extends StatelessWidget {
  final AppState state;
  final ThemeMode themeMode;
  final VoidCallback onCycleTheme;
  const _TitleBar({required this.state, required this.themeMode,
      required this.onCycleTheme});

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    return Container(
      height: 44,
      decoration: BoxDecoration(
        color: t.surface,
        border: Border(bottom: BorderSide(color: t.border)),
      ),
      child: Row(children: [
        Expanded(
          child: DragToMoveArea(
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 14),
              child: Row(children: [
                ClipRRect(
                  borderRadius: BorderRadius.circular(6),
                  child: Image.asset('assets/logo.png', width: 20, height: 20),
                ),
                const SizedBox(width: 9),
                Text('峰神引擎 · 去字幕',
                    style: TextStyle(
                        fontSize: 12.5,
                        fontWeight: FontWeight.w600,
                        color: t.ink)),
                const SizedBox(width: 10),
                Container(
                  padding:
                      const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
                  decoration: BoxDecoration(
                    color: t.primarySoft,
                    borderRadius: BorderRadius.circular(99),
                  ),
                  child: Text(
                    kAppEdition,
                    style: TextStyle(
                        fontSize: 10.5,
                        fontWeight: FontWeight.w700,
                        color: t.primaryInk),
                  ),
                ),
              ]),
            ),
          ),
        ),
        _WinBtn(icon: Icons.remove, tooltip: '最小化',
            onTap: () => windowManager.minimize()),
        _WinBtn(
            icon: Icons.crop_square,
            onTap: () async {
              if (await windowManager.isMaximized()) {
                windowManager.unmaximize();
              } else {
                windowManager.maximize();
              }
            }),
        _WinBtn(
            icon: Icons.close, danger: true, onTap: () => windowManager.close()),
      ]),
    );
  }
}

class _WinBtn extends StatefulWidget {
  final IconData icon;
  final VoidCallback onTap;
  final bool danger;
  final String? tooltip;
  const _WinBtn({required this.icon, required this.onTap, this.danger = false,
      this.tooltip});

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
      child: Icon(widget.icon,
          size: 16,
          color: dangerHover ? Colors.white : t.dim),
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
  final String current;
  final ValueChanged<String> onNav;
  const _Sidebar({required this.current, required this.onNav});

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
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        // 品牌头
        Padding(
          padding: const EdgeInsets.fromLTRB(10, 2, 10, 16),
          child: Row(children: [
            ClipRRect(
              borderRadius: BorderRadius.circular(10),
              child: Image.asset('assets/logo.png', width: 36, height: 36),
            ),
            const SizedBox(width: 11),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text('峰神引擎',
                      style: TextStyle(
                          fontSize: 15,
                          fontWeight: FontWeight.w700,
                          color: t.ink)),
                  Text('去字幕 $kAppEdition v$kAppVersion',
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(fontSize: 11, color: t.faint)),
                ],
              ),
            ),
          ]),
        ),
        // 导航区可滚动,小窗口下仍可访问全部入口。
        Expanded(
          child: ListView(
            padding: EdgeInsets.zero,
            children: [
              _SectionLabel('流程'),
              for (final item in navWorkspace)
                _NavTile(
                    item: item,
                    on: current == item.id,
                    onTap: () => onNav(item.id)),
              const SizedBox(height: 10),
              _SectionLabel('配置'),
              for (final item in navConfig)
                _NavTile(
                    item: item,
                    on: current == item.id,
                    onTap: () => onNav(item.id)),
            ],
          ),
        ),
        // 开发版页脚:无授权概念,标识全参数可调
        Padding(
          padding: const EdgeInsets.fromLTRB(12, 0, 12, 2),
          child: Text('$kAppEdition · 全参数面板可调 · 不连服务器',
              style: TextStyle(fontSize: 10.5, color: t.faint)),
        ),
      ]),
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
      child: Row(children: [
        Container(
          width: 3,
          height: 13,
          decoration: BoxDecoration(
            color: t.primary,
            borderRadius: BorderRadius.circular(2),
          ),
        ),
        const SizedBox(width: 7),
        Text(text,
            style: TextStyle(
                fontSize: 12.5,
                fontWeight: FontWeight.w700,
                letterSpacing: 1.5,
                color: t.ink)),
        const SizedBox(width: 10),
        Expanded(child: Container(height: 1, color: t.border)),
      ]),
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
          child: Row(children: [
            Icon(item.icon,
                size: 17,
                color: on ? t.primaryInk : t.faint),
            const SizedBox(width: 11),
            Text(item.label,
                style: TextStyle(
                  fontSize: 13.5,
                  fontWeight: on ? FontWeight.w600 : FontWeight.w500,
                  color: on ? t.primaryInk : t.dim,
                )),
          ]),
        ),
      ),
    );
  }
}

class _StatusBar extends StatelessWidget {
  final AppState state;
  const _StatusBar({required this.state});

  @override
  Widget build(BuildContext context) {
    final st = state;
    final t = context.tokens;
    return Container(
      height: 34,
      decoration: BoxDecoration(
        color: t.surface,
        border: Border(top: BorderSide(color: t.border)),
      ),
      padding: const EdgeInsets.symmetric(horizontal: 20),
      child: Row(children: [
        _Dot(ok: st.engineConnected),
        Text(st.engineConnected ? '引擎已连接' : '连接中断,重连中…',
            style: TextStyle(fontSize: 11.5, color: t.dim)),
        if (st.savePhase == SavePhase.saving) ...[
          const SizedBox(width: 18),
          Text('自动保存…',
              style: TextStyle(fontSize: 11.5, color: t.dim)),
        ] else if (st.savePhase == SavePhase.saved) ...[
          const SizedBox(width: 18),
          Text('已自动保存',
              style: TextStyle(fontSize: 11.5, color: t.faint)),
        ] else if (st.savePhase == SavePhase.failed) ...[
          const SizedBox(width: 18),
          Flexible(
            child: Text('● 保存失败:${st.saveError}',
                style: TextStyle(fontSize: 11.5, color: t.danger),
                overflow: TextOverflow.ellipsis),
          ),
        ],
        const Spacer(),
        if (st.running)
          Text('运行中 · ${st.stage}',
              style: TextStyle(
                  fontSize: 11.5,
                  color: t.primaryInk,
                  fontWeight: FontWeight.w600)),
        const SizedBox(width: 16),
        Text('v$kAppVersion',
            style: TextStyle(fontSize: 11.5, color: t.faint)),
      ]),
    );
  }
}

class _Dot extends StatelessWidget {
  final bool ok;
  const _Dot({required this.ok});
  @override
  Widget build(BuildContext context) {
    return Container(
      width: 7,
      height: 7,
      margin: const EdgeInsets.only(right: 6),
      decoration: BoxDecoration(
        shape: BoxShape.circle,
        color: ok ? context.tokens.success : context.tokens.danger,
      ),
    );
  }
}
