import 'dart:async';
import 'dart:io';

import 'package:flutter/foundation.dart';

import 'engine_client.dart';
import 'models.dart';

/// 全局状态:任务列表 + SSE 驱动刷新 + 引擎进程生命周期。
class AppState extends ChangeNotifier {
  final EngineClient client = EngineClient();

  List<DesubTask> tasks = [];
  bool engineReady = false;
  String? fatalError;
  StreamSubscription<EngineEvent>? _sub;

  /// 详情页订阅的滚动日志(仅当前任务)。
  final Map<int, List<String>> logs = {};
  int? watchingTaskId;

  /// 卡密状态(在线去字幕引擎);null = 尚未查询成功。
  CardKeyStatus? cardKey;
  bool cardKeyLoading = false;
  String? cardKeyError;

  /// 拉取卡密状态;失败保留错误文案供界面展示重试。
  Future<void> refreshCardKey() async {
    cardKeyLoading = true;
    cardKeyError = null;
    notifyListeners();
    try {
      cardKey = await client.cardkeyStatus();
    } catch (e) {
      print('[cardkey] status failed: $e');
      cardKeyError = '$e';
    }
    cardKeyLoading = false;
    notifyListeners();
  }

  /// 激活卡密,成功后刷新状态;失败原样上抛(ApiException 中文文案)。
  Future<void> activateCardKey(String server, String cardKey) async {
    await client.activateCardKey(server: server, cardKey: cardKey);
    await refreshCardKey();
  }

  /// 解绑卡密,成功后刷新状态。
  Future<void> deactivateCardKey() async {
    await client.deactivateCardKey();
    await refreshCardKey();
  }

  Future<void> boot(String engineExe) async {
    try {
      print('[boot] engine exe: $engineExe exists=${File(engineExe).existsSync()}');
      await client.start(engineExe);
      print('[boot] engine ready at ${client.baseUrl}');
      engineReady = true;
      await refresh();
      // 侧栏授权卡片常驻,启动即拉一次卡密状态(未激活/失败都会在卡片上呈现)。
      await refreshCardKey();
      _sub = client.events().listen(_onEvent, onError: (_) {});
      notifyListeners();
    } catch (e) {
      print('[boot] failed: $e');
      fatalError = '$e';
      notifyListeners();
    }
  }

  Future<void> refresh() async {
    tasks = await client.listTasks();
    notifyListeners();
  }

  void _onEvent(EngineEvent ev) {
    if (ev.taskId != 0 && ev.type == 'log') {
      final list = logs.putIfAbsent(ev.taskId, () => []);
      list.add(ev.msg);
      if (list.length > 5000) list.removeRange(0, list.length - 5000);
    }
    // 任何事件都可能改变列表(进度/状态),轻量刷新:progress 高频,
    // 只打本地补丁;stage/queue/done 走全量刷新。
    switch (ev.type) {
      case 'progress':
        final i = tasks.indexWhere((t) => t.id == ev.taskId);
        if (i >= 0) {
          final t = tasks[i];
          tasks[i] = DesubTask(
            id: t.id, name: t.name, srcPath: t.srcPath, outName: t.outName,
            paramsJson: t.paramsJson, status: t.status,
            // 在线链路 progress 带 stage(upload/download);本地管线空值=repair 帧计数
            stage: ev.stage.isNotEmpty ? ev.stage : 'repair',
            done: ev.done, total: ev.total, workDir: t.workDir,
            reportJson: t.reportJson, error: t.error,
            createdAt: t.createdAt, updatedAt: t.updatedAt,
          );
        }
        notifyListeners();
      case 'stage' || 'queue' || 'done':
        refresh();
      default:
        notifyListeners();
    }
  }

  void watchLogs(int taskId) {
    watchingTaskId = taskId;
    logs.putIfAbsent(taskId, () => []);
  }

  Future<String?> action(Future<void> Function() op) async {
    try {
      await op();
      await refresh();
      return null;
    } catch (e) {
      return '$e';
    }
  }

  /// 打开产物目录(Windows 资源管理器)。
  Future<void> openWorkDir(DesubTask t) async {
    if (t.workDir.isEmpty) return;
    await Process.run('explorer.exe', [t.workDir]);
  }

  @override
  void dispose() {
    _sub?.cancel();
    client.dispose();
    super.dispose();
  }
}
