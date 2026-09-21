import 'dart:convert';

/// 与 Go 侧 config.json 完全一致的字典模型 + 声明式字段定义。
/// 不做强类型镜像,保证与 desub-engine 配置格式零偏差(round-trip 无损)。

Object? getPath(Map<String, dynamic> cfg, String path) {
  dynamic node = cfg;
  for (final part in path.split('.')) {
    if (node is! Map<String, dynamic>) return null;
    node = node[part];
  }
  return node;
}

/// 开发版三引擎选择(创建/重跑任务三选一;映射到配置键,随任务快照固化)。
const devEngineOptions = <String, String>{
  'temporal': '时域迁移 temporal(邻帧真实像素,快)',
  'delogo': '空间修补 delogo(单帧内修补)',
  'propainter': 'ProPainter 生成式(复杂遮挡,需 GPU)',
};

/// 从配置快照反推三引擎选择(propainter 强制路由 > delogo > 默认 temporal)。
String engineOfConfig(Map<String, dynamic> cfg) {
  final repair = cfg['repair'];
  final engine = repair is Map ? repair['engine'] : null;
  final force = repair is Map ? repair['force_engine'] : null;
  if (force == 'propainter') return 'propainter';
  if (engine == 'delogo') return 'delogo';
  return 'temporal';
}

/// 深拷贝配置并按三引擎选择覆盖 repair/enhance 键(propainter 走时域管线+强制路由)。
Map<String, dynamic> configWithEngine(Map<String, dynamic> cfg, String engine) {
  final next = (jsonDecode(jsonEncode(cfg)) as Map).cast<String, dynamic>();
  final repair =
      (next['repair'] as Map?)?.cast<String, dynamic>() ?? <String, dynamic>{};
  final enhance =
      (next['enhance'] as Map?)?.cast<String, dynamic>() ?? <String, dynamic>{};
  switch (engine) {
    case 'delogo':
      repair['engine'] = 'delogo';
      repair['force_engine'] = '';
    case 'propainter':
      repair['engine'] = 'temporal';
      repair['force_engine'] = 'propainter';
      enhance['propainter'] = true;
    default:
      repair['engine'] = 'temporal';
      repair['force_engine'] = 'motion';
  }
  next['repair'] = repair;
  next['enhance'] = enhance;
  return next;
}

void setPath(Map<String, dynamic> cfg, String path, Object? value) {
  final parts = path.split('.');
  Map<String, dynamic> node = cfg;
  for (var i = 0; i < parts.length - 1; i++) {
    final next = node[parts[i]];
    if (next is Map<String, dynamic>) {
      node = next;
    } else {
      final created = <String, dynamic>{};
      node[parts[i]] = created;
      node = created;
    }
  }
  node[parts.last] = value;
}

/// 数值解析容错:int/double/string 互通。
double? asDouble(Object? v) {
  if (v is num) return v.toDouble();
  if (v is String) return double.tryParse(v);
  return null;
}

enum FieldKind { text, integer, decimal, slider, range, chips, dropdown, multiline, path, bool, colorlist, fontfile, videofile }

class FieldDef {
  final String key; // json 路径
  final String label;
  final FieldKind kind;
  final String? hint;
  final List<String> options; // chips / dropdown
  final Map<String, String> optionLabels; // dropdown 选项显示名(空=显示原值)
  final double sliderMin, sliderMax; // slider 用
  final String? unit;
  final bool rangeInteger; // 双向滑条取整回写
  final bool expandableRange; // 上限可随输入扩展超出 sliderMax
  final double? hardMin, hardMax; // 编辑框极限保护(空=用滑杆行程)
  const FieldDef(this.key, this.label, this.kind,
      {this.hint,
      this.options = const [],
      this.optionLabels = const {},
      this.sliderMin = 0,
      this.sliderMax = 1,
      this.unit,
      this.rangeInteger = false,
      this.expandableRange = false,
      this.hardMin,
      this.hardMax});
}

class FieldGroup {
  final String title;
  final String caption;
  final List<FieldDef> fields;
  const FieldGroup(this.title, this.caption, this.fields);
}

class ConfigPage {
  final String navId;
  final String title;
  final String subtitle;
  final List<FieldGroup> groups;
  const ConfigPage(this.navId, this.title, this.subtitle, this.groups);
}

