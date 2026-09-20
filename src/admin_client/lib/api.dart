import 'dart:convert';

import 'package:http/http.dart' as http;

class ApiException implements Exception {
  final int status;
  final String message;
  ApiException(this.status, this.message);

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
  final String actor;
  final String action;
  final String target;
  final String detail;
  final bool ok;

  AuditEntry.fromJson(Map<String, dynamic> m)
      : ts = m['ts'] as int? ?? 0,
        actor = m['actor'] as String? ?? '',
        action = m['action'] as String? ?? '',
        target = m['target'] as String? ?? '',
        detail = m['detail'] as String? ?? '',
        ok = m['ok'] as bool? ?? false;
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

/// 去字幕计费服务端管理 API:Bearer 管理 token 鉴权。
class AdminApi {
  final String base;
  final String token;
  final http.Client _c = http.Client();

  AdminApi(this.base, this.token);

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
    final d = await _req('GET', '/v1/admin/cards/audit');
    return [for (final m in (d['audit'] as List? ?? [])) AuditEntry.fromJson(m)];
  }

  Future<List<CreditTx>> transactions(int cardId) async {
    final d = await _req('GET', '/v1/admin/transactions',
        q: {'user_id': '$cardId'});
    return [for (final m in (d as List? ?? [])) CreditTx.fromJson(m)];
  }

  Future<Map<String, dynamic>> cardStatus(String code) async =>
      Map<String, dynamic>.from(await _req('GET', '/v1/cards/$code/status'));
}
