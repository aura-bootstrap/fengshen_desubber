import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';

/// UI 本地偏好(主题模式),存 exe 旁 ui_settings.json。
class UiSettings {
  static File _file() {
    final exeDir = File(Platform.resolvedExecutable).parent;
    return File('${exeDir.path}\\ui_settings.json');
  }

  static Future<ThemeMode> loadThemeMode() async {
    try {
      final j = jsonDecode(await _file().readAsString());
      return switch (j['theme']) {
        'light' => ThemeMode.light,
        'dark' => ThemeMode.dark,
        _ => ThemeMode.system,
      };
    } catch (_) {
      return ThemeMode.light; // 默认浅色
    }
  }

  static Future<void> saveThemeMode(ThemeMode m) async {
    try {
      await _file().writeAsString(jsonEncode({
        'theme': switch (m) {
          ThemeMode.light => 'light',
          ThemeMode.dark => 'dark',
          _ => 'system',
        },
      }));
    } catch (_) {}
  }
}
