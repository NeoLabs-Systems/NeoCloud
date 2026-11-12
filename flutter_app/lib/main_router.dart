part of 'main.dart';

// The selected page is stored in preferences. The browser address stays at
// the app root; older page links are still accepted on entry.

class _AppRouteParser extends RouteInformationParser<AppRoute> {
  const _AppRouteParser();

  @override
  Future<AppRoute> parseRouteInformation(RouteInformation routeInformation) =>
      SynchronousFuture<AppRoute>(AppRoute.parse(routeInformation.uri.path));

  @override
  RouteInformation restoreRouteInformation(AppRoute configuration) => RouteInformation(uri: Uri.parse('/'));
}

/// Page changes replace the address instead of adding browser history.
class _AppRouteProvider extends PlatformRouteInformationProvider {
  _AppRouteProvider()
    : super(initialRouteInformation: RouteInformation(uri: Uri.parse(WidgetsBinding.instance.platformDispatcher.defaultRouteName)));

  @override
  void routerReportsNewRouteInformation(RouteInformation routeInformation, {RouteInformationReportingType type = RouteInformationReportingType.none}) {
    super.routerReportsNewRouteInformation(routeInformation, type: RouteInformationReportingType.neglect);
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
