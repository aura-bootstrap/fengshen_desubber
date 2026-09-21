import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:http/http.dart' as http;

import 'models.dart';

/// EngineClient 拉起并驱动 Go 引擎(-serve 模式)。
class EngineClient {
  Process? _proc;
  late String baseUrl;
  final http.Client _http = http.Client();

  bool get running => _proc != null;

  /// 启动引擎,从 stdout 首行读取 PORT=n 完成握手。
  Future<void> start(String engineExe) async {
    if (_proc != null) return;
    _proc = await Process.start(
      engineExe,
      ['-serve', '-port', '0'],
      mode: ProcessStartMode.normal,
      workingDirectory: File(engineExe).parent.absolute.path,
    );
    final completer = Completer<void>();
    _proc!.stdout
        .transform(utf8.decoder)
        .transform(const LineSplitter())
        .listen((line) {
      if (line.startsWith('PORT=') && !completer.isCompleted) {
        baseUrl = 'http://127.0.0.1:${line.substring(5)}';
        completer.complete();
      }
    });
    _proc!.stderr.drain<void>();
    unawaited(_proc!.exitCode.then((_) => _proc = null));
    // 首跑遇杀软扫描全新引擎 exe 可能耗数秒,留足余量
    await completer.future.timeout(
      const Duration(seconds: 15),
      onTimeout: () => throw StateError('引擎启动超时(15s 未输出 PORT)'),
    );
  }

  Future<void> dispose() async {
    _proc?.kill();
    _proc = null;
    _http.close();
  }

  Uri _u(String path) => Uri.parse('$baseUrl$path');

  Future<Map<String, dynamic>> getConfig() async =>
      decodeJsonObject(await _get('/api/config'));

  /// 保存配置;校验失败抛 ConfigValidationException(含字段错误列表)。
  Future<void> putConfig(Map<String, dynamic> cfg) async {
    final resp = await _http.put(_u('/api/config'),
        headers: {'Content-Type': 'application/json'}, body: jsonEncode(cfg));
    if (resp.statusCode == 422) {
      final j = decodeJsonObject(utf8.decode(resp.bodyBytes));
      final errs = (j['errors'] as List? ?? [])
          .map((e) => FieldIssue(e['field'] as String, e['msg'] as String))
          .toList();
      throw ConfigValidationException(errs);
    }
    if (resp.statusCode != 200) {
      throw ApiException(resp.statusCode, utf8.decode(resp.bodyBytes));
    }
  }

