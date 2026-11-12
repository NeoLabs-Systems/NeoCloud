import Flutter
import UIKit
import workmanager_apple

@main
@objc class AppDelegate: FlutterAppDelegate {
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
    return super.application(application, didFinishLaunchingWithOptions: launchOptions)
  }
}
