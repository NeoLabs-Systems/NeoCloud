import Flutter
import UIKit
import UserNotifications

/// The `neocloud/background` channel on iOS. iOS has no foreground service:
/// background passes run when BGTaskScheduler decides. What the app can do
/// is report what holds those passes back (Background App Refresh, Low Power
/// Mode) and, while a transfer runs, ask for the extra time iOS grants an
/// app that leaves the screen, so the current file can finish.
public class NeoCloudBackgroundPlugin: NSObject, FlutterPlugin {
  private var channel: FlutterMethodChannel?
  private var owners = Set<String>()
  private var task: UIBackgroundTaskIdentifier = .invalid

  public static func register(with registrar: FlutterPluginRegistrar) {
    let channel = FlutterMethodChannel(name: "neocloud/background", binaryMessenger: registrar.messenger())
    let instance = NeoCloudBackgroundPlugin()
    instance.channel = channel
    registrar.addMethodCallDelegate(instance, channel: channel)
  }

  public func handle(_ call: FlutterMethodCall, result: @escaping FlutterResult) {
    let args = call.arguments as? [String: Any] ?? [:]
    switch call.method {
    case "status":
      status(result)
    case "startTransfer":
      startTransfer(owner: args["owner"] as? String ?? "transfer")
      result(true)
    case "updateTransfer":
      result(nil)
    case "stopTransfer":
      owners.remove(args["owner"] as? String ?? "transfer")
      if owners.isEmpty { endTask() }
      result(nil)
    case "requestNotifications":
      UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .badge, .sound]) { granted, _ in
        DispatchQueue.main.async { result(granted) }
      }
    case "openNotificationSettings":
      if #available(iOS 16.0, *) {
        open(UIApplication.openNotificationSettingsURLString)
      } else {
        open(UIApplication.openSettingsURLString)
      }
      result(nil)
    case "openAppSettings":
      open(UIApplication.openSettingsURLString)
      result(nil)
    default:
      result(FlutterMethodNotImplemented)
    }
  }

  private func status(_ result: @escaping FlutterResult) {
    UNUserNotificationCenter.current().getNotificationSettings { settings in
      DispatchQueue.main.async {
        result([
          "platform": "ios",
          "notifications": Self.name(settings.authorizationStatus),
          "backgroundRefresh": Self.name(UIApplication.shared.backgroundRefreshStatus),
          "lowPowerMode": ProcessInfo.processInfo.isLowPowerModeEnabled,
        ])
      }
    }
  }

  /// Roughly half a minute once the app leaves the screen; when it runs
  /// out, the owners are asked to stop and the next pass resumes.
  private func startTransfer(owner: String) {
    owners.insert(owner)
    guard task == .invalid else { return }
    task = UIApplication.shared.beginBackgroundTask(withName: "NeoCloud upload") { [weak self] in
      guard let self = self else { return }
      for owner in self.owners {
        self.channel?.invokeMethod("stopRequested", arguments: ["owner": owner, "reason": "timeout"])
      }
      self.owners.removeAll()
      self.endTask()
    }
  }

  private func endTask() {
    guard task != .invalid else { return }
    UIApplication.shared.endBackgroundTask(task)
    task = .invalid
  }

  private func open(_ link: String) {
    guard let url = URL(string: link) else { return }
    UIApplication.shared.open(url)
  }

  private static func name(_ status: UNAuthorizationStatus) -> String {
    switch status {
    case .authorized, .provisional: return "granted"
    case .denied: return "denied"
    case .notDetermined: return "notDetermined"
    @unknown default: return "denied"
    }
  }

  private static func name(_ status: UIBackgroundRefreshStatus) -> String {
    switch status {
    case .available: return "available"
    case .denied: return "denied"
    case .restricted: return "restricted"
    @unknown default: return "denied"
    }
  }
}
