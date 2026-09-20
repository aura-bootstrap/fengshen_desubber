import 'dart:io';

import 'package:flutter/material.dart';
import 'package:window_manager/window_manager.dart';

import 'app_shell.dart';
import 'app_state.dart';
import 'theme.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  await windowManager.ensureInitialized();
  const opts = WindowOptions(
    size: Size(1180, 760),
    minimumSize: Size(960, 620),
    title: '峰神·去字幕',
    titleBarStyle: TitleBarStyle.normal,
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

/// 引擎 exe 定位:优先与壳同目录(打包形态),退回源码树 ../backend/bin(开发形态)。
String resolveEngineExe() {
  final side = File('${File(Platform.resolvedExecutable).parent.path}\\desub-engine.exe');
  if (side.existsSync()) return side.path;
  final dev = File('${Directory.current.path}\\..\\backend\\bin\\desub-engine.exe');
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
