import 'dart:async';

import 'package:flutter_test/flutter_test.dart';

import 'package:desub_client/app_state.dart';
import 'package:desub_client/engine_client.dart';
import 'package:desub_client/models.dart';

class FakeEngineClient extends EngineClient {
  final controller = StreamController<EngineEvent>.broadcast();
  final balances = <int>[10, 8, 9];
  int statusCalls = 0;

  final task = const DesubTask(
    id: 1,
    name: '在线任务',
    srcPath: r'D:\视频\样片.mp4',
    outName: '样片_desub.mp4',
    paramsJson: '{"online":true}',
    status: 'running',
    stage: 'upload',
    done: 0,
    total: 0,
    workDir: '',
    reportJson: '',
    error: '',
  );

  @override
  Future<void> start(String engineExe) async {
    baseUrl = 'http://127.0.0.1:1';
  }

  @override
  Future<List<DesubTask>> listTasks() async => [task];

  @override
  Future<CardKeyStatus> cardkeyStatus() async {
    final index = statusCalls < balances.length ? statusCalls : balances.length - 1;
    final balance = balances[index];
    statusCalls++;
    return CardKeyStatus(
      activated: true,
      masked: 'ABCDE…QRST',
      credits: balance,
      machineHash: 'machine',
      degraded: false,
      stale: false,
    );
  }

  @override
  Stream<EngineEvent> events() => controller.stream;

  @override
  Future<void> dispose() async {
    await controller.close();
  }
}

Future<void> flushEvents() async {
  await Future<void>.delayed(const Duration(milliseconds: 20));
}

void main() {
  test('在线任务扣费和失败退款后刷新余额', () async {
    final client = FakeEngineClient();
    final state = AppState(client: client);
    await state.boot('fake-engine.exe');

    expect(state.cardKey?.credits, 10);
    expect(client.statusCalls, 1);

    client.controller.add(const EngineEvent(
      type: 'stage',
      taskId: 1,
      stage: 'cloud',
    ));
    await flushEvents();
    expect(state.cardKey?.credits, 8);
    expect(client.statusCalls, 2);

    client.controller.add(const EngineEvent(
      type: 'done',
      taskId: 1,
      status: 'failed',
    ));
    await flushEvents();
    expect(state.cardKey?.credits, 9);
    expect(client.statusCalls, 3);

    state.dispose();
  });
}
