import 'dart:io';

import 'package:flutter/material.dart';

/// release 下 build 抛异常的默认表现是无字灰盒(RenderErrorBox:无界约束下高度取
/// 10 万 px,会把整页撑爆),且不留任何线索供事后追因。这里统一接管:异常+堆栈
/// 追加写 exeDir/logs/ui_error_widget.log(写不进退系统临时目录),灰盒换有界
/// 占位块,页面其余部分保持可用。
/// 本文件为三端(管理/开发/用户)同文副本,改动须三端同步。
void installErrorWidgetCapture() {
  ErrorWidget.builder = (details) {
    _append(details);
    return const ErrorPlaceholder();
  };
}

class ErrorPlaceholder extends StatelessWidget {
  const ErrorPlaceholder({super.key});

  @override
  Widget build(BuildContext context) => Container(
        height: 44,
        alignment: Alignment.centerLeft,
        padding: const EdgeInsets.symmetric(horizontal: 10),
        decoration: BoxDecoration(
          color: const Color(0xFFFDECEC),
          borderRadius: BorderRadius.circular(8),
        ),
        child: const Text(
          '控件渲染异常,日志见 logs/ui_error_widget.log',
          style: TextStyle(fontSize: 12, color: Color(0xFFC03535)),
        ),
      );
}

void _append(FlutterErrorDetails details) {
  // 捕获自身任何失败都不得影响错误占位展示:全程吞异常。
  try {
    final ts = DateTime.now().toIso8601String();
    final stack =
        (details.stack?.toString() ?? '').split('\n').take(30).join('\n');
    final line = '[$ts] ${details.exception}\n$stack\n\n';
    debugPrint(line);
    for (final dir in [
      Directory('${_exeDir()}${Platform.pathSeparator}logs'),
      Directory.systemTemp,
    ]) {
      try {
        dir.createSync(recursive: true);
        File('${dir.path}${Platform.pathSeparator}ui_error_widget.log')
            .writeAsStringSync(line, mode: FileMode.append, flush: true);
        return;
      } catch (_) {
        continue;
      }
    }
  } catch (_) {/* 吞掉 */}
}

String _exeDir() {
  final p = Platform.resolvedExecutable;
  final i = p.lastIndexOf(Platform.pathSeparator);
  return i <= 0 ? p : p.substring(0, i);
}
