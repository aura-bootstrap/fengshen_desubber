import 'dart:io';

import 'package:flutter/material.dart';
import 'package:window_manager/window_manager.dart';

import 'app_shell.dart';
import 'app_state.dart';
import 'engine_client.dart';
import 'error_widget_capture.dart';
import 'models.dart';
import 'pages/config_page.dart';
import 'pages/json_page.dart';
import 'pages/run_page.dart';
import 'pages/tasks_page.dart';
import 'pages/versions_page.dart';
import 'settings.dart';
import 'theme.dart';
import 'widgets/top_toast.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  installErrorWidgetCapture();
  await windowManager.ensureInitialized();
  // 隐藏原生标题栏(主题融合由 AppShell 的自定义标题栏接管)
  await windowManager.setTitleBarStyle(
    TitleBarStyle.hidden,
    windowButtonVisibility: false,
  );
  final mode = await UiSettings.loadThemeMode();
  runApp(DesubApp(initialMode: mode));
}

class DesubApp extends StatelessWidget {
  final ThemeMode initialMode;
  const DesubApp({super.key, required this.initialMode});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: '峰神·去字幕(开发版)',
      debugShowCheckedModeBanner: false,
      theme: buildAppTheme(),
      themeMode: ThemeMode.light, // 永久浅色(用户指定;暗色 token 保留备用)
      home: Root(themeMode: initialMode, onCycleTheme: () {}),
    );
  }
}

class Root extends StatefulWidget {
  final ThemeMode themeMode;
  final VoidCallback onCycleTheme;
  const Root({super.key, required this.themeMode, required this.onCycleTheme});

  @override
  State<Root> createState() => _RootState();
}

class _RootState extends State<Root> {
  late final EngineClient client;
  late final AppState state;
  String nav = 'tasks';
  String? bootError;
  bool booting = true;
  String bootStep = '';

  @override
  void initState() {
    super.initState();
    client = EngineClient();
    state = AppState(client);
    // 任务页点卡片/立即运行 → 跳到运行页看详情
    state.onOpenTask = (_) => setState(() => nav = 'run');
    // 运行级失败 → Toast 提示
    state.onErrorToast = (msg) {
      if (mounted) TopToast.show(context, msg, error: true);
    };
    _boot();
  }

  Future<void> _boot() async {
    setState(() {
      booting = true;
      bootError = null;
      bootStep = '正在启动引擎…';
    });
    try {
      // 引擎 exe 与 Flutter exe 同目录(发布包结构)
      final engineExe =
          '${File(Platform.resolvedExecutable).parent.path}\\desub-engine.exe';
      await client.start(engineExe);
      await state.init();
    } catch (e) {
      bootError = '$e';
    }
    if (mounted) setState(() => booting = false);
  }

  @override
  void dispose() {
    client.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final t = context.tokens;
    if (booting) {
      return Scaffold(
        body: Center(
          child: Column(mainAxisSize: MainAxisSize.min, children: [
            const CircularProgressIndicator(),
            const SizedBox(height: 16),
            Text(bootStep, style: TextStyle(fontSize: 12.5, color: t.dim)),
          ]),
        ),
      );
    }
    if (bootError != null) {
      return Scaffold(
        body: Center(
          child: Column(mainAxisSize: MainAxisSize.min, children: [
            Icon(Icons.error_outline, size: 44, color: t.danger),
            const SizedBox(height: 14),
            Text('引擎启动失败',
                style: TextStyle(
                    fontSize: 17, fontWeight: FontWeight.w700, color: t.ink)),
            const SizedBox(height: 8),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 60),
              child: Text(bootError!,
                  textAlign: TextAlign.center,
                  style: TextStyle(fontSize: 12.5, color: t.dim)),
            ),
            const SizedBox(height: 18),
            FilledButton(onPressed: _boot, child: const Text('重试')),
          ]),
        ),
      );
    }
    return AnimatedBuilder(
      animation: state,
      builder: (context, _) {
        final page = switch (nav) {
          'run' => RunPage(state: state),
          'tasks' => TasksPage(state: state),
          'json' => JsonPageView(state: state),
          'versions' =>
            VersionsPage(key: const ValueKey('versions'), state: state),
          _ => ConfigPageView(
              key: ValueKey(nav),
              state: state,
              page: configPages.firstWhere((p) => p.navId == nav),
            ),
        };
        return AppShell(
          state: state,
          currentNav: nav,
          // 切页兜底同步:任务/运行页进度除 SSE 实时刷新外,切入时再对一次账
          onNav: (id) {
            setState(() => nav = id);
            if (id == 'tasks' || id == 'run') {
              state.refreshTasks();
            }
          },
          themeMode: widget.themeMode,
          onCycleTheme: widget.onCycleTheme,
          child: page,
        );
      },
    );
  }
}
