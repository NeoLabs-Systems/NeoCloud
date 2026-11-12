Pod::Spec.new do |s|
  s.name             = 'neocloud_background'
  s.version          = '0.1.0'
  s.summary          = "NeoCloud's background status and transfer time on iOS."
  s.description      = 'Background App Refresh, Low Power Mode and notification status, and extra time to finish an upload when NeoCloud leaves the screen.'
  s.homepage         = 'https://github.com/NeoLabs-Systems/NeoCloud'
  s.license          = { :type => 'Proprietary' }
  s.author           = 'NeoLabs Systems'
  s.source           = { :path => '.' }
  s.source_files     = 'neocloud_background/Sources/neocloud_background/**/*.swift'
  s.resource_bundles = { 'neocloud_background_privacy' => ['neocloud_background/Sources/neocloud_background/PrivacyInfo.xcprivacy'] }
  s.dependency 'Flutter'
  s.platform = :ios, '13.0'
  s.pod_target_xcconfig = { 'DEFINES_MODULE' => 'YES', 'EXCLUDED_ARCHS[sdk=iphonesimulator*]' => 'i386' }
  s.swift_version = '5.0'
end
