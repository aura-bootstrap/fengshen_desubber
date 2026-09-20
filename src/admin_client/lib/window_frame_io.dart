import 'dart:io';

import 'package:flutter/material.dart';
import 'package:window_manager/window_manager.dart';

import 'theme.dart';

Future<void> initAdminWindow() async {
  if (!Platform.isWindows) return;
  await windowManager.ensureInitialized();
  await windowManager.setTitleBarStyle(
    TitleBarStyle.hidden,
    windowButtonVisibility: false,
  );
}

class AdminWindowFrame extends StatelessWidget {
  final Widget child;

  const AdminWindowFrame({super.key, required this.child});

  @override
  Widget build(BuildContext context) {
    if (!Platform.isWindows) return child;
    return Column(
      children: [
        const _TitleBar(),
        Expanded(child: child),
      ],
    );
  }
}

class _TitleBar extends StatelessWidget {
  const _TitleBar();

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    return Material(
      color: t.surface,
      child: Container(
        height: 44,
        decoration: BoxDecoration(
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
                        child: Image.asset(
                          'assets/logo.png',
                          width: 20,
                          height: 20,
                        ),
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
                          '管理版',
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
            _WindowButton(
              icon: Icons.remove,
              tooltip: '最小化',
              onTap: windowManager.minimize,
            ),
            _WindowButton(
              icon: Icons.crop_square,
              onTap: () async {
                if (await windowManager.isMaximized()) {
                  await windowManager.unmaximize();
                } else {
                  await windowManager.maximize();
                }
              },
            ),
            _WindowButton(
              icon: Icons.close,
              danger: true,
              onTap: windowManager.close,
            ),
          ],
        ),
      ),
    );
  }
}

class _WindowButton extends StatefulWidget {
  final IconData icon;
  final VoidCallback onTap;
  final bool danger;
  final String? tooltip;

  const _WindowButton({
    required this.icon,
    required this.onTap,
    this.danger = false,
    this.tooltip,
  });

  @override
  State<_WindowButton> createState() => _WindowButtonState();
}

class _WindowButtonState extends State<_WindowButton> {
  bool _hover = false;

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    final dangerHover = widget.danger && _hover;
    final button = Container(
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
        child: widget.tooltip == null
            ? button
            : Tooltip(message: widget.tooltip!, child: button),
      ),
    );
  }
}