/// 字段联动置灰规则(配置页逐字段求值):
/// OCR 关→ocr_stride 置灰;ProPainter 关→pp_* 全组置灰;
/// 引擎非 temporal→motion/neighbors 置灰;非 delogo→pad 置灰。
bool fieldDisabled(Map<String, dynamic> cfg, String key) {
  if (key == 'detect.ocr_stride') {
    return getPath(cfg, 'detect.ocr') != true;
  }
  if (key.startsWith('enhance.pp_') &&
      getPath(cfg, 'enhance.propainter') != true) {
    return true;
  }
  if ((key == 'repair.motion' || key == 'repair.neighbors') &&
      getPath(cfg, 'repair.engine') != 'temporal') {
    return true;
  }
  if (key == 'repair.pad' && getPath(cfg, 'repair.engine') != 'delogo') {
    return true;
  }
  return false;
}

const configSchemaDomains = <String>{
  'detect', 'repair', 'enhance', 'encode', 'output',
};

/// 五域配置完整性校验(JSON 页写回/发布前把关)。
String? validateDesubConfig(Map<String, dynamic> value) {
  final missing =
      configSchemaDomains.where((key) => !value.containsKey(key)).toList();
  if (missing.isNotEmpty) return '配置缺少域:${missing.join(', ')}';
  final invalid =
      configSchemaDomains.where((key) => value[key] is! Map).toList();
  if (invalid.isNotEmpty) return '配置域必须是 object:${invalid.join(', ')}';
  return null;
}

const x264PresetOptions = [
  'ultrafast', 'superfast', 'veryfast', 'faster', 'fast',
  'medium', 'slow', 'slower', 'veryslow',
];

