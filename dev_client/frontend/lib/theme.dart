import 'package:flutter/material.dart';

/// 非颜色常量(不随明暗变化)。
abstract final class AppConst {
  static const radiusCard = 12.0;
  static const radiusCtrl = 8.0;
  static const fontFamily = 'Microsoft YaHei';
  static const fontMono = 'Cascadia Code';
}

/// 主题色板(ThemeExtension),与 fengshen-slicer dev_client 同一套调色。
class AppTokens extends ThemeExtension<AppTokens> {
  final Color bg, surface, border, ink, dim, faint;
  final Color primary, primaryInk, primarySoft;
  final Color success, successSoft, warn, danger, dangerSoft;
  final Color consoleBg;
  final Color brandA, brandB;

  const AppTokens({
    required this.bg,
    required this.surface,
    required this.border,
    required this.ink,
    required this.dim,
    required this.faint,
    required this.primary,
    required this.primaryInk,
    required this.primarySoft,
    required this.success,
    required this.successSoft,
    required this.warn,
    required this.danger,
    required this.dangerSoft,
    required this.consoleBg,
    required this.brandA,
    required this.brandB,
  });

  static const light = AppTokens(
    bg: Color(0xFFF0F2F5),
    surface: Color(0xFFFFFFFF),
    border: Color(0xFFB4BDCC),
    ink: Color(0xFF161C28),
    dim: Color(0xFF5B6472),
    faint: Color(0xFF66717F),
    primary: Color(0xFF3B5BFD),
    primaryInk: Color(0xFF2E49D6),
    primarySoft: Color(0xFFDEE6FF),
    success: Color(0xFF1E9E62),
    successSoft: Color(0xFFE4F6EC),
    warn: Color(0xFFC98A0B),
    danger: Color(0xFFC03535),
    dangerSoft: Color(0xFFFBEAEA),
    consoleBg: Color(0xFF101623),
    brandA: Color(0xFF3B5BFD),
    brandB: Color(0xFF2E49D6),
  );

  static const dark = AppTokens(
    bg: Color(0xFF0F141C),
    surface: Color(0xFF161D29),
    border: Color(0xFF263040),
    ink: Color(0xFFE9EDF5),
    dim: Color(0xFFB4BDCC),
    faint: Color(0xFF98A2B6),
    primary: Color(0xFF6B85FF),
    primaryInk: Color(0xFFC3CFFF),
    primarySoft: Color(0xFF24305C),
    success: Color(0xFF3FBF7F),
    successSoft: Color(0xFF15362B),
    warn: Color(0xFFE0A83C),
    danger: Color(0xFFE26868),
    dangerSoft: Color(0xFF3D2020),
    consoleBg: Color(0xFF0A0F17),
    brandA: Color(0xFF5E79F0),
    brandB: Color(0xFF3B5BFD),
  );

  @override
  AppTokens copyWith() => this;

  @override
  AppTokens lerp(AppTokens? other, double t) => t < 0.5 ? this : other!;
}

extension AppTokensX on BuildContext {
  AppTokens get tokens => Theme.of(this).extension<AppTokens>()!;
}

ThemeData _build(AppTokens t, Brightness brightness) {
  final isDark = brightness == Brightness.dark;
  final base =
      isDark ? ThemeData.dark(useMaterial3: true) : ThemeData.light(useMaterial3: true);
  final scheme = (isDark ? ColorScheme.dark : ColorScheme.light)(
    primary: t.primary,
    onPrimary: isDark ? const Color(0xFF101528) : Colors.white,
    primaryContainer: t.primarySoft,
    onPrimaryContainer: t.primaryInk,
    surface: t.bg,
    onSurface: t.ink,
    surfaceContainerHighest: t.surface,
    outline: t.border,
    outlineVariant: t.border,
    error: t.danger,
  );
  return base.copyWith(
    colorScheme: scheme,
    scaffoldBackgroundColor: t.bg,
    textTheme: base.textTheme.apply(fontFamily: AppConst.fontFamily),
    extensions: [t],
    cardTheme: CardThemeData(
      color: t.surface,
      elevation: 0.6,
      margin: EdgeInsets.zero,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(AppConst.radiusCard),
        side: BorderSide(color: t.border),
      ),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        backgroundColor: t.primary,
        foregroundColor: isDark ? const Color(0xFF101528) : Colors.white,
        textStyle: const TextStyle(
            fontSize: 13, fontWeight: FontWeight.w600, fontFamily: AppConst.fontFamily),
        padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 12),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(AppConst.radiusCtrl)),
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: OutlinedButton.styleFrom(
        foregroundColor: t.dim,
        textStyle: const TextStyle(
            fontSize: 13, fontWeight: FontWeight.w600, fontFamily: AppConst.fontFamily),
        padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 12),
        side: BorderSide(color: t.border),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(AppConst.radiusCtrl)),
      ),
    ),
    inputDecorationTheme: InputDecorationTheme(
      isDense: true,
      filled: true,
      fillColor: t.surface,
      contentPadding: const EdgeInsets.symmetric(horizontal: 13, vertical: 11),
      border: OutlineInputBorder(
        borderRadius: BorderRadius.circular(AppConst.radiusCtrl),
        borderSide: BorderSide(color: t.border),
      ),
      enabledBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(AppConst.radiusCtrl),
        borderSide: BorderSide(color: t.border),
      ),
      focusedBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(AppConst.radiusCtrl),
        borderSide: BorderSide(color: t.primary, width: 1.5),
      ),
      hintStyle: TextStyle(fontSize: 12, color: t.faint),
    ),
    dividerTheme: DividerThemeData(color: t.border, thickness: 1, space: 1),
    snackBarTheme: SnackBarThemeData(
      behavior: SnackBarBehavior.floating,
      backgroundColor: isDark ? const Color(0xFF263040) : null,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(10)),
    ),
    progressIndicatorTheme: ProgressIndicatorThemeData(
      linearTrackColor: isDark ? const Color(0xFF263040) : const Color(0xFFC9D0DD),
    ),
  );
}

ThemeData buildAppTheme() => _build(AppTokens.light, Brightness.light);
ThemeData buildAppDarkTheme() => _build(AppTokens.dark, Brightness.dark);
