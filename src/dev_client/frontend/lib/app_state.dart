import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';

import 'engine_client.dart';
import 'models.dart';

/// config 最近一次变更来源:决定 JSON 编辑器是否允许重写控制器文本(防光标跳)。
enum EditOrigin { form, json, remote }

/// 自动保存相位:状态栏/JSON 页据此显示『自动保存…/已自动保存/保存失败』。
enum SavePhase { idle, saving, saved, failed }

class LogLine {
  final String ts, text;
  final LogKind kind;
  const LogLine(this.ts, this.text, this.kind);
}

enum LogKind { plain, stage, ok, err }

/// 全局应用状态:配置 + 任务 + 引擎事件聚合(开发版:无授权概念,不连服务器)。
class AppState extends ChangeNotifier {
  final EngineClient client;
  AppState(this.client);

  Map<String, dynamic> config = {};
  EditOrigin lastOrigin = EditOrigin.remote;
  String jsonError = '';
  SavePhase savePhase = SavePhase.idle;
  String saveError = '';
  Timer? _cfgDebounce;
  bool _cfgInFlight = false;
  bool _cfgPending = false;
  bool engineConnected = false;

  // 运行状态(选中任务的实时进度,由 SSE + 任务行对账)
  bool running = false;
  String stage = '';
  int doneFrames = 0;
  int totalFrames = 0;
  final List<LogLine> logs = [];
  static const maxLogs = 500;

  StreamSubscription? _sseSub;
  int _retrySec = 1;

  Future<void> init() async {
    _connectEvents();
    config = await client.getConfig();
    lastOrigin = EditOrigin.remote;
    engineConnected = true;
    await refreshTasks();
    notifyListeners();
  }

  // ---- 任务 ----

  List<TaskInfo> taskList = [];
  int taskTotal = 0;

  /// 运行页当前查看的任务 id(0=未选择)。
  int selectedTaskId = 0;

  /// 点任务卡片后的跳转回调(由 Root 注入:切到运行页)。
  void Function(int id)? onOpenTask;

  /// 运行级失败的 Toast 通道:main.dart 接 TopToast。
  void Function(String msg)? onErrorToast;

  Future<void> refreshTasks() async {
    try {
      final w = await client.tasks();
      taskList = w.tasks;
      taskTotal = w.total;
      if (selectedTaskId > 0 && !taskList.any((t) => t.id == selectedTaskId)) {
        selectedTaskId = 0;
      }
      _syncRunViewFromTasks();
      notifyListeners();
    } catch (_) {}
  }

  /// 用任务行对账运行视图(切页/刷新时;SSE 为实时主通道)。
  void _syncRunViewFromTasks() {
    TaskInfo? run;
    for (final t in taskList) {
      if (t.running) {
        run = t;
        break;
      }
    }
    running = run != null;
    if (run != null) {
      stage = run.stage;
      doneFrames = run.done;
      totalFrames = run.total;
    }
  }

  /// 当前选中的任务。
  TaskInfo? get selectedTask {
    for (final t in taskList) {
      if (t.id == selectedTaskId) return t;
    }
    return null;
  }

  /// 引擎正在运行的任务(无则 null)。
  TaskInfo? get runningTask {
    for (final t in taskList) {
      if (t.running) return t;
    }
    return null;
  }

  /// 队列中等待自动调度的任务数。
  int get queuedCount => taskList.where((t) => t.queued).length;

  /// 选中任务并跳转运行页(任务卡片点击)。
  void openTask(int id) {
    selectedTaskId = id;
    notifyListeners();
    onOpenTask?.call(id);
  }

  /// 创建任务(快照当前配置;engine 非空时按三引擎选择覆盖快照);runNow=true 时创建即运行(忙则入队)并选中。
  Future<TaskInfo> createTask({
    String name = '',
    required String srcPath,
    String outName = '',
    String outDir = '',
    String? engine,
    bool runNow = false,
  }) async {
    final params = engine == null
        ? (jsonDecode(jsonEncode(config)) as Map).cast<String, dynamic>()
        : configWithEngine(config, engine);
    setPath(params, 'encode.crf', 15);
    setPath(params, 'output.dir', outDir.trim());
    final tk = await client.createTask(
      name: name,
      srcPath: srcPath,
      outName: outName,
      params: params,
      runNow: runNow,
    );
    await refreshTasks();
    selectedTaskId = tk.id;
    if (tk.running) {
      running = true;
      stage = '';
      doneFrames = 0;
      totalFrames = 0;
    }
    notifyListeners();
    return tk;
  }

