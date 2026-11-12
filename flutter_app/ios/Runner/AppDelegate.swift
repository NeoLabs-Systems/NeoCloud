import Flutter
import UIKit
import workmanager_apple

@main
@objc class AppDelegate: FlutterAppDelegate {
  // neocloud://pair?server=…&code=… links (the web pairing page's "Open in
  // the app"). The one that started the app waits until Dart asks for it
  // (listenForAppLinks in lib/src/pairing/pairing_link.dart); later ones are
  // passed on as they arrive.
  private var linkChannel: FlutterMethodChannel?
  private var initialLink: String?
  private var dartListening = false

  override func application(
    _ application: UIApplication,
    didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?
  ) -> Bool {
    GeneratedPluginRegistrant.register(with: self)
    // Camera upload runs as a BGAppRefreshTask (short, a few times a day)
    // and a BGProcessingTask (minutes long, mostly overnight on the
    // charger). The identifiers must match BGTaskSchedulerPermittedIdentifiers
    // in Info.plist and cameraUploadTaskId / cameraProcessingTaskId in
    // lib/src/mobile/camera_upload_io.dart; iOS picks the actual times.
    WorkmanagerPlugin.setPluginRegistrantCallback { registry in
      GeneratedPluginRegistrant.register(with: registry)
    }
    WorkmanagerPlugin.registerPeriodicTask(
      withIdentifier: "systems.neolabs.neocloud.cameraUpload", earliestBeginInSeconds: 3600)
    WorkmanagerPlugin.registerBGProcessingTask(
      withIdentifier: "systems.neolabs.neocloud.cameraUpload.processing")
    if let flutter = window?.rootViewController as? FlutterViewController {
      let channel = FlutterMethodChannel(
        name: "systems.neolabs.neocloud/links", binaryMessenger: flutter.binaryMessenger)
      channel.setMethodCallHandler { [weak self] call, result in
        guard let self = self, call.method == "initialLink" else {
          result(FlutterMethodNotImplemented)
          return
        }
        self.dartListening = true
        result(self.initialLink)
        self.initialLink = nil
      }
      linkChannel = channel
    }
    return super.application(application, didFinishLaunchingWithOptions: launchOptions)
  }

  override func application(
    _ app: UIApplication, open url: URL,
    options: [UIApplication.OpenURLOptionsKey: Any] = [:]
  ) -> Bool {
    guard url.scheme == "neocloud", url.host == "pair" else {
      return super.application(app, open: url, options: options)
    }
    if dartListening {
      linkChannel?.invokeMethod("link", arguments: url.absoluteString)
    } else {
      initialLink = url.absoluteString
    }
    return true
  }
}
