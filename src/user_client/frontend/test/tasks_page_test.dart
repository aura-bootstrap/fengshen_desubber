import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:desub_client/app_state.dart';
import 'package:desub_client/pages/tasks_page.dart';
import 'package:desub_client/theme.dart';

void main() {
  testWidgets('tasks page renders header, new-task button and empty hint',
      (tester) async {
    final state = AppState();
    await tester.pumpWidget(MaterialApp(
      theme: buildAppTheme(),
      home: Scaffold(body: TasksPage(state: state, onOpenDetail: (_) {})),
    ));
    expect(find.text('任务'), findsOneWidget);
    expect(find.text('新建任务'), findsOneWidget);
    expect(find.textContaining('暂无任务'), findsOneWidget);
  });
}