/// 与 desub-engine Config 五域一一对应(全部本机可调,随任务快照固化)。
final configPages = <ConfigPage>[
  ConfigPage('detect', '检测', '字幕带定位与事件归并参数(本机可调,随任务快照固化)', [
    FieldGroup('字幕带', '字幕出现的画面底部区域定位', [
      FieldDef('detect.band_start', '字幕带起点(画面高度比)', FieldKind.slider,
          sliderMin: 0.3, sliderMax: 0.95,
          hint: '字幕带上沿位置;0.58 表示从画面 58% 高度到底部'),
      FieldDef('detect.scene_threshold', '镜头切换阈值', FieldKind.slider,
          sliderMin: 0, sliderMax: 1,
          hint: 'scene_score 阈值,0=关闭镜头切分'),
    ]),
    FieldGroup('事件归并', '相邻帧字幕框归并为事件', [
      FieldDef('detect.max_gap', '事件内最大断帧', FieldKind.integer,
          hint: '同一事件允许的最大间隔帧数(0..120)'),
      FieldDef('detect.close_gap', '时域闭运算窗口(帧)', FieldKind.integer,
          hint: '同位置事件间隔小于此值时合并(0..600)'),
      FieldDef('detect.close_overlap', '闭运算重叠阈值', FieldKind.slider,
          sliderMin: 0, sliderMax: 1,
          hint: '相邻事件框相交占比较小者的最小比例'),
      FieldDef('detect.edge_pad', '事件前后扩帧', FieldKind.integer,
          hint: '每个事件前后各扩 N 帧,覆盖淡入淡出残留(0..60)'),
    ]),
    FieldGroup('OCR 融合', 'easyocr 旁车检测融合进掩码', [
      FieldDef('detect.ocr', '启用 OCR 融合', FieldKind.bool,
          hint: '需要本机 Python 环境与 scripts/ocr_boxes.py 依赖'),
      FieldDef('detect.ocr_stride', 'OCR 抽帧间隔', FieldKind.integer,
          hint: '每 N 帧跑一次 OCR(1..120)'),
    ]),
    FieldGroup('半透明条', '检测并解混半透明底衬条', [
      FieldDef('detect.alpha', '半透明条解混', FieldKind.bool,
          hint: '字幕下有半透明底衬时开启,按 alpha 解混而非修补'),
    ]),
  ]),
  ConfigPage('repair', '修复', '修补引擎与像素迁移参数(本机可调,随任务快照固化)', [
    FieldGroup('引擎', 'temporal 时域迁移 / delogo 空间修补', [
      FieldDef('repair.engine', '修补引擎', FieldKind.dropdown,
          options: ['temporal', 'delogo'],
          optionLabels: {'temporal': '时域迁移 (temporal)', 'delogo': '空间修补 (delogo)'}),
      FieldDef('repair.force_engine', '强制路由', FieldKind.dropdown,
          options: ['', 'motion', 'propainter'],
          optionLabels: {'': '自动路由', 'motion': '全部运动补偿 (motion)', 'propainter': '全部生成式 (propainter)'},
          hint: '覆盖事件级路由,所有事件强制走同一引擎'),
    ]),
    FieldGroup('时域迁移', 'temporal 引擎专用', [
      FieldDef('repair.motion', '运动补偿像素迁移', FieldKind.bool,
          hint: '按光流把邻帧像素对齐后迁移'),
      FieldDef('repair.neighbors', '时域邻域半径(帧)', FieldKind.integer,
          hint: '取前后各 N 帧做像素填充(1..60)'),
    ]),
    FieldGroup('空间修补', 'delogo 引擎专用', [
      FieldDef('repair.pad', '掩码外扩(px)', FieldKind.integer,
          hint: 'delogo 框额外外扩像素(0..64)'),
    ]),
    FieldGroup('质感', '修复区质感匹配', [
      FieldDef('repair.grain', '颗粒质感匹配', FieldKind.bool,
          hint: '修复区噪声/色度/块效应向源片对齐,减轻「塑料感」'),
    ]),
  ]),
  ConfigPage('enhance', '增强', '生成式与像素级精修旁车(本机可调,随任务快照固化)', [
    FieldGroup('ProPainter', '生成式修复旁车(高风险事件)', [
      FieldDef('enhance.propainter', '启用 ProPainter', FieldKind.bool,
          hint: '需要本机 Python 环境、ProPainter 依赖与 GPU'),
      FieldDef('enhance.pp_mask_dilation', '掩码膨胀(px)', FieldKind.integer,
          hint: '验证值 8:去描边晕染'),
      FieldDef('enhance.pp_tight_dilate', '笔画级掩码膨胀(px)', FieldKind.integer,
          hint: '验证值 7:覆盖字形抗锯齿与深色描边'),
      FieldDef('enhance.pp_raft_iter', 'RAFT 迭代数', FieldKind.integer,
          hint: '验证值 32'),
      FieldDef('enhance.pp_neighbor_length', '局部邻域长度', FieldKind.integer,
          hint: '验证值 20'),
      FieldDef('enhance.pp_concurrency', '分块并发数', FieldKind.integer,
          hint: 'GPU  bound;16GB 卡实测 K=2 更慢(1..8)'),
    ]),
    FieldGroup('像素级精修', '掩码与人脸后处理', [
      FieldDef('enhance.sam2', 'SAM2 掩码精修', FieldKind.bool,
          hint: '修补前把掩码精修到像素级(需 SAM2 旁车)'),
      FieldDef('enhance.face_restore', '人脸修复', FieldKind.bool,
          hint: '修复区内人脸经 GFPGAN 复原(需旁车)'),
      FieldDef('enhance.vlm_qc', 'VLM 残留复核', FieldKind.bool,
          hint: 'verify 阶段残留框经 VLM 二次判定(需本地 ollama)'),
    ]),
  ]),
  ConfigPage('encode', '编码', 'x264 重编码参数(本机可调,随任务快照固化)', [
    FieldGroup('x264', '单遍重编码画质与速度', [
      FieldDef('encode.crf', 'CRF', FieldKind.integer,
          hint: '0..51,越小画质越高;推荐 17'),
      FieldDef('encode.preset', '档位', FieldKind.dropdown,
          options: x264PresetOptions,
          hint: '越慢压缩率越高;推荐 medium'),
    ]),
  ]),
  ConfigPage('output', '产出', '校验与风险标记(本机可调,随任务快照固化)', [
    FieldGroup('校验', '产物回检', [
      FieldDef('output.verify', '产物复检', FieldKind.bool,
          hint: '对输出视频再跑一次检测,报告残留'),
      FieldDef('output.risk_coverage', '高风险覆盖率阈值', FieldKind.slider,
          sliderMin: 0, sliderMax: 1,
          hint: '真实像素覆盖率低于此值的事件标记为高风险'),
    ]),
  ]),
];

/// 版本管理页只展示配置参数的白名单:配置页字段顶层键。
final Set<String> configTopKeys = {
  for (final p in configPages)
    for (final g in p.groups)
      for (final f in g.fields) f.key.split('.').first,
};

