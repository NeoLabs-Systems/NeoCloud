part of 'main.dart';

// The web address follows the open page (/app/files/<folderId>,
// /app/settings/security, /app/admin/backups …), so a reload, a bookmark or
// the browser's back and forward buttons land on the same page. The
// controller stays the single source of truth: the router only reads
// [NeoCloudController.currentRoute] and hands addresses to
// [NeoCloudController.openRoute].

class _AppRouteParser extends RouteInformationParser<AppRoute> {
  const _AppRouteParser();

  @override
  Future<AppRoute> parseRouteInformation(RouteInformation routeInformation) =>
      SynchronousFuture<AppRoute>(AppRoute.parse(routeInformation.uri.path));

  @override
  RouteInformation restoreRouteInformation(AppRoute configuration) => RouteInformation(uri: Uri.parse(configuration.location));
}

/// Pushes a history entry per page change, except where the controller asks
/// for a replacement (the first address after signing in, a page the
/// browser itself opened, a folder that no longer exists).
class _AppRouteProvider extends PlatformRouteInformationProvider {
  _AppRouteProvider(this.controller)
    : super(initialRouteInformation: RouteInformation(uri: Uri.parse(WidgetsBinding.instance.platformDispatcher.defaultRouteName)));

  final NeoCloudController controller;
  bool _reported = false;

  @override
  void routerReportsNewRouteInformation(RouteInformation routeInformation, {RouteInformationReportingType type = RouteInformationReportingType.none}) {
    final replace = controller._takeRouteReplacement() || !_reported;
    _reported = true;
    super.routerReportsNewRouteInformation(routeInformation, type: replace ? RouteInformationReportingType.neglect : type);
  }
}

class _AppRouterDelegate extends RouterDelegate<AppRoute> with ChangeNotifier, PopNavigatorRouterDelegateMixin<AppRoute> {
  _AppRouterDelegate(this.controller) {
    controller.addListener(notifyListeners);
  }

  final NeoCloudController controller;

  @override
  GlobalKey<NavigatorState> get navigatorKey => _navigatorKey;

  @override
  AppRoute? get currentConfiguration => controller.currentRoute;

  @override
  Future<void> setInitialRoutePath(AppRoute configuration) {
    controller.openRoute(configuration, initial: true);
    return SynchronousFuture<void>(null);
  }

  @override
  Future<void> setNewRoutePath(AppRoute configuration) {
    controller.openRoute(configuration, fromBrowser: true);
    return SynchronousFuture<void>(null);
  }

  @override
  void dispose() {
    controller.removeListener(notifyListeners);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Navigator(
      key: navigatorKey,
      pages: <Page<void>>[
        MaterialPage<void>(
          key: const ValueKey<String>('home'),
          child: _NativeRuntime(controller: controller, child: NeoCloudRoot(controller: controller)),
        ),
      ],
      onDidRemovePage: (_) {},
    );
  }
}
