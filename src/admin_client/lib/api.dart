import 'dart:convert';

import 'package:http/http.dart' as http;

class ApiException implements Exception {
  final int status;
  final String message;
  ApiException(this.status, this.message);

  bool get unauthorized => status == 401;
  bool get forbidden => status == 403;
  bool get conflict => status == 409;

  @override
  String toString() => message;
}

class CardInfo {
  final int id;
  final String name;
  final String codeMasked;
  final String batch;
  final int balance;
  final String status;
  final int createdAt;

  CardInfo.fromJson(Map<String, dynamic> m)
      : id = m['id'] as int? ?? 0,
        name = m['name'] as String? ?? '',
        codeMasked = m['code_masked'] as String? ?? '',
        batch = m['batch'] as String? ?? '',
        balance = m['balance'] as int? ?? 0,
        status = m['status'] as String? ?? '',
        createdAt = m['created_at'] as int? ?? 0;
}

class AuditEntry {
  final int ts;
  final String scope;
  final String actor;
  final String action;
  final String target;
  final String detail;
  final bool ok;
  final String taskId;
  final String machineHash;
  final int cardId;
  final String cardMasked;
  final String provider;
  final String sourceName;
  final String sourcePath;
  final int durationSec;
  final int cost;
  final int? balanceAfter;
  final String status;
  final String error;

  AuditEntry.fromJson(Map<String, dynamic> m)
      : ts = m['ts'] as int? ?? 0,
        scope = m['scope'] as String? ?? '',
        actor = m['actor'] as String? ?? '',
        action = m['action'] as String? ?? '',
        target = m['target'] as String? ?? '',
        detail = m['detail'] as String? ?? '',
        ok = m['ok'] as bool? ?? false,
        taskId = m['task_id'] as String? ?? '',
        machineHash = m['machine_hash'] as String? ?? '',
        cardId = m['card_id'] as int? ?? 0,
        cardMasked = m['card_masked'] as String? ?? '',
        provider = m['provider'] as String? ?? '',
        sourceName = m['source_name'] as String? ?? '',
        sourcePath = m['source_path'] as String? ?? '',
        durationSec = m['duration_sec'] as int? ?? 0,
        cost = m['cost'] as int? ?? 0,
        balanceAfter = m['balance_after'] as int?,
        status = m['status'] as String? ?? '',
        error = m['error'] as String? ?? '';
}

class CreditTx {
  final int id;
  final String taskId;
  final String kind;
  final int amount;
  final int balanceAfter;
  final String createdAt;

  CreditTx.fromJson(Map<String, dynamic> m)
      : id = m['ID'] as int? ?? 0,
        taskId = m['TaskID'] as String? ?? '',
        kind = m['Kind'] as String? ?? '',
        amount = m['Amount'] as int? ?? 0,
        balanceAfter = m['BalanceAfter'] as int? ?? 0,
        createdAt = m['CreatedAt'] as String? ?? '';
}

/// 管理员账号(服务端 Account 落库形态的脱敏投影,永不含口令)。
class AccountInfo {
  final String username;
  final String role;
  final String status;
  final int version; // 乐观锁基线,写操作回传
  final int createdAt;

  AccountInfo.fromJson(Map<String, dynamic> m)
      : username = m['username'] as String? ?? '',
        role = m['role'] as String? ?? '',
        status = m['status'] as String? ?? '',
        version = m['version'] as int? ?? 0,
        createdAt = m['created_at'] as int? ?? 0;

  bool get isRoot => role == 'root';
  bool get disabled => status == 'disabled';
}

/// 去字幕计费服务端管理 API:Bearer 会话令牌鉴权(/v1/admin/login 换取)。
class AdminApi {
  final String base;
  final String token;
  final String username;
  final String role; // root|admin
  final http.Client _c = http.Client();

  AdminApi(this.base, this.token, {this.username = '', this.role = ''});

  /// 用户名+密码换会话令牌;成功返回带令牌的 AdminApi。
  static Future<AdminApi> login(
      String base, String username, String password) async {
    final anon = AdminApi(base, '');
    try {
      final d = await anon._req('POST', '/v1/admin/login',
          body: {'username': username, 'password': password});
      return AdminApi(base, d['token'] as String? ?? '',
          username: d['username'] as String? ?? '',
          role: d['role'] as String? ?? '');
    } finally {
      anon.dispose();
    }
  }

  Uri _u(String path, [Map<String, String>? q]) =>
      Uri.parse('$base$path').replace(queryParameters: q);