  /// 配置历史列表(新→旧;「版本管理」页)。
  Future<List<ConfigHistEntry>> configHistory() async {
    final list = jsonDecode(await _get('/api/config/history')) as List;
    return list
        .map((e) => ConfigHistEntry.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  /// 取单条配置历史 payload(恢复时回填编辑器,走正常自动保存落盘)。
  Future<Map<String, dynamic>> configHistoryPayload(int version) async =>
      decodeJsonObject(await _get('/api/config/history/$version'));

  // ---- 任务 ----

  /// 任务列表(id 倒序;beforeId=往下翻更旧)。
  Future<TaskWindow> tasks({int beforeId = 0, int limit = 100}) async {
    final params = <String, String>{'limit': '$limit'};
    if (beforeId > 0) params['before_id'] = '$beforeId';
    final j = decodeJsonObject(
        await _get(Uri(path: '/api/tasks', queryParameters: params).toString()));
    return TaskWindow(
      (j['tasks'] as List? ?? [])
          .map((e) => TaskInfo.fromJson(e as Map<String, dynamic>))
          .toList(),
      (j['total'] as num?)?.toInt() ?? 0,
    );
  }

  /// 最近运行过的任务(无则 null)。
  Future<TaskInfo?> latestTask() async {
    final j = decodeJsonObject(await _get('/api/tasks/latest'));
    final b = j['task'];
    if (b == null) return null;
    return TaskInfo.fromJson(b as Map<String, dynamic>);
  }

  /// 任务详情。
  Future<TaskInfo> taskDetail(int id) async =>
      TaskInfo.fromJson(decodeJsonObject(await _get('/api/tasks/$id')));

  /// 创建任务;params 为当前配置快照(空则由引擎取磁盘配置)。
  /// run_now=true 时创建即运行(引擎忙自动入队)。
  Future<TaskInfo> createTask({
    String name = '',
    required String srcPath,
    String outName = '',
    Map<String, dynamic>? params,
    bool runNow = false,
  }) async {
    final j = await _postJson('/api/tasks', {
      'name': name,
      'src_path': srcPath,
      'out_name': outName,
      if (params != null) 'params': params,
      'run_now': runNow,
    });
    return TaskInfo.fromJson(j['task'] as Map<String, dynamic>);
  }

  /// 运行任务;引擎空闲→'started';忙→入队 'queued'(一轮结束自动调度)。
  /// params 非空时先覆盖任务参数快照(重跑换引擎)再运行。
  Future<String> runTask(int id, {Map<String, dynamic>? params}) async {
    final j = await _postJson(
        '/api/tasks/$id/run', params == null ? null : {'params': params});
    return j['status'] as String? ?? 'started';
  }

  /// 手动暂停:运行中→停止;排队中→移出队列。
  Future<void> stopTask(int id) async => _postJson('/api/tasks/$id/stop');

  Future<void> deleteTask(int id) async {
    final resp = await _http.delete(_u('/api/tasks/$id'));
    if (resp.statusCode != 200) {
      throw ApiException(resp.statusCode, utf8.decode(resp.bodyBytes));
    }
  }

  // ---- 卡密(在线去字幕) ----

  /// 卡密状态;远端不可达时引擎回本地缓存(stale=true)。
  Future<CardKeyStatus> cardkeyStatus() async =>
      CardKeyStatus.fromJson(decodeJsonObject(await _get('/api/cardkey/status')));

  /// 激活卡密;server 空串时引擎回落全局配置 online.server。
  Future<void> cardkeyActivate(String cardKey, {String server = ''}) async =>
      _postJson('/api/cardkey/activate',
          {'card_key': cardKey, if (server.isNotEmpty) 'server': server});

  /// 解绑:只删本地 keyfile,不解远端绑定。
  Future<void> cardkeyDeactivate() async =>
      _postJson('/api/cardkey/deactivate');

  Future<String> _get(String path) async {
    final resp = await _http.get(_u(path));
    if (resp.statusCode != 200) {
      throw ApiException(resp.statusCode, utf8.decode(resp.bodyBytes));
    }
    return utf8.decode(resp.bodyBytes);
  }

  Future<Map<String, dynamic>> _postJson(String path,
      [Map<String, dynamic>? body]) async {
    final resp = await _http.post(_u(path),
        headers: {'Content-Type': 'application/json'},
        body: body == null ? '{}' : jsonEncode(body));
    if (resp.statusCode >= 400) {
      final text = utf8.decode(resp.bodyBytes);
      String msg = text;
      try {
        msg = decodeJsonObject(text)['error'] as String? ?? text;
      } catch (_) {}
      throw ApiException(resp.statusCode, msg);
    }
    return decodeJsonObject(utf8.decode(resp.bodyBytes));
  }

  /// 订阅 SSE 事件流;断线由调用方决定重连。
  Stream<EngineEvent> events() async* {
    final req = http.Request('GET', _u('/api/events'));
    final resp = await _http.send(req);
    if (resp.statusCode != 200) {
      throw ApiException(resp.statusCode, 'SSE 连接失败');
    }
    final buf = StringBuffer();
    await for (final chunk in resp.stream.transform(utf8.decoder)) {
      buf.write(chunk);
      var text = buf.toString();
      int idx;
      while ((idx = text.indexOf('\n\n')) >= 0) {
        final frame = text.substring(0, idx);
        text = text.substring(idx + 2);
        for (final line in frame.split('\n')) {
          if (line.startsWith('data: ')) {
            try {
              yield EngineEvent.fromJson(
                  jsonDecode(line.substring(6)) as Map<String, dynamic>);
            } catch (_) {/* 跳过坏帧 */}
          }
        }
      }
      buf
        ..clear()
        ..write(text);
    }
  }
}

class FieldIssue {
  final String field, msg;
  const FieldIssue(this.field, this.msg);
}

class ConfigValidationException implements Exception {
  final List<FieldIssue> issues;
  const ConfigValidationException(this.issues);
  @override
  String toString() => issues.map((e) => '${e.field}: ${e.msg}').join('\n');
}

class ApiException implements Exception {
  final int status;
  final String message;
  const ApiException(this.status, this.message);
  @override
  String toString() => 'HTTP $status: $message';
}
