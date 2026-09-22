import 'dart:io';

import 'package:flutter/material.dart';
import 'package:window_manager/window_manager.dart';

import 'app_shell.dart';
import 'app_state.dart';
import 'error_widget_capture.dart';
import 'theme.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  installErrorWidgetCapture();
  await windowManager.ensureInitialized();
  const opts = WindowOptions(
    size: Size(1180, 760),
    minimumSize: Size(960, 620),
    title: '峰神引擎-字幕去除器-用户版',
    // 隐藏原生标题栏(主题融合由 AppShell 的自定义标题栏接管)
    titleBarStyle: TitleBarStyle.hidden,
    windowButtonVisibility: false,
  );
  unawaitedWindow(opts);

  final state = AppState();
  runApp(DesubApp(state: state));
}

void unawaitedWindow(WindowOptions opts) {
  windowManager.waitUntilReadyToShow(opts, () async {
    await windowManager.show();
    await windowManager.focus();
  });
}

/// 引擎 exe 定位:优先与壳同目录(打包形态),退回源码树 backend/bin(开发形态,
/// 按 exe 位置 frontend/build/windows/x64/runner/`<cfg>`/ 上溯 6 级到 user_client/,与 CWD 无关)。
String resolveEngineExe() {
  final exeDir = File(Platform.resolvedExecutable).parent.path;
  final side = File('$exeDir\\desub-engine.exe');
  if (side.existsSync()) return side.path;
  final dev = File('$exeDir\\..\\..\\..\\..\\..\\..\\backend\\bin\\desub-engine.exe');
  return dev.absolute.path;
}

class DesubApp extends StatefulWidget {
  final AppState state;
  const DesubApp({super.key, required this.state});

  @override
  State<DesubApp> createState() => _DesubAppState();
}

class _DesubAppState extends State<DesubApp> {
  @override
  void initState() {
    super.initState();
    widget.state.boot(resolveEngineExe());
  }

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: '峰神·去字幕',
      debugShowCheckedModeBanner: false,
      theme: buildAppTheme(),
      darkTheme: buildAppDarkTheme(),
      themeMode: ThemeMode.system,
      home: AppShell(state: widget.state),
    );
  }
}
