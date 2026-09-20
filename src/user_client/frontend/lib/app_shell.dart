import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';

import 'app_state.dart';
import 'pages/tasks_page.dart';
import 'theme.dart';

/// 应用壳:顶栏(品牌+引擎状态) + 任务页。
class AppShell extends StatelessWidget {
  final AppState state;
  const AppShell({super.key, required this.state});

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
                  listenable: state,
                  builder: (context, _) {
                    if (state.fatalError != null) {
                      return Text('引擎异常: ${state.fatalError}',
                          style: TextStyle(fontSize: 12, color: t.danger));
                    }
                    return Row(
                      children: [
                        Icon(Icons.circle,
                            size: 8, color: state.engineReady ? t.success : t.warn),
                        const SizedBox(width: 6),
                        Text(state.engineReady ? '引擎已连接' : '引擎启动中…',
                            style: TextStyle(fontSize: 12, color: t.dim)),
                      ],
                    );
                  },
                ),
              ],
            ),
          ),
          Expanded(child: TasksPage(state: state)),
        ],
      ),
    );
  }
}
