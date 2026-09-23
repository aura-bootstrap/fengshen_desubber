/// 后端 /api/tasks 行模型与 SSE 事件模型。
class DesubTask {
  final int id;
  final String name;
  final String srcPath;
  final String outName;
  final String paramsJson;
  final String status; // pending|queued|running|succeeded|failed|stopped
  final String
  stage; // 本地: probe|cuts|detect|repair|engine|verify;在线: upload|cloud|download
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

  /// 在线任务(params 快照 online:true)走云端三阶段,否则本地六阶段管线。
  bool get isOnline => paramsJson.contains('"online":true');

  static const localStages = [
    'probe',
    'cuts',
    'detect',
    'repair',
    'engine',
    'verify',
  ];
  static const onlineStages = ['upload', 'cloud', 'download'];
  List<String> get stages => isOnline ? onlineStages : localStages;

  /// 阶段序列中的位置,用于阶段时间线;-1 = 尚未进入任何阶段。
  int get stageIndex => stages.indexOf(stage);

  /// 当前阶段内进度(0..1):repair 是帧计数,upload/download 是字节计数,
  /// 无计数的阶段(probe/cuts/cloud 等)在进行中按 0 计,过后由阶段位置推进。
  double get stageFraction {
    if (status == 'succeeded') return 1;
    if (total <= 0) return 0;
    return (done / total).clamp(0.0, 1.0);
  }

  /// 总进度 = (已过阶段数 + 当前阶段内进度) / 阶段总数,每个阶段都有百分比。
  double get progress {
    if (status == 'succeeded') return 1;
    final i = stageIndex;
    if (i < 0) return 0;
    return ((i + stageFraction) / stages.length).clamp(0.0, 1.0);
  }

  static String labelOf(String stage) => switch (stage) {
    'probe' => '探测',
    'cuts' => '镜头切分',
    'detect' => '字幕检测',
    'repair' => '修复',
    'engine' => '合成输出',
    'verify' => '复检',
    'upload' => '上传',
    'cloud' => '云端处理',
    'download' => '下载成片',
    _ => '',
  };

  String get stageLabel {
    final l = labelOf(stage);
    if (l.isNotEmpty) return l;
    return status == 'succeeded' ? '完成' : '等待';
  }
}

class EngineEvent {
  final String type; // progress|stage|log|queue|done|account
  final int taskId;
  final String stage;
  final int done;
  final int total;
  final String msg;
  final String status;
  final int? balance;

  const EngineEvent({
    required this.type,
    this.taskId = 0,
    this.stage = '',
    this.done = 0,
    this.total = 0,
    this.msg = '',
    this.status = '',
    this.balance,
  });

  factory EngineEvent.fromJson(Map<String, dynamic> j) => EngineEvent(
    type: j['type'] as String? ?? '',
    taskId: (j['task_id'] as num? ?? 0).toInt(),
    stage: j['stage'] as String? ?? '',
    done: (j['done'] as num? ?? 0).toInt(),
    total: (j['total'] as num? ?? 0).toInt(),
    msg: j['msg'] as String? ?? '',
    status: j['status'] as String? ?? '',
    balance: (j['balance'] as num?)?.toInt(),
  );
}

/// 机器账户状态(/api/account/status)。充值卡只作为核销与请求凭据。
class MachineAccountStatus {
  final bool linked;
  final int? balance;
  final bool balanceAvailable;
  final String machineHash;
  final bool degraded;
  final String error;

  const MachineAccountStatus({
    required this.linked,
    required this.balance,
    required this.balanceAvailable,
    required this.machineHash,
    required this.degraded,
    required this.error,
  });

  factory MachineAccountStatus.fromJson(Map<String, dynamic> j) =>
      MachineAccountStatus(
        linked: j['linked'] as bool? ?? false,
        balance: (j['balance'] as num?)?.toInt(),
        balanceAvailable: j['balance_available'] as bool? ?? false,
        machineHash: j['machine_hash'] as String? ?? '',
        degraded: j['degraded'] as bool? ?? false,
        error: j['error'] as String? ?? '',
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