  /// 运行(重跑)任务;引擎忙时由引擎入队,一轮结束自动调度。
  /// engine 非空时先按三引擎选择覆盖任务参数快照再运行。
  /// 返回 'started' | 'queued'。
  Future<String> runTask(int id, {String? engine}) async {
    Map<String, dynamic>? params;
    if (engine != null) {
      TaskInfo? tk;
      for (final t in taskList) {
        if (t.id == id) {
          tk = t;
          break;
        }
      }
      tk ??= await client.taskDetail(id);
      Map<String, dynamic> base = {};
      try {
        final v = jsonDecode(tk.paramsJson);
        if (v is Map) base = v.cast<String, dynamic>();
      } catch (_) {}
      params = configWithEngine(base.isEmpty ? config : base, engine);
    }
    final status = await client.runTask(id, params: params);
    await refreshTasks();
    if (status == 'started') {
      selectedTaskId = id;
      running = true;
      stage = '';
      doneFrames = 0;
      totalFrames = 0;
    }
    notifyListeners();
    return status;
  }

  /// 手动暂停任务:运行中 → 停止;排队中 → 移出队列。
  Future<void> stopTask(int id) async {
    await client.stopTask(id);
    await refreshTasks();
    notifyListeners();
  }

  Future<void> deleteTask(int id) async {
    await client.deleteTask(id);
    if (selectedTaskId == id) selectedTaskId = 0;
    await refreshTasks();
  }

  /// 任务详情刷新(运行页报告区)。
  Future<TaskInfo> taskDetail(int id) => client.taskDetail(id);

  // ---- 云端账号(在线去字幕内部通道) ----

  CloudStatus? cloud;
  bool cloudLoading = false;
  String? cloudError;

  /// 刷新云端账号状态(进新建任务对话框选在线引擎时调用)。
  Future<void> refreshCloud() async {
    cloudLoading = true;
    cloudError = null;
    notifyListeners();
    try {
      cloud = await client.cloudStatus();
    } catch (e) {
      cloud = null;
      cloudError = e is ApiException ? e.message : '$e';
    }
    cloudLoading = false;
    notifyListeners();
  }

  /// 登录云端账号;server 空串时引擎回落全局配置 online.server。成功后刷新状态。
  Future<void> loginCloud(String username, String password,
      {String server = ''}) async {
    await client.cloudLogin(username, password, server: server);
    await refreshCloud();
  }

  /// 清除本机云端凭据(不吊销远端会话)。
  Future<void> clearCloudCredential() async {
    await client.clearCloudCredential();
    cloud = null;
    await refreshCloud();
  }

  // ---- 配置编辑(防抖 1s 自动落盘) ----

  /// 表单控件改动:单键写回 + 防抖 1s 自动保存。
  void editConfig(String key, Object? value) {
    setPath(config, key, value);
    lastOrigin = EditOrigin.form;
    jsonError = '';
    _touchConfig();
  }

  /// JSON 编辑页改动:合法 object 才整体替换 config;非法仅置 jsonError。
  void setConfigJson(String text) {
    try {
      final v = jsonDecode(text);
      if (v is! Map) {
        jsonError = 'JSON 顶层不是 object,未写回';
        notifyListeners();
        return;
      }
      final next = v.cast<String, dynamic>();
      final schemaError = validateDesubConfig(next);
      if (schemaError != null) {
        jsonError = '$schemaError,未写回';
        notifyListeners();
        return;
      }
      config = next;
      lastOrigin = EditOrigin.json;
      jsonError = '';
      _touchConfig();
    } catch (_) {
      jsonError = 'JSON 语法错误,未写回';
      notifyListeners();
    }
  }

  /// 恢复历史版本/粘贴导入:整体替换并立即落盘。
  Future<void> replaceConfigAndSave(Map<String, dynamic> m) async {
    final schemaError = validateDesubConfig(m);
    if (schemaError != null) {
      jsonError = schemaError;
      savePhase = SavePhase.failed;
      saveError = schemaError;
      notifyListeners();
      return;
    }
    config = m;
    lastOrigin = EditOrigin.json;
    jsonError = '';
    _cfgDebounce?.cancel();
    _cfgPending = false;
    await _saveConfig();
  }

  /// JSON 编辑页投影文本(四空格缩进)。
  String get configPrettyJson =>
      const JsonEncoder.withIndent('    ').convert(config);

