import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:http/http.dart' as http;

import 'models.dart';

/// EngineClient 拉起并驱动 Go 引擎(-serve 模式),契约与 fengshen-slicer 相同:
/// stdout 首行 PORT=n 完成握手。
class EngineClient {
  Process? _proc;
  late String baseUrl;
  final http.Client _http = http.Client();

  bool get running => _proc != null;

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

  Future<String> _get(String path) async {
    final resp = await _http.get(_u(path));
    if (resp.statusCode != 200) {
      throw ApiException(resp.statusCode, utf8.decode(resp.bodyBytes));
    }
    return utf8.decode(resp.bodyBytes);
  }

  Future<List<DesubTask>> listTasks() async {
    final j = jsonDecode(await _get('/api/tasks')) as Map<String, dynamic>;
    final list = (j['tasks'] as List? ?? [])
        .map((e) => DesubTask.fromJson(e as Map<String, dynamic>))
        .toList();
    return list;
  }

  Future<DesubTask> taskDetail(int id) async =>
      DesubTask.fromJson(jsonDecode(await _get('/api/tasks/$id')) as Map<String, dynamic>);

  Future<DesubTask> createTask({
    required String name,
    required String srcPath,
    required String outName,
    required Map<String, dynamic> params,
    required bool runNow,
  }) async {
    final resp = await _http.post(_u('/api/tasks'),
        headers: {'Content-Type': 'application/json'},
        body: jsonEncode({
          'name': name,
          'src_path': srcPath,
          'out_name': outName,
          'params': params,
          'run_now': runNow,
        }));
    if (resp.statusCode != 201) {
      throw ApiException(resp.statusCode, utf8.decode(resp.bodyBytes));
    }
    final j = jsonDecode(utf8.decode(resp.bodyBytes)) as Map<String, dynamic>;
    return DesubTask.fromJson(j['task'] as Map<String, dynamic>);
  }

  Future<void> runTask(int id) => _post('/api/tasks/$id/run');
  Future<void> stopTask(int id) => _post('/api/tasks/$id/stop');

  Future<void> _post(String path) async {
    final resp = await _http.post(_u(path));
    if (resp.statusCode >= 300) {
      throw ApiException(resp.statusCode, utf8.decode(resp.bodyBytes));
    }
  }

  Future<void> deleteTask(int id) async {
    final resp = await _http.delete(_u('/api/tasks/$id'));
    if (resp.statusCode != 200) {
      throw ApiException(resp.statusCode, utf8.decode(resp.bodyBytes));
    }
  }

  /// SSE 事件流;断线由调用方决定是否重连。
  Stream<EngineEvent> events() async* {
    final req = http.Request('GET', _u('/api/events'));
    final resp = await _http.send(req);
    if (resp.statusCode != 200) {
      throw ApiException(resp.statusCode, 'SSE connect failed');
    }
    final sb = StringBuffer();
    await for (final chunk in resp.stream.transform(utf8.decoder)) {
      sb.write(chunk);
      final s = sb.toString();
      var consumed = 0;
      var idx = s.indexOf('\n\n', consumed);
      while (idx >= 0) {
        final block = s.substring(consumed, idx);
        consumed = idx + 2;
        idx = s.indexOf('\n\n', consumed);
        for (final line in block.split('\n')) {
          if (line.startsWith('data: ')) {
            try {
              yield EngineEvent.fromJson(
                  jsonDecode(line.substring(6)) as Map<String, dynamic>);
            } catch (_) {}
          }
        }
      }
      final rest = s.substring(consumed);
      sb.clear();
      sb.write(rest);
    }
  }
}
