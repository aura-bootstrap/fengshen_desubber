/// 后端 /api/tasks 行模型与 SSE 事件模型。
class DesubTask {
  final int id;
  final String name;
  final String srcPath;
  final String outName;
  final String paramsJson;
  final String status; // pending|queued|running|succeeded|failed|stopped
  final String stage; // probe|cuts|detect|repair|engine|verify
  final int done;
  final int total;
  final String workDir;
  final String reportJson;
  final String error;
  final DateTime? createdAt;
  final DateTime? updatedAt;

  const DesubTask({
    required this.id,
    required this.name,
    required this.srcPath,
    required this.outName,
    required this.paramsJson,
    required this.status,
    required this.stage,
    required this.done,
    required this.total,
    required this.workDir,
    required this.reportJson,
    required this.error,
    this.createdAt,
    this.updatedAt,
  });

  factory DesubTask.fromJson(Map<String, dynamic> j) => DesubTask(
        id: (j['id'] as num).toInt(),
        name: j['name'] as String? ?? '',
        srcPath: j['src_path'] as String? ?? '',
        outName: j['out_name'] as String? ?? '',
        paramsJson: j['params_json'] as String? ?? '',
        status: j['status'] as String? ?? '',
        stage: j['stage'] as String? ?? '',
        done: (j['done'] as num? ?? 0).toInt(),
        total: (j['total'] as num? ?? 0).toInt(),
        workDir: j['work_dir'] as String? ?? '',
        reportJson: j['report_json'] as String? ?? '',
        error: j['error'] as String? ?? '',
        createdAt: DateTime.tryParse(j['created_at'] as String? ?? ''),
        updatedAt: DateTime.tryParse(j['updated_at'] as String? ?? ''),
      );

  bool get active => status == 'running' || status == 'queued';

  double get progress {
    if (status == 'succeeded') return 1;
    if (total <= 0) return 0;
    return (done / total).clamp(0.0, 1.0);
  }

  /// 阶段序列中的位置(0..5),用于阶段时间线。
  static const stageOrder = ['probe', 'cuts', 'detect', 'repair', 'engine', 'verify'];
  int get stageIndex => stageOrder.indexOf(stage);

  String get stageLabel => switch (stage) {
        'probe' => '探测',
        'cuts' => '镜头切分',
        'detect' => '字幕检测',
        'repair' => '修复',
        'engine' => '合成输出',
        'verify' => '复检',
        _ => status == 'succeeded' ? '完成' : '等待',
      };
}

class EngineEvent {
  final String type; // progress|stage|log|queue|done
  final int taskId;
  final String stage;
  final int done;
  final int total;
  final String msg;
  final String status;

  const EngineEvent({
    required this.type,
    this.taskId = 0,
    this.stage = '',
    this.done = 0,
    this.total = 0,
    this.msg = '',
    this.status = '',
  });

  factory EngineEvent.fromJson(Map<String, dynamic> j) => EngineEvent(
        type: j['type'] as String? ?? '',
        taskId: (j['task_id'] as num? ?? 0).toInt(),
        stage: j['stage'] as String? ?? '',
        done: (j['done'] as num? ?? 0).toInt(),
        total: (j['total'] as num? ?? 0).toInt(),
        msg: j['msg'] as String? ?? '',
        status: j['status'] as String? ?? '',
      );
}

/// 卡密状态(/api/cardkey/status),在线去字幕引擎使用。
class CardKeyStatus {
  final bool activated;
  final String server;
  final String masked; // 脱敏卡号,形如 ABCDE…Z
  final int credits; // 剩余点数
  final String machineHash;
  final bool degraded; // 引擎侧降级(如云端暂不可达,余额为缓存值)
  final bool stale; // 状态缓存过期

  const CardKeyStatus({
    required this.activated,
    required this.server,
    required this.masked,
    required this.credits,
    required this.machineHash,
    required this.degraded,
    required this.stale,
  });

  factory CardKeyStatus.fromJson(Map<String, dynamic> j) => CardKeyStatus(
        activated: j['activated'] as bool? ?? false,
        server: j['server'] as String? ?? '',
        masked: j['masked'] as String? ?? '',
        credits: (j['credits'] as num? ?? 0).toInt(),
        machineHash: j['machine_hash'] as String? ?? '',
        degraded: j['degraded'] as bool? ?? false,
        stale: j['stale'] as bool? ?? false,
      );
}

class ApiException implements Exception {
  final int statusCode;
  final String body;
  ApiException(this.statusCode, this.body);
  @override
  String toString() {
    try {
      final m = RegExp(r'"error"\s*:\s*"([^"]+)"').firstMatch(body);
      if (m != null) return m.group(1)!;
    } catch (_) {}
    return 'HTTP $statusCode';
  }
}