  /// 任意配置对象的缩进投影(版本管理页查看历史 payload 用)。
  String prettyJsonOf(Map<String, dynamic> m) =>
      const JsonEncoder.withIndent('    ').convert(m);

  /// 字段控件族 fontfile 下拉的占位(去字幕无字体概念,恒空列表)。
  Future<List<String>> loadFonts() async => const [];

  void _touchConfig() {
    notifyListeners();
    _cfgDebounce?.cancel();
    _cfgDebounce = Timer(const Duration(seconds: 1), () {
      // ignore: discarded_futures
      _saveConfig();
    });
  }

  Future<void> _saveConfig() async {
    if (_cfgInFlight) {
      _cfgPending = true; // 不叠发:在途时只记一笔,结束后补一发
      return;
    }
    _cfgInFlight = true;
    savePhase = SavePhase.saving;
    saveError = '';
    notifyListeners();
    try {
      final sent = jsonEncode(config);
      await client.putConfig(config);
      savePhase = SavePhase.saved;
      await _refreshAfterSave(sent);
    } on ConfigValidationException catch (e) {
      savePhase = SavePhase.failed;
      saveError = e.issues.map((i) => '${i.field} ${i.msg}').join('; ');
    } catch (e) {
      savePhase = SavePhase.failed;
      saveError = e is ApiException ? e.message : e.toString();
    } finally {
      _cfgInFlight = false;
      if (_cfgPending) {
        _cfgPending = false;
        _cfgDebounce?.cancel();
        _cfgDebounce = Timer(const Duration(seconds: 1), () {
          // ignore: discarded_futures
          _saveConfig();
        });
      }
      notifyListeners();
    }
  }

  /// 保存成功后回读引擎落盘的配置(与引擎语义对齐),期间有新编辑则不覆盖。
  Future<void> _refreshAfterSave(String sent) async {
    try {
      final fresh = await client.getConfig();
      if (jsonEncode(config) != sent) return;
      config = fresh;
      lastOrigin = EditOrigin.remote;
      jsonError = '';
    } catch (_) {}
  }

  // ---- SSE 事件 ----

  void _connectEvents() {
    _sseSub?.cancel();
    _sseSub = client.events().listen(
      _onEvent,
      onError: (_) {},
      onDone: _reconnect,
    );
  }

  void _reconnect() {
    engineConnected = false;
    notifyListeners();
    Future.delayed(Duration(seconds: _retrySec), () {
      _retrySec = (_retrySec * 2).clamp(1, 8);
      _connectEvents();
    });
  }

  /// 测试口:直接注入一条引擎事件(绕开 SSE 连接)。
  @visibleForTesting
  void handleEvent(EngineEvent ev) => _onEvent(ev);

  void _onEvent(EngineEvent ev) {
    _retrySec = 1;
    if (!engineConnected) engineConnected = true;
    switch (ev.type) {
      case 'log':
        _appendLog(ev.msg, LogKind.plain);
      case 'stage':
        stage = ev.stage;
        running = true;
        _appendLog('== ${ev.stage} ==', LogKind.stage);
        refreshTasks();
      case 'progress':
        running = true;
        if (ev.stage.isNotEmpty) stage = ev.stage;
        doneFrames = ev.done;
        totalFrames = ev.total;
      case 'queue':
        refreshTasks();
      case 'done':
        running = false;
        final failed = ev.status == 'failed';
        _appendLog(
          failed ? '==== 失败:${ev.msg} ====' : '==== ${ev.status} ====',
          failed ? LogKind.err : LogKind.ok,
        );
        if (failed && ev.msg.isNotEmpty) onErrorToast?.call(ev.msg);
        // 终态已落库:稍延迟避开读写竞态再对账
        Future.delayed(const Duration(milliseconds: 150), refreshTasks);
    }
    notifyListeners();
  }

  void _appendLog(String text, LogKind kind) {
    final now = DateTime.now();
    String two(int v) => v.toString().padLeft(2, '0');
    final ts = '${two(now.hour)}:${two(now.minute)}:${two(now.second)}';
    logs.add(LogLine(ts, text, kind));
    if (logs.length > maxLogs) {
      logs.removeRange(0, logs.length - maxLogs);
    }
  }

  /// 总进度:修补帧计数;无计数时 0。
  double get totalProgress =>
      totalFrames > 0 ? (doneFrames / totalFrames).clamp(0.0, 1.0) : 0;

  @override
  void dispose() {
    _sseSub?.cancel();
    _cfgDebounce?.cancel();
    super.dispose();
  }
}
