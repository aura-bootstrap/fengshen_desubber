import 'package:flutter/widgets.dart';

/// 响应式断点(需求 2 的量化实现)。
abstract final class Responsive {
  /// 窗口 ≥1500 时内容区限宽 1100 居中;否则铺满(留 28 内边距)。
  static double contentMaxWidth(double width) => width >= 1500 ? 1100 : width;

  /// 参数卡字段网格:≥900 双列,否则单列。
  static int fieldColumns(double width) => width >= 900 ? 2 : 1;

  /// 任务卡网格:≥1100 三列,≥700 两列,否则单列。
  static int taskColumns(double width) =>
      width >= 1100 ? 3 : (width >= 700 ? 2 : 1);
}

/// 限宽居中容器:全屏时内容不摊大饼。
class CenteredContent extends StatelessWidget {
  final Widget child;
  const CenteredContent({super.key, required this.child});

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(builder: (context, c) {
      final max = Responsive.contentMaxWidth(c.maxWidth);
      return Align(
        alignment: Alignment.topCenter,
        child: ConstrainedBox(
          constraints: BoxConstraints(maxWidth: max),
          child: child,
        ),
      );
    });
  }
}