  Future<dynamic> _req(String method, String path,
      {Object? body, Map<String, String>? q}) async {
    final req = http.Request(method, _u(path, q));
    req.headers['Authorization'] = 'Bearer $token';
    if (body != null) {
      req.headers['Content-Type'] = 'application/json';
      req.body = jsonEncode(body);
    }
    final res = await http.Response.fromStream(await _c.send(req));
    final decoded = res.body.isEmpty ? null : jsonDecode(res.body);
    if (res.statusCode >= 400) {
      final msg =
          decoded is Map ? (decoded['error'] ?? 'HTTP ${res.statusCode}') : 'HTTP ${res.statusCode}';
      throw ApiException(res.statusCode, '$msg');
    }
    return decoded;
  }

  void dispose() => _c.close();

  Future<List<CardInfo>> listCards({String batch = ''}) async {
    final d = await _req('GET', '/v1/admin/cards',
        q: batch.isEmpty ? null : {'batch': batch});
    return [for (final m in (d['cards'] as List? ?? [])) CardInfo.fromJson(m)];
  }

  /// 发卡,明文卡面仅此一次返回。
  Future<List<Map<String, dynamic>>> generate(
      int count, int credits, String batch, String name) async {
    final d = await _req('POST', '/v1/admin/cards/generate',
        body: {'count': count, 'credits': credits, 'batch': batch, 'name': name});
    return [for (final m in (d['cards'] as List? ?? [])) Map<String, dynamic>.from(m)];
  }

  Future<int> recharge(String card, int credits) async {
    final d = await _req('POST', '/v1/admin/cards/recharge',
        body: {'card': card, 'credits': credits});
    return d['balance'] as int? ?? 0;
  }

  Future<void> setStatus(String action, {String card = '', String batch = ''}) =>
      _req('POST', '/v1/admin/cards/$action',
          body: {'card': card, 'batch': batch});

  Future<void> unbind({int cardId = 0, String code = ''}) =>
      _req('POST', '/v1/admin/cards/unbind',
          body: {'card_id': cardId, 'code': code});

  Future<List<AuditEntry>> audit() async {
    final d = await _req('GET', '/v1/admin/audit');
    return [for (final m in (d['audit'] as List? ?? [])) AuditEntry.fromJson(m)];
  }

  Future<List<CreditTx>> transactions(int cardId) async {
    final d = await _req('GET', '/v1/admin/transactions',
        q: {'user_id': '$cardId'});
    return [for (final m in (d as List? ?? [])) CreditTx.fromJson(m)];
  }

  Future<Map<String, dynamic>> cardStatus(String code) async =>
      Map<String, dynamic>.from(await _req('GET', '/v1/cards/$code/status'));

  /// 当前会话身份(启动校验令牌存活 + 取角色)。
  Future<Map<String, dynamic>> me() async =>
      Map<String, dynamic>.from(await _req('GET', '/v1/admin/me'));

  /// 登出:服务端 bump 口令纪元,全部在途会话即刻失效。
  Future<void> logout() => _req('POST', '/v1/admin/logout');

  /// 改本人密码(成功即吊销旧会话,调用方须重新登录)。
  Future<void> changePassword(String oldPassword, String newPassword) =>
      _req('POST', '/v1/admin/password',
          body: {'old_password': oldPassword, 'new_password': newPassword});

  /// 账号列表(仅 root)。
  Future<List<AccountInfo>> listAccounts() async {
    final d = await _req('GET', '/v1/admin/accounts');
    return [
      for (final m in (d['accounts'] as List? ?? [])) AccountInfo.fromJson(m)
    ];
  }

  /// 建普通管理员(仅 root;API 不可建 root)。
  Future<void> createAccount(String username, String password) =>
      _req('POST', '/v1/admin/accounts',
          body: {'username': username, 'password': password});

  /// 禁用/启用管理员(仅 root;禁用即吊销其会话)。version 为乐观锁基线。
  Future<void> setAccountStatus(String username, String status, int version) =>
      _req('POST', '/v1/admin/accounts/$username/status',
          body: {'status': status, 'version': version});

  /// 重置管理员密码(仅 root;旧会话随之失效)。
  Future<void> resetAccountPassword(
          String username, String password, int version) =>
      _req('POST', '/v1/admin/accounts/$username/password',
          body: {'password': password, 'version': version});

  /// 硬删管理员(仅 root;root 不可删)。
  Future<void> deleteAccount(String username, int version) =>
      _req('POST', '/v1/admin/accounts/$username/delete',
          body: {'version': version});
}