/// GET /api/tasks 的一页窗口。
class TaskWindow {
  final List<TaskInfo> tasks;
  final int total;
  const TaskWindow(this.tasks, this.total);
}

/// 任务(视频 + 参数快照 + 运行状态;对应引擎 tasks 表一行)。
class TaskInfo {
  final int id;
  final String name;
  final String srcPath;
  final String outName;
  final String paramsJson;
  final String status; // pending|queued|running|succeeded|failed|stopped
  final String stage; // probe|cuts|detect|ocr|engine|repair|verify
  final int done, total;
  final String workDir;
  final String reportJson;
  final String error;
  final DateTime createdAt;
  const TaskInfo(this.id, this.name, this.srcPath, this.outName,
      this.paramsJson, this.status, this.stage, this.done, this.total,
      this.workDir, this.reportJson, this.error, this.createdAt);

  factory TaskInfo.fromJson(Map<String, dynamic> j) => TaskInfo(
        (j['id'] as num?)?.toInt() ?? 0,
        j['name'] as String? ?? '',
        j['src_path'] as String? ?? '',
        j['out_name'] as String? ?? '',
        j['params_json'] as String? ?? '',
        j['status'] as String? ?? 'pending',
        j['stage'] as String? ?? '',
        (j['done'] as num?)?.toInt() ?? 0,
        (j['total'] as num?)?.toInt() ?? 0,
        j['work_dir'] as String? ?? '',
        j['report_json'] as String? ?? '',
        j['error'] as String? ?? '',
        DateTime.tryParse(j['created_at'] as String? ?? '') ??
            DateTime.fromMillisecondsSinceEpoch(0),
      );

  double get progress => total > 0 ? (done / total).clamp(0.0, 1.0) : 0;

  bool get running => status == 'running';
  bool get queued => status == 'queued';

  String get statusLabel => switch (status) {
        'queued' => '排队中',
        'running' => '运行中',
        'succeeded' => '已完成',
        'failed' => '失败',
        'stopped' => '已暂停',
        _ => '待启动',
      };

  String get stageLabel => switch (stage) {
        'probe' => '探测',
        'cuts' => '镜头切分',
        'detect' => '检测',
        'ocr' => 'OCR',
        'engine' => '引擎路由',
        'repair' => '修补',
        'verify' => '复检',
        _ => stage,
      };

  String get createdLabel {
    if (createdAt.millisecondsSinceEpoch == 0) return '';
    final d = createdAt.toLocal();
    String two(int v) => v.toString().padLeft(2, '0');
    return '${d.month}-${two(d.day)} ${two(d.hour)}:${two(d.minute)}';
  }
}

/// 引擎事件(SSE):progress|stage|log|queue|done。
class EngineEvent {
  final String type;
  final int taskId;
  final String stage;
  final int done, total;
  final String msg;
  final String status;
  EngineEvent(this.type, this.taskId, this.stage, this.done, this.total,
      this.msg, this.status);

  factory EngineEvent.fromJson(Map<String, dynamic> j) => EngineEvent(
        j['type'] as String? ?? 'log',
        (j['task_id'] as num?)?.toInt() ?? 0,
        j['stage'] as String? ?? '',
        (j['done'] as num?)?.toInt() ?? 0,
        (j['total'] as num?)?.toInt() ?? 0,
        j['msg'] as String? ?? '',
        j['status'] as String? ?? '',
      );
}

Map<String, dynamic> decodeJsonObject(String body) =>
    jsonDecode(body) as Map<String, dynamic>;

/// 配置历史索引条目(引擎 sqlite config_history;「版本管理」列表用)。
class ConfigHistEntry {
  final int version;
  final int savedAt;
  ConfigHistEntry({required this.version, required this.savedAt});

  factory ConfigHistEntry.fromJson(Map<String, dynamic> j) => ConfigHistEntry(
        version: (j['version'] as num).toInt(),
        savedAt: (j['saved_at'] as num).toInt(),
      );

  String get savedLabel {
    final d = DateTime.fromMillisecondsSinceEpoch(savedAt * 1000);
    String two(int v) => v.toString().padLeft(2, '0');
    return '${d.year}-${two(d.month)}-${two(d.day)} ${two(d.hour)}:${two(d.minute)}:${two(d.second)}';
  }
}
