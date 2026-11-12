// ignore: unused_import
import 'package:intl/intl.dart' as intl;
import 'app_l10n.dart';

// ignore_for_file: type=lint

/// The translations for English (`en`).
class AppL10nEn extends AppL10n {
  AppL10nEn([String locale = 'en']) : super(locale);

  @override
  String get appName => 'NeoCloud';

  @override
  String get appEyebrow => 'PRIVATE CLOUD';

  @override
  String get cancel => 'Cancel';

  @override
  String get save => 'Save';

  @override
  String get saving => 'Saving…';

  @override
  String get saved => 'Saved';

  @override
  String get close => 'Close';

  @override
  String get dismiss => 'Dismiss';

  @override
  String get retry => 'Try again';

  @override
  String get refresh => 'Refresh';

  @override
  String get create => 'Create';

  @override
  String get delete => 'Delete';

  @override
  String get clear => 'Clear';

  @override
  String get copy => 'Copy';

  @override
  String get copied => 'Copied';

  @override
  String get continueLabel => 'Continue';

  @override
  String get genericError => 'Something went wrong. Please try again.';

  @override
  String get connectionError =>
      'Can’t reach NeoCloud. Check your connection and try again.';

  @override
  String get signIn => 'Sign in';

  @override
  String get authSignInTitle => 'Welcome back';

  @override
  String get authSignInSubtitle => 'Sign in to continue.';

  @override
  String get authCreateAccount => 'Create account';

  @override
  String get createTheFirstAccount => 'Create the first account';

  @override
  String get firstAccountSubtitle =>
      'You’re the first one here. With this account you set up NeoCloud and invite others.';

  @override
  String get createAnotherAccount => 'Create another NeoCloud account.';

  @override
  String get usernameOrEmail => 'Username or email';

  @override
  String get username => 'Username';

  @override
  String get email => 'Email';

  @override
  String get password => 'Password';

  @override
  String get confirmPassword => 'Confirm password';

  @override
  String get forgotPasswordAskAdmin =>
      'NeoCloud doesn\'t send emails yet. Ask the person who runs your NeoCloud to reset your password.';

  @override
  String get alreadyHaveAnAccount => 'Already have an account? Sign in';

  @override
  String get needANewAccount => 'Need a new account? Register';

  @override
  String get verification => 'Verification';

  @override
  String get enter2faCode => 'Enter your 2FA code';

  @override
  String get openYourAuthenticator =>
      'Open your authenticator app and enter the 6-digit code, or use a recovery code.';

  @override
  String get twoFactorOrRecoveryCode => '2FA or recovery code';

  @override
  String get verify => 'Verify';

  @override
  String get backToSignIn => 'Back to sign in';

  @override
  String get enterAUsername => 'Enter a username.';

  @override
  String get usernameTooShort => 'Use at least 3 characters.';

  @override
  String get enterAValidEmail => 'Enter a valid email address.';

  @override
  String get enterAPassword => 'Enter a password.';

  @override
  String get passwordsDoNotMatch => 'Passwords don’t match.';

  @override
  String get enterYourUsernameOrEmail => 'Enter your username or email.';

  @override
  String get enterYour2faCode => 'Enter your 2FA or recovery code.';

  @override
  String get showPassword => 'Show password';

  @override
  String get hidePassword => 'Hide password';

  @override
  String passwordStrength(String label) {
    return 'Password strength: $label';
  }

  @override
  String get strengthEmpty => 'empty';

  @override
  String get strengthRejected => 'too weak, will be rejected';

  @override
  String get strengthGood => 'good';

  @override
  String get strengthStrong => 'strong';

  @override
  String get passwordHintEmpty =>
      'Use 8+ characters. Longer passphrases work well.';

  @override
  String get passwordHintLength => 'Use at least 8 characters.';

  @override
  String get passwordHintUserInfo => 'Do not include your username or email.';

  @override
  String get passwordHintPattern =>
      'Avoid repeated characters and obvious sequences.';

  @override
  String get passwordHintMore => 'Add more length or another character type.';

  @override
  String get passwordHintStrong => 'Strong password.';

  @override
  String get passwordHintGood =>
      'Good password. A little more length makes it stronger.';

  @override
  String get navFiles => 'Files';

  @override
  String get navAllFiles => 'All files';

  @override
  String get navRecent => 'Recent';

  @override
  String get navTrash => 'Trash';

  @override
  String get navAdmin => 'Admin';

  @override
  String get navSettings => 'Settings';

  @override
  String storageUsedOf(String used, String total) {
    return '$used of $total';
  }

  @override
  String storageUsedUnlimited(String used) {
    return '$used used';
  }

  @override
  String get storageLabel => 'Storage';

  @override
  String get unlimited => 'Unlimited';

  @override
  String get filesTitle => 'Files';

  @override
  String get filesSubtitle => 'Everything you keep here is encrypted.';

  @override
  String get upload => 'Upload';

  @override
  String get newFolder => 'New folder';

  @override
  String get folderName => 'Folder name';

  @override
  String get rename => 'Rename';

  @override
  String get newName => 'New name';

  @override
  String get moveTo => 'Move to…';

  @override
  String get moveHere => 'Move here';

  @override
  String get makeCopy => 'Make a copy';

  @override
  String get download => 'Download';

  @override
  String get downloadZip => 'Download as ZIP';

  @override
  String get versions => 'Versions';

  @override
  String get open => 'Open';

  @override
  String get moveToTrash => 'Move to trash';

  @override
  String get home => 'Home';

  @override
  String get columnName => 'Name';

  @override
  String get columnSize => 'Size';

  @override
  String get columnModified => 'Modified';

  @override
  String itemsCount(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count items',
      one: '1 item',
      zero: 'No items',
    );
    return '$_temp0';
  }

  @override
  String selectedCount(int count) {
    return '$count selected';
  }

  @override
  String get emptyFolderTitle => 'This folder is empty';

  @override
  String get emptyFolderSubtitle =>
      'Drop files anywhere on this page, or use Upload.';

  @override
  String dropToUpload(String folder) {
    return 'Drop to upload to “$folder”';
  }

  @override
  String get searchFiles => 'Search files and folders';

  @override
  String noSearchResults(String query) {
    return 'Nothing matches “$query”.';
  }

  @override
  String get trashConfirmTitle => 'Move to trash?';

  @override
  String trashConfirmMessage(int count, String name) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count items move to the trash. You can restore them from there.',
      one: '“$name” moves to the trash. You can restore it from there.',
    );
    return '$_temp0';
  }

  @override
  String nameExistsTitle(String name) {
    return '“$name” already exists';
  }

  @override
  String get nameExistsMessage =>
      'A file with this name is already in this folder.';

  @override
  String get replace => 'Replace';

  @override
  String get keepBoth => 'Keep both';

  @override
  String get skip => 'Skip';

  @override
  String get applyToAll => 'Do this for the remaining files';

  @override
  String get uploadsTitle => 'Uploads';

  @override
  String uploadsActive(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: 'Uploading $count files',
      one: 'Uploading 1 file',
    );
    return '$_temp0';
  }

  @override
  String get uploadsDone => 'Uploads finished';

  @override
  String get uploadWaiting => 'Waiting';

  @override
  String get uploadDone => 'Done';

  @override
  String get uploadSkipped => 'Skipped';

  @override
  String get uploadFailed => 'Failed';

  @override
  String get uploadCancel => 'Cancel upload';

  @override
  String get uploadClearFinished => 'Clear finished';

  @override
  String get recentTitle => 'Recent';

  @override
  String get recentSubtitle => 'Files you changed or added most recently.';

  @override
  String get recentEmpty => 'No files yet';

  @override
  String get recentEmptySubtitle => 'Upload something and it shows up here.';

  @override
  String get trashTitle => 'Trash';

  @override
  String get trashSubtitle =>
      'Deleted items wait here before they are removed for good.';

  @override
  String trashRetention(int days) {
    return 'Items are removed automatically after $days days.';
  }

  @override
  String get restore => 'Restore';

  @override
  String get deleteForever => 'Delete forever';

  @override
  String get emptyTrash => 'Empty trash';

  @override
  String get emptyTrashConfirm =>
      'Everything in the trash is deleted for good. This can’t be undone.';

  @override
  String deleteForeverConfirm(String name) {
    return '“$name” is deleted for good. This can’t be undone.';
  }

  @override
  String get trashIsEmpty => 'The trash is empty';

  @override
  String get trashIsEmptySubtitle => 'Deleted files and folders show up here.';

  @override
  String deletedOn(String date, String path) {
    return 'Deleted $date · from $path';
  }

  @override
  String versionsTitle(String name) {
    return 'Versions of “$name”';
  }

  @override
  String get versionCurrent => 'Current version';

  @override
  String versionNumber(int number) {
    return 'Version $number';
  }

  @override
  String get noVersions =>
      'No earlier versions yet. Replacing the file keeps the old one here.';

  @override
  String get noPreview => 'No preview for this file type yet.';

  @override
  String get previewTooLarge => 'This file is too large to preview here.';

  @override
  String get details => 'Details';

  @override
  String get settingsTitle => 'Settings';

  @override
  String get settingsScopeAccount => 'You';

  @override
  String get settingsScopeApp => 'This app';

  @override
  String get settingsAccountHint => 'Saved to your account';

  @override
  String get settingsDeviceHint => 'Applies to this device';

  @override
  String get settingsSearchHint => 'Search settings';

  @override
  String get settingsTwoFactorOn => '2FA on';

  @override
  String get settingsTwoFactorOff => '2FA off';

  @override
  String get settingsSaveHint => 'Changes save automatically';

  @override
  String get settingsSaveFailed => 'Couldn’t save the last change';

  @override
  String get pageProfile => 'Profile';

  @override
  String get pageProfileDescription => 'Your name and sign-in details.';

  @override
  String get pageSecurity => 'Sign-in & security';

  @override
  String get pageSecurityDescription =>
      'Password, two-factor authentication and active sessions.';

  @override
  String get pageDevices => 'Devices';

  @override
  String get pageDevicesDescription =>
      'Desktop and mobile apps connected to your account.';

  @override
  String get pageStorage => 'Storage';

  @override
  String get pageStorageDescription =>
      'How much space you use and what takes it.';

  @override
  String get pageGeneral => 'General';

  @override
  String get pageGeneralDescription =>
      'Appearance, language and how files are shown.';

  @override
  String get displayName => 'Display name';

  @override
  String get displayNameHint => 'Shown instead of your username.';

  @override
  String get accountUsername => 'Username';

  @override
  String get accountEmail => 'Email';

  @override
  String get accountRole => 'Role';

  @override
  String get roleAdmin => 'Administrator';

  @override
  String get roleUser => 'User';

  @override
  String get signOut => 'Sign out';

  @override
  String get signOutDescription => 'End this session on this device.';

  @override
  String get disconnectDescription =>
      'Sign this phone out of NeoCloud; it forgets its access and camera upload turns off.';

  @override
  String get changePassword => 'Change password';

  @override
  String get currentPassword => 'Current password';

  @override
  String get newPassword => 'New password';

  @override
  String get passwordChanged => 'Password changed.';

  @override
  String get twoFactorTitle => 'Two-factor authentication';

  @override
  String twoFactorOn(int count) {
    return 'On · $count recovery codes left';
  }

  @override
  String get twoFactorOff =>
      'Off. Add a code from an authenticator app to every sign-in.';

  @override
  String get twoFactorEnable => 'Set up';

  @override
  String get twoFactorDisable => 'Turn off';

  @override
  String get twoFactorSetupStep =>
      'Scan this QR code with your authenticator app, or enter the key manually. Then enter the code it shows.';

  @override
  String get twoFactorCode => '6-digit code';

  @override
  String get recoveryCodesTitle => 'Save your recovery codes';

  @override
  String get recoveryCodesMessage =>
      'Each code signs you in once if you lose your phone. They won’t be shown again.';

  @override
  String get sessionsTitle => 'Active sessions';

  @override
  String get sessionCurrent => 'This session';

  @override
  String get deviceCurrent => 'This device';

  @override
  String sessionLastSeen(String date) {
    return 'Last active $date';
  }

  @override
  String get sessionRevoke => 'Sign out';

  @override
  String get devicesEmpty => 'No devices connected yet.';

  @override
  String get deviceConnect => 'Connect a device';

  @override
  String get deviceName => 'Device name';

  @override
  String get deviceTokenTitle => 'Device token';

  @override
  String get deviceTokenMessage =>
      'Paste this token into the NeoCloud app on that device. It is shown only once.';

  @override
  String get deviceRevoke => 'Disconnect';

  @override
  String deviceLastSeen(String date) {
    return 'Last seen $date';
  }

  @override
  String get deviceNeverSeen => 'Not used yet';

  @override
  String get storageUsed => 'Used';

  @override
  String get storageQuota => 'Quota';

  @override
  String get storageCurrentFiles => 'Current files';

  @override
  String get storageVersions => 'Earlier versions';

  @override
  String get storageTrash => 'Trash';

  @override
  String get theme => 'Theme';

  @override
  String get themeSystem => 'System';

  @override
  String get themeLight => 'Light';

  @override
  String get themeDark => 'Dark';

  @override
  String get language => 'Language';

  @override
  String get fileView => 'File view';

  @override
  String get fileViewList => 'List';

  @override
  String get fileViewGrid => 'Grid';

  @override
  String get uploadConflict => 'When a file already exists';

  @override
  String get uploadConflictAsk => 'Ask';

  @override
  String get adminTitle => 'Admin';

  @override
  String get adminSubtitle =>
      'Accounts, storage limits and how this server runs.';

  @override
  String get adminTabUsers => 'Users';

  @override
  String get adminTabStorage => 'Storage';

  @override
  String get adminTabAccess => 'Access';

  @override
  String get adminTabServer => 'Server';

  @override
  String get adminUsersTitle => 'Accounts';

  @override
  String adminUsersCount(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count accounts',
      one: '1 account',
    );
    return '$_temp0';
  }

  @override
  String adminUserYou(String name) {
    return '$name (you)';
  }

  @override
  String get adminSearchHint => 'Search admin settings';

  @override
  String get adminSearchUsers => 'Search by name or email';

  @override
  String adminQuotaDefault(String size) {
    return 'Default ($size)';
  }

  @override
  String get adminSetQuota => 'Storage limit';

  @override
  String get adminQuotaModeDefault => 'Server default';

  @override
  String get adminQuotaModeCustom => 'Custom';

  @override
  String get adminQuotaModeUnlimited => 'Unlimited';

  @override
  String get adminQuotaCustomHint => 'e.g. 50GB or 500MB';

  @override
  String get adminMakeAdmin => 'Make admin';

  @override
  String get adminRemoveAdmin => 'Remove admin';

  @override
  String get adminResetPassword => 'Reset password';

  @override
  String adminResetPasswordMessage(String name) {
    return 'Sets a new password for $name and signs them out everywhere.';
  }

  @override
  String get adminDeleteUser => 'Delete account';

  @override
  String adminDeleteUserMessage(String name) {
    return '$name and all their files are deleted for good.';
  }

  @override
  String get adminUserDisabled => 'Disabled';

  @override
  String get adminDisableUser => 'Disable';

  @override
  String get adminEnableUser => 'Enable';

  @override
  String get adminSignOutEverywhere => 'Sign out everywhere';

  @override
  String adminSignOutEverywhereTitle(String name) {
    return 'Sign out $name everywhere?';
  }

  @override
  String get adminSignOutEverywhereMessage =>
      'Every active session for this account ends, and its devices and WebDAV app passwords stop working. They can sign in again right away.';

  @override
  String adminSignedOutEverywhere(String name) {
    return 'Signed $name out of every session.';
  }

  @override
  String get accountDataTitle => 'Your data';

  @override
  String get accountExportTitle => 'Export account data';

  @override
  String get accountExportHint =>
      'Your profile, sessions, devices, links, shares and the list of your files and folders as one file. File contents are not included: download your files, or a folder as ZIP, to keep them. Passwords and keys are not included.';

  @override
  String get accountExport => 'Export';

  @override
  String get accountExported => 'Downloaded.';

  @override
  String get accountDeleteTitle => 'Delete account';

  @override
  String get accountDeleteHint =>
      'Deletes your account with all files, versions, links, shares and devices. People you shared with lose access. This cannot be undone.';

  @override
  String get accountDelete => 'Delete account';

  @override
  String get accountDeleteConfirm =>
      'Everything in this account is deleted for good. Export your data first if you want to keep it.';

  @override
  String accountDeleteTypeUsername(String name) {
    return 'Type your username $name to confirm.';
  }

  @override
  String get accountDeleted => 'Your account was deleted.';

  @override
  String get accountConfirmPassword => 'Enter your password to continue.';

  @override
  String get accountConfirmPasswordCode =>
      'Enter your password and a code from your authenticator app.';

  @override
  String get accountWrongPassword => 'The password is not correct.';

  @override
  String get accountWrongCode => 'The code is not valid.';

  @override
  String get accountLastAdmin =>
      'You’re the only admin, so your account can’t be deleted yet. Make someone else an admin first.';

  @override
  String get accountConfirmMismatch => 'The username doesn\'t match.';

  @override
  String adminFilesCount(int count) {
    return '$count files';
  }

  @override
  String get adminStorageTitle => 'Storage policy';

  @override
  String get adminDefaultQuota => 'Default quota per account';

  @override
  String get adminDefaultQuotaDescription =>
      'A size like 10GB, or “unlimited”.';

  @override
  String get adminMaxUpload => 'Largest single file';

  @override
  String get adminMaxUploadDescription => '0 means no limit beyond the quota.';

  @override
  String get adminTrashDays => 'Days in trash';

  @override
  String get adminTrashDaysDescription =>
      '0 keeps items until the trash is emptied.';

  @override
  String get adminVersions => 'Versions kept per file';

  @override
  String get adminRecalculate => 'Recalculate usage';

  @override
  String get adminAccessTitle => 'Registration';

  @override
  String get adminAllowSignup => 'Open sign-up';

  @override
  String get adminAllowSignupDescription =>
      'When off, nobody can register. The first account on a new server always can.';

  @override
  String get adminServerTitle => 'Server';

  @override
  String get adminVersion => 'Version';

  @override
  String get adminUptime => 'Uptime';

  @override
  String get adminDatabase => 'Database';

  @override
  String get adminAccounts => 'Accounts';

  @override
  String get adminFiles => 'Files';

  @override
  String get adminStored => 'Stored';

  @override
  String get adminDiskFree => 'Free on storage disk';

  @override
  String get adminLogs => 'Recent logs';

  @override
  String get adminMasterKey => 'Master key';

  @override
  String get adminMasterKeyDescription =>
      'Every file is encrypted under this key. Keep a copy offline: without it, files and backups can’t be decrypted.';

  @override
  String get adminShowMasterKey => 'Show master key';

  @override
  String get adminConfirmPassword => 'Confirm with your password';

  @override
  String get officePreviewUnavailable =>
      'There’s no preview for this file. Download it to open it.';

  @override
  String get play => 'Play';

  @override
  String get pause => 'Pause';

  @override
  String get mediaUnavailable => 'This file can’t be played here.';

  @override
  String get adminTabBackups => 'Backups';

  @override
  String get backupTargets => 'Backup targets';

  @override
  String get backupTargetsDescription =>
      'Copies of every file and the database, encrypted, on another disk or server. Only new files are uploaded each time.';

  @override
  String get backupAddTarget => 'Add target';

  @override
  String get backupEditTarget => 'Edit target';

  @override
  String get backupNoTargets => 'No backup targets yet.';

  @override
  String get backupName => 'Name';

  @override
  String get backupType => 'Type';

  @override
  String get backupTypeLocal => 'Folder / NAS';

  @override
  String get backupTypeSftp => 'SFTP';

  @override
  String get backupTypeS3 => 'S3-compatible';

  @override
  String get backupTypeWebdav => 'WebDAV';

  @override
  String get backupPath => 'Folder path';

  @override
  String get backupPathHint => 'e.g. /mnt/nas/neocloud';

  @override
  String get backupHost => 'Host';

  @override
  String get backupPort => 'Port';

  @override
  String get backupUsername => 'Username';

  @override
  String get backupPassword => 'Password';

  @override
  String get backupPrivateKey => 'Private key (optional)';

  @override
  String get backupSecretKept => 'Leave empty to keep the saved one';

  @override
  String get backupEndpoint => 'Endpoint (empty for AWS)';

  @override
  String get backupRegion => 'Region';

  @override
  String get backupBucket => 'Bucket';

  @override
  String get backupAccessKey => 'Access key';

  @override
  String get backupSecretKey => 'Secret key';

  @override
  String get backupPrefix => 'Prefix (optional)';

  @override
  String get backupPathStyle => 'Path-style URLs (MinIO)';

  @override
  String get backupUrl => 'WebDAV folder URL';

  @override
  String get backupSchedule => 'Schedule';

  @override
  String get backupScheduleManual => 'Manual';

  @override
  String get backupScheduleDaily => 'Daily';

  @override
  String get backupScheduleWeekly => 'Weekly';

  @override
  String get backupHour => 'At hour (server time)';

  @override
  String get backupWeekday => 'Weekday';

  @override
  String get backupRetention => 'Snapshots to keep';

  @override
  String get backupRunNow => 'Back up now';

  @override
  String get backupTest => 'Test connection';

  @override
  String get backupTestOk => 'Connection works.';

  @override
  String get backupStarted => 'Backup started.';

  @override
  String get backupNeverRun => 'Never backed up';

  @override
  String backupLastOk(String date, int count) {
    return 'Last backup $date · $count new files';
  }

  @override
  String backupLastFailed(String error) {
    return 'Last backup failed: $error';
  }

  @override
  String get backupRunning => 'Backing up…';

  @override
  String get backupDeleteMessage =>
      'The target is removed from NeoCloud. Backups already on it stay there.';

  @override
  String get backupHistory => 'Recent runs';

  @override
  String get adminBackupRunOk => 'Finished';

  @override
  String get adminBackupRunFailed => 'Failed';

  @override
  String get backupRestoreHint =>
      'Restore with the CLI while NeoCloud is stopped: neocloud backup restore <id> <snapshot>. You need the master key.';

  @override
  String get adminPreviews => 'Previews';

  @override
  String adminPreviewsValue(String office) {
    return 'Images: yes · Office: $office';
  }

  @override
  String get yes => 'yes';

  @override
  String get noLibreOffice => 'no (install LibreOffice)';

  @override
  String get forgotPassword => 'Forgot password?';

  @override
  String get forgotPasswordTitle => 'Reset your password';

  @override
  String get newMenu => 'New';

  @override
  String get uploadFiles => 'Upload files';

  @override
  String get sortBy => 'Sort';

  @override
  String get sortName => 'Name';

  @override
  String get sortSize => 'Size';

  @override
  String get sortModified => 'Last modified';

  @override
  String get sortType => 'Type';

  @override
  String get sortAscending => 'Ascending';

  @override
  String get sortDescending => 'Descending';

  @override
  String movedTo(String folder) {
    return 'Moved to “$folder”.';
  }

  @override
  String dragItems(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count items',
      one: '1 item',
    );
    return '$_temp0';
  }

  @override
  String get selectAll => 'Select all';

  @override
  String get openFolder => 'Open folder';

  @override
  String get showInFolder => 'Show in folder';

  @override
  String get copied2 => 'Copied.';

  @override
  String get deviceApprovalTitle => 'Connect a device';

  @override
  String get deviceApprovalSubtitle =>
      'A device asks for access to your files.';

  @override
  String get deviceApprovalEnterCode => 'Enter the code the device shows.';

  @override
  String get deviceCodeLabel => 'Device code';

  @override
  String get deviceCodeUnknown =>
      'This code is unknown or has expired. Start the sign-in on the device again.';

  @override
  String deviceApproveHint(String account) {
    return 'Only approve it if you started the sign-in yourself. It will reach the files of $account.';
  }

  @override
  String get deviceApprove => 'Approve';

  @override
  String get deviceDeny => 'Deny';

  @override
  String deviceApproved(String name) {
    return '$name is connected. You can return to the device.';
  }

  @override
  String deviceDenied(String name) {
    return '$name was denied access.';
  }

  @override
  String get openNeoCloud => 'Open NeoCloud';

  @override
  String get connectTitle => 'Connect to NeoCloud';

  @override
  String get connectSubtitle =>
      'Enter the address of your NeoCloud server, then sign in.';

  @override
  String get connectServerLabel => 'Server address';

  @override
  String get connectButton => 'Connect';

  @override
  String get connectInvalidServer =>
      'Enter a server address such as https://cloud.example.com.';

  @override
  String get connectApproveTitle => 'Approve in your browser';

  @override
  String get connectApproveSubtitle =>
      'Sign in to NeoCloud in the browser window that opened and check that it shows this code.';

  @override
  String get connectWaiting => 'Waiting for approval…';

  @override
  String get connectOpenBrowser => 'Open browser again';

  @override
  String get connectDenied => 'The sign-in was denied in the browser.';

  @override
  String get connectExpired =>
      'The code expired before it was approved. Connect again.';

  @override
  String get deviceRevokedNotice =>
      'This device was signed out. Connect it again to continue.';

  @override
  String get vaultBoundaryError =>
      'Items can’t be moved into or out of a vault. Download and upload them instead.';

  @override
  String shareTargetTitle(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: 'Save $count files to NeoCloud',
      one: 'Save 1 file to NeoCloud',
    );
    return '$_temp0';
  }

  @override
  String get uploadHere => 'Upload here';

  @override
  String shareRejected(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count shared items couldn’t be received.',
      one: '1 shared item couldn’t be received.',
    );
    return '$_temp0';
  }

  @override
  String get shareTooLarge =>
      'This share is larger than 2 GB and wasn’t received. Upload big files from the Files page.';

  @override
  String get edit => 'Edit';

  @override
  String get editorDone => 'Done';

  @override
  String get editorSaved => 'Saved as a new version';

  @override
  String get editorUnsaved => 'Unsaved changes';

  @override
  String get editorDiscardTitle => 'Discard your changes?';

  @override
  String get editorDiscardMessage =>
      'What you changed since the last save will be lost.';

  @override
  String get editorKeepEditing => 'Keep editing';

  @override
  String get editorDiscard => 'Discard';

  @override
  String get editorConflictTitle => 'Changed elsewhere';

  @override
  String editorConflictMessage(String name) {
    return '$name was changed since you opened it. Save anyway? The other version stays in the version history.';
  }

  @override
  String get editorOverwrite => 'Save anyway';

  @override
  String get vaultNoMediaPreview =>
      'Vault videos and audio can’t be played directly. Download the file to play it.';

  @override
  String get shareLink => 'Share';

  @override
  String get newVault => 'New vault';

  @override
  String get vaultLock => 'Lock';

  @override
  String get vaultSubtitle =>
      'End-to-end encrypted. Names and contents are encrypted on this device; NeoCloud can’t read them.';

  @override
  String get vaultIntro =>
      'A vault is a folder whose names and contents are encrypted on your device with a passphrase only you know.';

  @override
  String get vaultPassphrase => 'Passphrase';

  @override
  String get vaultPassphraseRepeat => 'Repeat passphrase';

  @override
  String get vaultNoRecovery =>
      'The passphrase can’t be recovered or reset, not even by NeoCloud. If you forget it, the files in this vault are lost.';

  @override
  String get vaultCreate => 'Create vault';

  @override
  String get vaultNameRequired => 'Give the vault a name.';

  @override
  String vaultPassphraseTooShort(int count) {
    return 'Use at least $count characters.';
  }

  @override
  String get vaultWrongPassphrase => 'That passphrase doesn’t open this vault.';

  @override
  String vaultLockedTitle(String name) {
    return '$name is locked';
  }

  @override
  String get vaultLockedSubtitle =>
      'Enter the passphrase to see and add files. It stays unlocked until you lock it or close NeoCloud.';

  @override
  String get vaultUnlock => 'Unlock';

  @override
  String shareLinkTitle(String name) {
    return 'Share “$name”';
  }

  @override
  String get shareExpiry => 'Link expires';

  @override
  String get shareExpiryNever => 'Never';

  @override
  String get shareExpiryDay => '1 day';

  @override
  String get shareExpiryWeek => '7 days';

  @override
  String get shareExpiryMonth => '30 days';

  @override
  String get sharePasswordToggle => 'Protect with a password';

  @override
  String get sharePasswordHint => 'Visitors need it to open the link.';

  @override
  String get sharePassword => 'Link password';

  @override
  String get sharePasswordRequired =>
      'Enter a password or turn the protection off.';

  @override
  String get shareAllowUpload => 'Allow uploads';

  @override
  String get shareAllowUploadHint =>
      'Anyone with the link can add files to this folder.';

  @override
  String get shareCreate => 'Create link';

  @override
  String get shareExpired => 'Expired';

  @override
  String get shareNeverExpires => 'Never expires';

  @override
  String shareExpiresOn(String date) {
    return 'Expires $date';
  }

  @override
  String get sharePasswordTag => 'Password';

  @override
  String get shareUploadTag => 'Uploads allowed';

  @override
  String get shareFolderTag => 'Folder';

  @override
  String get shareRevoke => 'Revoke';

  @override
  String shareRevokeConfirm(String name) {
    return 'The link to $name stops working right away for everyone who has it.';
  }

  @override
  String get shareRevoked => 'Link revoked.';

  @override
  String get shareCopied => 'Link copied.';

  @override
  String get pageSharedLinks => 'Shared links';

  @override
  String get pageSharedLinksDescription =>
      'Public links to your files and folders.';

  @override
  String get sharedLinksHint =>
      'Create a link from a file’s actions. Revoking stops it at once.';

  @override
  String get sharedLinksEmpty => 'No shared links yet.';

  @override
  String get pageCameraUpload => 'Camera upload';

  @override
  String get pageCameraUploadDescription =>
      'Upload new photos and videos from this phone automatically.';

  @override
  String get disconnect => 'Disconnect';

  @override
  String get disconnectTitle => 'Disconnect this device?';

  @override
  String disconnectMessage(String server) {
    return 'This device forgets its access to $server and camera upload turns off. To connect again you sign in once more.';
  }

  @override
  String get syncChooseThisFolder => 'Choose this folder';

  @override
  String get syncChoose => 'Choose';

  @override
  String get syncNothingChosen => 'Not chosen yet';

  @override
  String get cameraAccessTitle => 'Allow access to your photos';

  @override
  String get cameraAccessMessage =>
      'NeoCloud reads new photos and videos from your library to upload them to your NeoCloud. Nothing else leaves the phone. You can choose “All photos” or a selection in the next step.';

  @override
  String get cameraAccessDenied =>
      'NeoCloud has no access to your photos. Allow it in the system settings to use camera upload.';

  @override
  String get cameraUploadOn =>
      'Camera upload is on. New photos are uploaded from now on.';

  @override
  String get cameraReconfirmTitle => 'Turn camera upload back on?';

  @override
  String cameraReconfirmMessage(String server) {
    return 'Camera upload was turned off because this device signed out or connected to another server or account. Photos go to $server only after you set it up again.';
  }

  @override
  String get cameraReconfirmLater => 'Not now';

  @override
  String get cameraReconfirmSetUp => 'Set up';

  @override
  String get cameraUploadFolder => 'Upload to';

  @override
  String get cameraUploadEnable => 'Upload new photos';

  @override
  String get cameraUploadEnableDescription =>
      'Photos taken after you turn this on; older ones stay on the phone only.';

  @override
  String get cameraUploadWifiOnly => 'Only on Wi-Fi';

  @override
  String get cameraUploadVideos => 'Include videos';

  @override
  String get cameraUploadStatus => 'Status';

  @override
  String get cameraUploadBackgroundHint =>
      'In the background the system decides when NeoCloud may run: on Android about hourly at most, on iOS a few times a day. Opening the app uploads right away.';

  @override
  String get cameraUploadOffState => 'Off';

  @override
  String get cameraUploadFailedState => 'Last run failed';

  @override
  String get cameraUploadFailedHint =>
      'Check the network and that NeoCloud may still read your photos.';

  @override
  String cameraUploadCount(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count files uploaded',
      one: '1 file uploaded',
      zero: 'Nothing uploaded yet',
    );
    return '$_temp0';
  }

  @override
  String cameraUploadSince(String date) {
    return 'since $date';
  }

  @override
  String cameraUploadLastRun(String date) {
    return 'last run $date';
  }

  @override
  String get cameraUploadRunNow => 'Upload now';

  @override
  String get cameraAccessSettings => 'Photo access';

  @override
  String get cameraOpenSettings => 'Open settings';

  @override
  String get shareSummaryView => 'Anyone with this link can view and download';

  @override
  String get shareSummaryViewUpload =>
      'Anyone with this link can view, download and add files';

  @override
  String get shareSummaryUploadOnly =>
      'Anyone with this link can add files but can’t see what’s in the folder';

  @override
  String get shareSummaryPassword => 'password required';

  @override
  String shareSummaryExpires(String date) {
    return 'expires $date';
  }

  @override
  String get shareSummaryNoExpiry => 'no expiry';

  @override
  String get shareSummaryExpired => 'expired';

  @override
  String get shareSummaryOneTime => 'one-time link';

  @override
  String shareSummaryDownloadsLeft(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count downloads left',
      one: '1 download left',
    );
    return '$_temp0';
  }

  @override
  String get shareSummaryUsedUp => 'download limit reached';

  @override
  String shareDownloadedTimes(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: 'Downloaded $count×',
      zero: 'Not downloaded yet',
    );
    return '$_temp0';
  }

  @override
  String shareExpiresInDays(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: 'expires in $count days',
      one: 'expires tomorrow',
    );
    return '$_temp0';
  }

  @override
  String shareExpiresInHours(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: 'expires in $count hours',
      one: 'expires in 1 hour',
      zero: 'expires within the hour',
    );
    return '$_temp0';
  }

  @override
  String shareExpiredOn(String date) {
    return 'expired $date';
  }

  @override
  String get shareModeTitle => 'Visitors can';

  @override
  String get shareModeView => 'View';

  @override
  String get shareModeViewUpload => 'View + upload';

  @override
  String get shareModeUploadOnly => 'File request';

  @override
  String get shareModeViewHint => 'See, preview and download what you share.';

  @override
  String get shareModeViewUploadHint =>
      'Also add files to this folder. Nothing is ever replaced.';

  @override
  String get shareModeUploadOnlyHint =>
      'Only add files. Visitors don’t see the folder’s contents, not even what others uploaded.';

  @override
  String get shareExpiryCustom => 'Custom…';

  @override
  String get shareExpiryPick => 'Expires on';

  @override
  String get sharePasswordGenerate => 'Generate';

  @override
  String get sharePasswordCopy => 'Copy password';

  @override
  String get sharePasswordCopied => 'Password copied.';

  @override
  String get sharePasswordSendSeparately =>
      'Send the password another way than the link, e.g. by phone or another messenger.';

  @override
  String sharePasswordTooShort(int count) {
    return 'Use at least $count characters.';
  }

  @override
  String get sharePasswordKeep => 'Leave empty to keep the current password.';

  @override
  String get shareLimitTitle => 'Download limit';

  @override
  String get shareLimitNone => 'No limit';

  @override
  String get shareLimitOnce => 'One-time';

  @override
  String get shareLimitCustom => 'Custom';

  @override
  String get shareLimitCount => 'Downloads allowed';

  @override
  String get shareLimitHint =>
      'Every file or ZIP download counts. The link stops working after the last one.';

  @override
  String get shareLimitInvalid =>
      'Enter a number of downloads from 1 to 1,000,000.';

  @override
  String get shareNote => 'Note for visitors (optional)';

  @override
  String get shareNoteHint => 'Shown on the share page.';

  @override
  String get shareSave => 'Save changes';

  @override
  String get shareSaved => 'Link updated.';

  @override
  String get shareEdit => 'Edit link';

  @override
  String get shareNewLink => 'New link';

  @override
  String shareMoreLinks(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other:
          '$count more active links for this item in Settings › Shared links.',
      one: '1 more active link for this item in Settings › Shared links.',
    );
    return '$_temp0';
  }

  @override
  String get shareGroupActive => 'Active';

  @override
  String get shareGroupInactive => 'Expired or used up';

  @override
  String get shareNoteTag => 'Note';

  @override
  String get shareModeFileRequestTag => 'File request';

  @override
  String get shareExpiryInPast => 'Pick an expiry date in the future.';

  @override
  String get vaultShowPassphrase => 'Show or hide';

  @override
  String get vaultGeneratePassphrase => 'Generate passphrase';

  @override
  String get vaultGeneratedHint =>
      'Six random words. Write them down — the vault can only be opened with them or the recovery key.';

  @override
  String get vaultCopyPassphrase => 'Copy passphrase';

  @override
  String get vaultStrengthTooWeak => 'Too weak';

  @override
  String get vaultStrengthWeak => 'Weak';

  @override
  String get vaultStrengthGood => 'Good';

  @override
  String get vaultStrengthStrong => 'Strong';

  @override
  String vaultStrengthLabel(String label) {
    return 'Strength: $label';
  }

  @override
  String vaultHintTooShort(int count) {
    return 'Use at least $count characters, or generate a passphrase.';
  }

  @override
  String get vaultHintTooWeak =>
      'Easy to guess. Add more words or characters, avoid names, years and common passwords.';

  @override
  String get vaultHintGood =>
      'Good. Longer is stronger: four or more unrelated words work well.';

  @override
  String get vaultHintStrong => 'Strong passphrase.';

  @override
  String get vaultWarningCallout =>
      'Only you hold the key. If you lose both the passphrase and the recovery key, nobody, not even NeoCloud, can open this vault again.';

  @override
  String get vaultNext => 'Next';

  @override
  String get vaultBack => 'Back';

  @override
  String get vaultStepDetails => 'Step 1 of 3 · Name and passphrase';

  @override
  String get vaultStepRecovery => 'Step 2 of 3 · Recovery key';

  @override
  String get vaultStepConfirm => 'Step 3 of 3 · Check the recovery key';

  @override
  String get vaultRecoveryTitle => 'Recovery key';

  @override
  String get vaultRecoveryIntro =>
      'These 24 words open the vault if you forget the passphrase. Write them down or print the sheet and keep it somewhere safe, offline.';

  @override
  String get vaultRecoveryDontStore =>
      'Don’t store the recovery key in NeoCloud. Anyone who has it can open the vault.';

  @override
  String get vaultRecoveryCopy => 'Copy words';

  @override
  String get vaultRecoveryDownload => 'Download recovery sheet';

  @override
  String get vaultRecoverySaved => 'I saved the recovery key somewhere safe';

  @override
  String get vaultRecoveryCheckIntro =>
      'Type these words from your recovery key to make sure you have it.';

  @override
  String vaultRecoveryWordNumber(String number) {
    return 'Word $number';
  }

  @override
  String get vaultRecoveryCheckWrong =>
      'That doesn’t match the recovery key. Check what you saved.';

  @override
  String get vaultSheetTitle => 'NeoCloud vault recovery key';

  @override
  String vaultSheetVault(String name) {
    return 'Vault: $name';
  }

  @override
  String vaultSheetServer(String server) {
    return 'Server: $server';
  }

  @override
  String vaultSheetCreated(String date) {
    return 'Created: $date';
  }

  @override
  String get vaultSheetUse =>
      'If you forget the passphrase: open the vault in NeoCloud, choose “Forgot passphrase?” and enter these 24 words in order.';

  @override
  String get vaultSheetKeep =>
      'Keep this sheet offline, e.g. printed with your important papers. Don’t store it in NeoCloud. Anyone with these words can open the vault.';

  @override
  String get vaultRecoveryCopied =>
      'Recovery words copied. Paste them somewhere offline and clear the clipboard.';

  @override
  String get vaultForgot => 'Forgot passphrase?';

  @override
  String get vaultRecoverTitle => 'Reset the passphrase';

  @override
  String get vaultRecoverIntro =>
      'Enter the 24 words of the recovery key you saved when you created this vault, then choose a new passphrase.';

  @override
  String get vaultRecoverWords => 'Recovery key (24 words)';

  @override
  String vaultRecoverWordCount(int count) {
    return '$count of 24 words';
  }

  @override
  String vaultRecoverUnknownWord(String number) {
    return 'Word $number isn’t a recovery word. Check the spelling.';
  }

  @override
  String get vaultRecoverChecksum =>
      'These words don’t form a valid recovery key. Check their order and spelling.';

  @override
  String get vaultRecoverWordsOk => 'Recovery key looks right.';

  @override
  String get vaultRecoverMismatch =>
      'This recovery key belongs to a different vault.';

  @override
  String get vaultNewPassphrase => 'New passphrase';

  @override
  String get vaultRecoverSubmit => 'Reset and unlock';

  @override
  String get vaultRecovered =>
      'New passphrase set. The vault is unlocked; your recovery key stays the same.';

  @override
  String get vaultShowRecovery => 'Show recovery key';

  @override
  String get vaultShowRecoveryIntro =>
      'Enter the vault’s passphrase to see its recovery key.';

  @override
  String get vaultBanner =>
      'No previews, search or sharing in vaults — by design. Names and contents are encrypted on this device before upload.';

  @override
  String get vaultUnlocking => 'Unlocking…';

  @override
  String get securityChecklistTitle => 'Make this server recoverable';

  @override
  String get securityChecklistSubtitle =>
      'Three things that decide whether your files survive a lost disk or server.';

  @override
  String get securityMasterKeyItem => 'Master key exported';

  @override
  String get securityMasterKeyItemHint =>
      'Every file and backup is encrypted under it. Keep a copy offline, outside this server.';

  @override
  String get securityBackupTargetItem => 'Backup target set up';

  @override
  String get securityBackupTargetItemHint =>
      'A second place – another disk, SFTP, S3 or WebDAV – that gets an encrypted copy.';

  @override
  String get securityBackupVerifiedItem => 'Backup verified';

  @override
  String get securityBackupVerifiedItemHint =>
      'A test restore of the latest snapshot worked.';

  @override
  String get securityBackupRanItem => 'A backup ran successfully';

  @override
  String get securityExportKey => 'Export master key';

  @override
  String get securitySetUpBackup => 'Set up backups';

  @override
  String get securityVerifyNow => 'Verify now';

  @override
  String get securityVerifyOk => 'Backup verified.';

  @override
  String securityVerifyFailed(String error) {
    return 'Verification failed: $error';
  }

  @override
  String get securityDone => 'Done';

  @override
  String get securityOpen => 'Open';

  @override
  String get securityBanner =>
      'Export the master key and keep it offline. Without it, files and backups can’t be decrypted if this server is lost.';

  @override
  String get securityKeyStored =>
      'I stored the master key somewhere safe, outside this server';

  @override
  String get securityKeyDownload => 'Download as file';

  @override
  String get securityDatabaseOk => 'OK';

  @override
  String get securityDatabasePending => 'Checking…';

  @override
  String get securityDatabaseDamaged => 'Damaged';

  @override
  String securityDatabaseCheckedAt(String date) {
    return 'checked $date';
  }

  @override
  String securityDamagedBlobs(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count damaged files found by the background check',
      one: '1 damaged file found by the background check',
    );
    return '$_temp0';
  }

  @override
  String get securityDiskFull =>
      'NeoCloud is out of space right now, so nothing was saved. Try again later or contact the person who runs it.';

  @override
  String get securityServerStopping =>
      'NeoCloud is restarting. Try again in a moment.';

  @override
  String get filesFavoritesEmpty => 'No favorites yet';

  @override
  String get filesFavoritesEmptySubtitle =>
      'Star files and folders you use often; they show up here.';

  @override
  String get filesFavoritesSubtitle => 'Files and folders you starred.';

  @override
  String get filesNavFavorites => 'Favorites';

  @override
  String filesTrashExpiresIn(int days) {
    String _temp0 = intl.Intl.pluralLogic(
      days,
      locale: localeName,
      other: 'deleted permanently in $days days',
      one: 'deleted permanently in 1 day',
      zero: 'deleted permanently today',
    );
    return '$_temp0';
  }

  @override
  String filesAndMore(String names, int count) {
    return '$names and $count more';
  }

  @override
  String filesCut(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count items cut · paste them in another folder',
      one: '1 item cut · paste it in another folder',
    );
    return '$_temp0';
  }

  @override
  String get filesCutAction => 'Cut';

  @override
  String get filesFavorite => 'favorite';

  @override
  String filesFavoriteFailed(String names) {
    return 'Couldn\'t change the star of $names.';
  }

  @override
  String get filesKindFolder => 'folder';

  @override
  String filesModifiedOn(String date) {
    return 'modified $date';
  }

  @override
  String get filesModifiedToday => 'modified today';

  @override
  String get filesModifiedYesterday => 'modified yesterday';

  @override
  String filesMoveFailed(String names, String folder) {
    return 'Couldn\'t move $names to “$folder”.';
  }

  @override
  String filesPasteHere(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: 'Paste $count items here',
      one: 'Paste 1 item here',
    );
    return '$_temp0';
  }

  @override
  String filesRenameFailed(String name) {
    return 'Couldn\'t rename “$name”.';
  }

  @override
  String filesRestoreFailed(String names) {
    return 'Couldn\'t restore $names.';
  }

  @override
  String get filesShortcutsHint =>
      'Press ? for keyboard shortcuts · Ctrl/⌘+K for commands · drag items onto a folder to move them';

  @override
  String get filesStar => 'Add to favorites';

  @override
  String filesStarred(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count items added to favorites',
      one: 'Added to favorites',
    );
    return '$_temp0';
  }

  @override
  String filesTrashFailed(String names) {
    return 'Couldn\'t move $names to the trash.';
  }

  @override
  String filesUndoFailed(String names) {
    return 'Couldn\'t undo for $names.';
  }

  @override
  String get filesUnstar => 'Remove from favorites';

  @override
  String filesUnstarred(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count items removed from favorites',
      one: 'Removed from favorites',
    );
    return '$_temp0';
  }

  @override
  String get filesWelcomeTitle => 'Welcome to your cloud';

  @override
  String get filesWelcomeUploadBody =>
      'Upload files or drop them anywhere on this page.';

  @override
  String get filesWelcomeUploadBodyPhone =>
      'Upload files from the New menu, or turn on camera upload.';

  @override
  String get filesWelcomeDesktopBody =>
      'Keep a folder on your computer in sync.';

  @override
  String get filesWelcomeDesktopAction => 'Get the app';

  @override
  String get filesWelcomeKeyBody =>
      'Without it, a restored server can\'t read any file. Store it somewhere safe.';

  @override
  String get filesWelcomeKeyAction => 'Open admin';

  @override
  String get paletteActions => 'Actions';

  @override
  String get paletteFolders => 'Go to folder';

  @override
  String get paletteHint => 'Type a command, folder or page';

  @override
  String get paletteNothing => 'Nothing matches.';

  @override
  String get palettePlaces => 'Places';

  @override
  String get paletteRecent => 'Recent files';

  @override
  String get paletteSearchFiles => 'Search files';

  @override
  String get paletteShortcuts => 'Keyboard shortcuts';

  @override
  String get paletteTitle => 'Command palette';

  @override
  String get paletteShortcutClear => 'Clear selection or search';

  @override
  String get paletteShortcutCutPaste => 'Cut and paste into another folder';

  @override
  String get paletteShortcutExtend => 'Extend the selection';

  @override
  String get paletteShortcutHomeEnd => 'First or last item';

  @override
  String get paletteShortcutMove => 'Move between items';

  @override
  String get paletteShortcutSelectAll => 'Select all';

  @override
  String get paletteShortcutToggle => 'Select or deselect';

  @override
  String get paletteShortcutTypeAhead => 'Jump to a name by typing it';

  @override
  String get searchAny => 'Any';

  @override
  String get searchClearFilters => 'Clear filters';

  @override
  String get searchEmptyHint =>
      'Try fewer words, other filters, or search all folders.';

  @override
  String get searchEverywhere => 'Search all folders';

  @override
  String get searchLookInTrash => 'Look in the trash';

  @override
  String get searchModified => 'Modified';

  @override
  String get searchModifiedDay => 'Last 24 hours';

  @override
  String get searchModifiedWeek => 'Last 7 days';

  @override
  String get searchModifiedMonth => 'Last 30 days';

  @override
  String get searchModifiedYear => 'Last year';

  @override
  String get searchNothingFiltered => 'Nothing matches these filters.';

  @override
  String get searchOpenFolder => 'Open containing folder';

  @override
  String searchResultsCount(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count results',
      one: '1 result',
      zero: 'No results',
    );
    return '$_temp0';
  }

  @override
  String get searchSize => 'Size';

  @override
  String get searchSize10m => 'Over 10 MB';

  @override
  String get searchSize100m => 'Over 100 MB';

  @override
  String get searchSize1g => 'Over 1 GB';

  @override
  String get searchThisFolder => 'This folder';

  @override
  String get searchType => 'Type';

  @override
  String get searchTypeArchives => 'Archives';

  @override
  String get searchTypeAudio => 'Audio';

  @override
  String get searchTypeDocuments => 'Documents';

  @override
  String get searchTypeFolders => 'Folders';

  @override
  String get searchTypeImages => 'Images';

  @override
  String get searchTypePdf => 'PDFs';

  @override
  String get searchTypeVideos => 'Videos';

  @override
  String get storageAlmostFull => 'Almost full';

  @override
  String get storageByType => 'By type';

  @override
  String get storageDetailsUnavailable =>
      'A detailed breakdown isn’t available right now.';

  @override
  String storageDuplicateGroup(int count, String size) {
    return '$count identical copies · $size each';
  }

  @override
  String get storageDuplicates => 'Duplicates';

  @override
  String get storageKeepNewest => 'Keep newest';

  @override
  String get storageLargest => 'Largest files';

  @override
  String get storageNewest => 'Newest';

  @override
  String get storageNoDuplicates => 'No identical files found.';

  @override
  String storageReclaimable(String size) {
    return '$size can be freed';
  }

  @override
  String get storageTypeOther => 'Other';

  @override
  String get storageTypeVaults => 'Vaults';

  @override
  String get undoAction => 'Undo';

  @override
  String undoMoved(int count, String folder) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count items moved to “$folder”',
      one: '1 item moved to “$folder”',
    );
    return '$_temp0';
  }

  @override
  String undoRenamed(String name) {
    return 'Renamed to “$name”';
  }

  @override
  String undoRestored(String name) {
    return '“$name” restored';
  }

  @override
  String undoTrashed(int count, String name) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count items moved to trash',
      one: '“$name” moved to trash',
    );
    return '$_temp0';
  }

  @override
  String uploadAnnounceFailed(String name) {
    return 'Upload failed: $name';
  }

  @override
  String uploadAnnounceFinishedWithFailures(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: 'Uploads finished, $count failed',
      one: 'Uploads finished, 1 failed',
    );
    return '$_temp0';
  }

  @override
  String uploadConflictApplyToAll(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: 'Do this for all $count remaining uploads',
      one: 'Do this for the 1 remaining upload',
    );
    return '$_temp0';
  }

  @override
  String get uploadConflictExisting => 'ALREADY THERE';

  @override
  String get uploadConflictIncoming => 'NEW UPLOAD';

  @override
  String uploadConflictKeepBothAs(String name) {
    return 'Keep both saves the new file as “$name”.';
  }

  @override
  String get uploadDismiss => 'Dismiss';

  @override
  String uploadEtaHours(int count) {
    return 'about $count h';
  }

  @override
  String uploadEtaMinutes(int count) {
    return 'about $count min';
  }

  @override
  String get uploadEtaUnderMinute => 'less than a minute';

  @override
  String get uploadFinalizing => 'Finishing…';

  @override
  String uploadInterrupted(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count uploads were interrupted',
      one: '1 upload was interrupted',
    );
    return '$_temp0';
  }

  @override
  String get uploadInterruptedHint =>
      'Pick the same files again to continue where they stopped.';

  @override
  String get uploadPause => 'Pause';

  @override
  String get uploadPauseAll => 'Pause all';

  @override
  String get uploadPaused => 'Paused';

  @override
  String uploadProgressCount(int done, int total) {
    return '$done of $total';
  }

  @override
  String get uploadResume => 'Resume';

  @override
  String get uploadResumeAll => 'Resume all';

  @override
  String get uploadResumeReselect => 'Resume (re-select files)';

  @override
  String get uploadWaitingForConnection => 'Waiting for connection';

  @override
  String get cameraStatusOff => 'Off';

  @override
  String get cameraStatusUpToDate => 'Up to date';

  @override
  String cameraStatusUploading(int current, int total) {
    return 'Uploading $current of $total';
  }

  @override
  String cameraStatusPending(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count files waiting',
      one: '1 file waiting',
    );
    return '$_temp0';
  }

  @override
  String get cameraStatusWaitingWifi => 'Waiting for Wi-Fi';

  @override
  String get cameraStatusWaitingCharger => 'Waiting for charger';

  @override
  String get cameraStatusLimited => 'Limited photo access';

  @override
  String get cameraStatusNoAccess => 'No photo access';

  @override
  String get cameraStatusNoFolder => 'No folder chosen';

  @override
  String get cameraStatusUnreachable => 'Can’t reach NeoCloud';

  @override
  String get cameraStatusSignedOut => 'Signed out';

  @override
  String get cameraStatusServerFull => 'NeoCloud is full';

  @override
  String cameraStatusFailed(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count failed',
      one: '1 failed',
    );
    return '$_temp0';
  }

  @override
  String get cameraCountOnPhone => 'On phone';

  @override
  String get cameraCountBackedUp => 'Backed up';

  @override
  String get cameraCountPending => 'Pending';

  @override
  String get cameraCountFailed => 'Failed';

  @override
  String cameraWaitingCloud(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count waiting on iCloud',
      one: '1 waiting on iCloud',
    );
    return '$_temp0';
  }

  @override
  String get cameraBackupTitle => 'Photo backup';

  @override
  String get cameraBackupEnableDescription =>
      'Back up the photos on this phone to NeoCloud. Nothing is ever deleted from the phone.';

  @override
  String get cameraScopeTitle => 'What should be backed up?';

  @override
  String get cameraScopeNew => 'New photos only';

  @override
  String get cameraScopeNewHint => 'Photos and videos taken from now on.';

  @override
  String cameraScopeEverything(int count, String size) {
    return 'Everything ($count items, about $size)';
  }

  @override
  String cameraScopeEverythingCount(int count) {
    return 'Everything ($count items)';
  }

  @override
  String get cameraScopeEverythingHint =>
      'Your whole library, newest first. The first run takes a while.';

  @override
  String get cameraScopeCounting => 'Counting your library…';

  @override
  String get cameraScopeSetting => 'Back up';

  @override
  String get cameraScopeNewShort => 'New only';

  @override
  String get cameraScopeEverythingShort => 'Everything';

  @override
  String get cameraChargingOnly => 'Only while charging';

  @override
  String get cameraChargingHint =>
      'In the background Android waits for the charger. iOS can’t tell apps, so it backs up when you open NeoCloud or tap Back up now.';

  @override
  String get cameraVideosWifiOnly => 'Videos only on Wi-Fi';

  @override
  String get cameraVideosWifiOnlyHint =>
      'Photos may use mobile data, videos wait for Wi-Fi.';

  @override
  String get cameraLimitedMessage =>
      'NeoCloud can only see the photos you selected. The others aren’t backed up.';

  @override
  String get cameraAllowFullAccess => 'Allow full access';

  @override
  String get cameraIosBackgroundTip =>
      'iOS runs background tasks rarely. Open NeoCloud now and then, best on Wi-Fi and while charging.';

  @override
  String get backgroundNotificationChannel => 'Backup progress';

  @override
  String backgroundNotificationProgress(int current, int total) {
    return 'Backing up $current of $total';
  }

  @override
  String get backgroundNotificationPause => 'Pause';

  @override
  String get backgroundCameraPaused => 'Paused';

  @override
  String get backgroundTitle => 'Keep backup running';

  @override
  String get backgroundIntroAndroid =>
      'Camera upload keeps going while NeoCloud is closed. To save battery, Android and your phone’s maker stop apps in the background; these settings keep NeoCloud running.';

  @override
  String get backgroundIntroIos =>
      'iOS decides when NeoCloud may run in the background: a few times a day, most often overnight on the charger. These settings give it every chance.';

  @override
  String get backgroundStatusGranted => 'Granted';

  @override
  String get backgroundStatusNeeded => 'Needed';

  @override
  String get backgroundStatusRecommended => 'Recommended';

  @override
  String get backgroundStatusOff => 'Off';

  @override
  String get backgroundPhotosTitle => 'Photos and videos';

  @override
  String get backgroundPhotosDetail =>
      'Access to the whole library, not only selected photos, so every new photo is backed up.';

  @override
  String get backgroundBatteryTitle => 'Unrestricted battery';

  @override
  String get backgroundBatteryDetail =>
      'Lets Android run NeoCloud in the background and start a backup soon after you take a photo.';

  @override
  String backgroundManufacturerTitle(String maker) {
    return '$maker settings';
  }

  @override
  String backgroundManufacturerDetail(String maker) {
    return '$maker stops apps in the background on top of Android. Change these so it leaves NeoCloud running:';
  }

  @override
  String get backgroundManufacturerGuide =>
      'Guide for your phone on dontkillmyapp.com';

  @override
  String get backgroundNotificationsTitle => 'Notifications';

  @override
  String get backgroundNotificationsDetailAndroid =>
      'Shows the backup’s progress, with a Pause button.';

  @override
  String get backgroundNotificationsDetailIos =>
      'Lets NeoCloud tell you when backup needs you.';

  @override
  String get backgroundDataTitle => 'Background data';

  @override
  String get backgroundDataDetail =>
      'Data Saver blocks uploads while NeoCloud is closed unless it may use data without restriction.';

  @override
  String get backgroundRefreshTitle => 'Background App Refresh';

  @override
  String get backgroundRefreshDetail =>
      'Without it, iOS never runs NeoCloud in the background.';

  @override
  String get backgroundRefreshRestricted =>
      'Turned off by Screen Time or a device profile; it can only be turned on there.';

  @override
  String get backgroundLowPowerTitle => 'Low Power Mode';

  @override
  String get backgroundLowPowerDetail =>
      'While it is on, iOS pauses background tasks. Turn it off in Settings › Battery to let a backup run.';

  @override
  String get backgroundSwipeHint =>
      'Don’t swipe NeoCloud away in the app switcher: iOS then stops its background tasks until you open it again.';

  @override
  String get backgroundAllow => 'Allow';

  @override
  String get backgroundAllowAll => 'Allow all';

  @override
  String get backgroundOpenSettings => 'Open settings';

  @override
  String get backgroundNotNow => 'Not now';

  @override
  String get backgroundBannerTitle => 'Backup may not run in the background';

  @override
  String get backgroundBannerMissing =>
      'Something NeoCloud needs to run in the background is turned off.';

  @override
  String backgroundBannerStale(String date) {
    return 'No complete backup since $date. The system may be stopping NeoCloud in the background.';
  }

  @override
  String get backgroundBannerFix => 'Check settings';

  @override
  String get backgroundReliabilityTitle => 'Background reliability';

  @override
  String get backgroundReliabilityAllSet => 'Everything NeoCloud needs is set.';

  @override
  String backgroundReliabilityOpen(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count settings to check',
      one: '1 setting to check',
    );
    return '$_temp0';
  }

  @override
  String get backgroundReliabilityCheck => 'Check';

  @override
  String get backgroundStepSamsungUnrestricted =>
      'Settings › Apps › NeoCloud › Battery › Unrestricted';

  @override
  String get backgroundStepSamsungNeverSleeping =>
      'Settings › Battery › Background usage limits › Never sleeping apps › add NeoCloud';

  @override
  String get backgroundStepXiaomiAutostart =>
      'Settings › Apps › Manage apps › NeoCloud › turn on Autostart';

  @override
  String get backgroundStepXiaomiNoRestrictions =>
      'Settings › Apps › Manage apps › NeoCloud › Battery saver › No restrictions';

  @override
  String get backgroundStepHuaweiAppLaunch =>
      'Settings › Battery › App launch › NeoCloud › Manage manually, with Auto-launch, Secondary launch and Run in background on';

  @override
  String get backgroundStepOppoBatteryUsage =>
      'Settings › Apps › NeoCloud › Battery usage › allow background activity and auto launch';

  @override
  String get backgroundStepOppoDontOptimise =>
      'Settings › Battery › Battery optimisation › NeoCloud › Don’t optimise';

  @override
  String get backgroundStepVivoBackgroundPower =>
      'Settings › Battery › Background power consumption management › NeoCloud › Allow';

  @override
  String get backgroundStepVivoAutostart =>
      'i Manager › App manager › Autostart manager › turn on NeoCloud';

  @override
  String get backgroundStepAsusAutostart =>
      'Mobile Manager › PowerMaster › Auto-start manager › NeoCloud › Allow';

  @override
  String get backgroundStepLockInRecents =>
      'Open the recent apps and lock NeoCloud there, so clearing them leaves it running';

  @override
  String get cameraBackUpNow => 'Back up now';

  @override
  String get cameraRetryFailed => 'Retry failed';

  @override
  String get cameraExportLog => 'Export upload log';

  @override
  String get cameraLogTitle => 'Upload log';

  @override
  String get cameraLogEmpty => 'Nothing logged yet.';

  @override
  String get cameraListEmpty => 'Nothing here.';

  @override
  String get cameraReasonWifi => 'waiting for Wi-Fi';

  @override
  String get cameraReasonIcloud => 'waiting on iCloud';

  @override
  String get cameraReasonNotAvailable =>
      'the system couldn’t provide this file';

  @override
  String cameraAttempts(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count attempts',
      one: '1 attempt',
    );
    return '$_temp0';
  }

  @override
  String get cameraNeverRun => 'Not run yet';

  @override
  String get cameraUnnamed => 'Not read yet';

  @override
  String get cameraOptions => 'Options';

  @override
  String get mobileNavCamera => 'Camera';

  @override
  String mobilePermissionsSet(int granted, int total) {
    return '$granted of $total set';
  }

  @override
  String mobilePermissionStep(int step, int total) {
    return 'Step $step of $total';
  }

  @override
  String mobileQrLoginExpiresAt(String time) {
    return 'Expires $time';
  }

  @override
  String get mobileRecentServers => 'Recent servers';

  @override
  String get mobileForgetServer => 'Forget';

  @override
  String get filesQuickAccess => 'Quick access';

  @override
  String get filesMoreActions => 'More actions';

  @override
  String filesMemberRemoveConfirm(String name, String item) {
    return '$name will no longer be able to open “$item”.';
  }

  @override
  String get done => 'Done';

  @override
  String deviceRevokeTitle(String name) {
    return 'Disconnect $name?';
  }

  @override
  String get deviceRevokeMessage =>
      'The app on that device is signed out and has to be connected again.';

  @override
  String get qrPairing => 'QR pairing';

  @override
  String get pairPhoneDescription =>
      'Show a QR code that signs the NeoCloud app on a phone in to this account, without typing an address or a password.';

  @override
  String get showQrCode => 'Show QR code';

  @override
  String get pairPhone => 'Pair a phone';

  @override
  String get pairTitle => 'Pair a phone';

  @override
  String get pairInstructions =>
      'Open the NeoCloud app on your phone, tap “Scan QR code” and point the camera at this code.';

  @override
  String get pairQrSemantics => 'QR code to pair a phone';

  @override
  String get pairWaiting => 'Waiting for the phone';

  @override
  String pairExpiresIn(String time) {
    return 'Expires in $time';
  }

  @override
  String get pairExpired => 'Code expired';

  @override
  String get pairNewCode => 'New code';

  @override
  String pairPairedWith(String device) {
    return 'Paired with $device ✓';
  }

  @override
  String get pairUnknownDevice => 'your phone';

  @override
  String get pairCantScan => 'Can\'t scan? Open this link on the phone:';

  @override
  String get pairLandingTitle => 'Pair with the NeoCloud app';

  @override
  String get pairLandingPhoneBody =>
      'This link signs in the NeoCloud app on this phone. Install the app if you haven\'t, then open the link in it. The code works for five minutes.';

  @override
  String get pairLandingOpenApp => 'Open in NeoCloud app';

  @override
  String get pairLandingAppStore => 'Get it on the App Store';

  @override
  String get pairLandingPlayStore => 'Get it on Google Play';

  @override
  String get pairLandingComputerBody =>
      'This link is meant for your phone. Open it there, or scan the QR code in Settings › Sign-in & security with the NeoCloud app.';

  @override
  String get pairLandingInvalid =>
      'This pairing link is incomplete. Show a new code in Settings › Sign-in & security.';

  @override
  String get connectOr => 'or';

  @override
  String get connectScanQr => 'Scan QR code';

  @override
  String get connectScanQrHint =>
      'In the web app, open Settings › Sign-in & security › Pair a phone.';

  @override
  String get scanTitle => 'Scan QR code';

  @override
  String get scanPrimeTitle => 'Use the camera to scan';

  @override
  String get scanPrimeBody =>
      'NeoCloud needs the camera only to read NeoCloud QR codes. Nothing is recorded or uploaded.';

  @override
  String get scanPrimeAllow => 'Allow camera';

  @override
  String get scanHint =>
      'Point the camera at the QR code in Settings › Sign-in & security';

  @override
  String get scanWrongCode => 'That\'s not a NeoCloud QR code';

  @override
  String get scanTorchOn => 'Turn on the light';

  @override
  String get scanTorchOff => 'Turn off the light';

  @override
  String get scanDeniedTitle => 'Camera access is off';

  @override
  String get scanDeniedBodyIos =>
      'Allow NeoCloud to use the camera in Settings, or connect with the server address instead.';

  @override
  String get scanDeniedBodyAndroid =>
      'Allow the camera when asked, or turn it on under Settings › Apps › NeoCloud › Permissions. You can also connect with the server address.';

  @override
  String get scanOpenSettings => 'Open Settings';

  @override
  String get scanFailedTitle => 'The camera didn\'t start';

  @override
  String get scanFailedBody =>
      'Close other apps that use the camera and try again.';

  @override
  String get webDavTitle => 'Connect apps (WebDAV)';

  @override
  String get webDavDescription =>
      'Finder, Windows Explorer, rclone and many other apps open NeoCloud as a network drive. They sign in with an app password, not your account password.';

  @override
  String get webDavAddress => 'WebDAV address';

  @override
  String get webDavPassword => 'App password';

  @override
  String get webDavPasswordHint =>
      'One per app, so you can disconnect each on its own. App passwords are listed above with a WebDAV tag.';

  @override
  String get webDavCreatePassword => 'Create app password';

  @override
  String get webDavPasswordName => 'Which app is it for?';

  @override
  String get webDavPasswordDefaultName => 'WebDAV';

  @override
  String get webDavPasswordTitle => 'Your app password';

  @override
  String get webDavPasswordMessage =>
      'Enter it in the app together with your user name. It is shown only once.';

  @override
  String get webDavRevokeMessage =>
      'Apps that use this app password can\'t reach your files any more.';

  @override
  String get webDavTag => 'WebDAV';

  @override
  String get webDavHowTo => 'How to connect common apps';

  @override
  String get webDavHintFinderTitle => 'Finder (macOS)';

  @override
  String get webDavHintFinder =>
      'Go › Connect to Server (⌘K), paste the address, then sign in with your user name and an app password.';

  @override
  String get webDavHintWindowsTitle => 'Windows';

  @override
  String get webDavHintWindows =>
      'In File Explorer, right-click This PC › Map network drive, paste the address and tick “Connect using different credentials”. Needs HTTPS.';

  @override
  String get webDavHintRcloneTitle => 'rclone';

  @override
  String get webDavHintRclone =>
      'rclone config › New remote › WebDAV, vendor “owncloud”, then the address, user name and app password.';

  @override
  String get webDavHintObsidianTitle => 'Obsidian';

  @override
  String get webDavHintObsidian =>
      'Install the Remotely Save plugin, choose WebDAV and enter the address, user name and app password.';

  @override
  String get webDavHintKeepassTitle => 'KeePassXC and KeePass apps';

  @override
  String get webDavHintKeepass =>
      'Open the database from the mounted drive in KeePassXC; on phones, KeePassDX and Strongbox open WebDAV addresses directly.';

  @override
  String get pageImport => 'Import';

  @override
  String get pageImportDescription =>
      'Move your files over from Nextcloud or another WebDAV server.';

  @override
  String get importNextcloudTitle => 'Move from Nextcloud';

  @override
  String get importNextcloudDescription =>
      'NeoCloud copies your files with their folders and dates. Nothing on the other server is changed.';

  @override
  String get importServerUrl => 'Server address';

  @override
  String get importAppPassword => 'App password';

  @override
  String get importAppPasswordHelpLink => 'How do I create an app password?';

  @override
  String get importAppPasswordHelpTitle => 'App password in Nextcloud';

  @override
  String get importAppPasswordHelp =>
      'In Nextcloud, open your avatar › Personal settings › Security. Under “Devices & sessions”, enter a name such as NeoCloud and click “Create new app password”. Copy the password shown and paste it here. NeoCloud keeps it encrypted and deletes it when the import ends.';

  @override
  String get importAppPasswordDocs => 'Nextcloud help';

  @override
  String get importSource => 'Source folder';

  @override
  String get importSourceHint =>
      'The folder on the other server; / takes everything.';

  @override
  String get importTarget => 'Import into';

  @override
  String get importChooseFolder => 'Choose folder';

  @override
  String get importTest => 'Test connection';

  @override
  String importTestFound(String files, String size) {
    return 'Connected. $files items in this folder, $size in total.';
  }

  @override
  String get importTestFailed =>
      'The server could not be read. Check the address, user name and app password.';

  @override
  String importQuotaWarning(String size) {
    return 'This won\'t fit: you need $size more space than your storage has left. The import stops when it is full.';
  }

  @override
  String get importStart => 'Start import';

  @override
  String get importStarted =>
      'Import started. You can leave this page; it keeps running.';

  @override
  String get importJobsTitle => 'Imports';

  @override
  String get importJobsEmpty => 'No imports yet.';

  @override
  String importInto(String folder) {
    return 'Into $folder';
  }

  @override
  String importFilesOf(String done, String total) {
    return '$done of $total files';
  }

  @override
  String importFilesDone(String done) {
    return '$done files';
  }

  @override
  String importBytesOf(String done, String total) {
    return '$done of $total';
  }

  @override
  String importSkipped(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count skipped (already there)',
      one: '1 skipped (already there)',
    );
    return '$_temp0';
  }

  @override
  String importFailed(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count failed',
      one: '1 failed',
    );
    return '$_temp0';
  }

  @override
  String importShowErrors(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: 'Show $count errors',
      one: 'Show 1 error',
    );
    return '$_temp0';
  }

  @override
  String get importHideErrors => 'Hide errors';

  @override
  String get importPause => 'Pause';

  @override
  String get importResume => 'Resume';

  @override
  String get importCancel => 'Cancel import';

  @override
  String get importCancelTitle => 'Cancel this import?';

  @override
  String get importCancelMessage =>
      'Files copied so far stay in NeoCloud. You can resume later; files already there are skipped.';

  @override
  String get importOpenFolder => 'Open folder';

  @override
  String get importRemove => 'Remove from list';

  @override
  String get importStatusQueued => 'Queued';

  @override
  String get importStatusScanning => 'Scanning';

  @override
  String get importStatusRunning => 'Importing';

  @override
  String get importStatusPaused => 'Paused';

  @override
  String get importStatusDone => 'Done';

  @override
  String get importStatusFailed => 'Failed';

  @override
  String get importStatusCancelled => 'Cancelled';

  @override
  String get filesWelcomeImportBody =>
      'Import your files with their folders and dates.';

  @override
  String get filesWelcomeImportAction => 'Import your files';

  @override
  String get searchFileContents => 'File contents';

  @override
  String get searchFileContentsHint =>
      'Also find files by the words inside them';

  @override
  String get searchMatchName => 'Name';

  @override
  String get searchMatchContent => 'Content';

  @override
  String get searchMatchBoth => 'Name + content';

  @override
  String get searchSettingsTitle => 'Search';

  @override
  String get searchSettingsContents => 'Search inside my files';

  @override
  String get searchSettingsContentsHint =>
      'NeoCloud indexes the text in your documents so search finds the words in them. Turn this off to keep your files out of the index.';

  @override
  String get searchSettingsContentsOff =>
      'Searching inside documents isn’t turned on in your NeoCloud yet. Once it is, this decides whether your files are included.';

  @override
  String get adminSearchTitle => 'Full-text search';

  @override
  String get adminSearchDescription =>
      'Lets people find files by the words inside them: text, PDF and Office documents. Vault files are never indexed.';

  @override
  String get adminSearchToggle => 'Full-text search';

  @override
  String get adminSearchPrivacy =>
      'Trade-off: the index stores which words each file contains. The text itself is encrypted, but anyone holding the database can tell whether a file contains a given word. Each user can opt out in their settings.';

  @override
  String get adminSearchState => 'Index';

  @override
  String get adminSearchOff => 'Off';

  @override
  String get adminSearchIndexing => 'Indexing';

  @override
  String get adminSearchUpToDate => 'Up to date';

  @override
  String get adminSearchIndexed => 'Indexed files';

  @override
  String get adminSearchPending => 'Waiting';

  @override
  String get adminSearchFailed => 'Failed';

  @override
  String get adminSearchFailedHint =>
      'Damaged or password-protected files can\'t be read.';

  @override
  String get adminSearchSize => 'Index size';

  @override
  String get adminSearchPdf => 'PDF text';

  @override
  String get adminSearchPdfFallback =>
      'Install poppler-utils for better PDF text; the built-in reader is used until then.';

  @override
  String get adminSearchOffice => 'Office documents';

  @override
  String get adminSearchToolPdftotext => 'pdftotext';

  @override
  String get adminSearchToolAvailable => 'Available';

  @override
  String get adminSearchToolMissing => 'Not installed';

  @override
  String get adminSearchRebuild => 'Rebuild index';

  @override
  String get adminSearchRebuildHint =>
      'Reads every file again, in the background.';

  @override
  String get adminSearchRebuildStarted =>
      'The index is being rebuilt in the background.';

  @override
  String get importTargetHint =>
      'Without a folder, files go into a new “Nextcloud” folder in Home.';

  @override
  String get importHostBlocked =>
      'NeoCloud can’t import from an address on your local network.';

  @override
  String get filesWelcomeDontShowAgain => 'Don’t show again';

  @override
  String get filesQuickAccessShow => 'Show quick access';

  @override
  String get filesQuickAccessHide => 'Hide quick access';

  @override
  String get adminSafetyAllDone => 'All done';

  @override
  String adminSafetyProgress(int done, int total) {
    return '$done of $total done';
  }

  @override
  String get adminSafetyExpand => 'Show checklist';

  @override
  String get adminSafetyCollapse => 'Hide checklist';

  @override
  String get authSignInWithSecurityKey => 'Sign in with security key';

  @override
  String get authOrContinueWith => 'or continue with';

  @override
  String get passkeyWebOnly =>
      'Security keys are only available in the NeoCloud web app.';

  @override
  String get passkeyNoneProvided => 'No security key was provided.';

  @override
  String get passkeyPromptDismissed => 'Security key prompt was dismissed.';

  @override
  String get passkeyAlreadyRegistered =>
      'That security key is already registered on this account.';

  @override
  String get passkeyCouldNotBeUsed => 'The security key could not be used.';

  @override
  String get passkeySignInFailed =>
      'Security key sign-in could not be started.';

  @override
  String get passkeyRegistrationFailed =>
      'The security key registration could not be started.';

  @override
  String get passkeysTitle => 'Security keys';

  @override
  String get passkeysDescription =>
      'Sign in with a hardware key or passkey instead of your password. A key that asks for a PIN or fingerprint also replaces your two-factor code.';

  @override
  String get passkeysNone => 'None';

  @override
  String passkeysRegistered(int count) {
    return '$count registered';
  }

  @override
  String get passkeyAdd => 'Add security key';

  @override
  String get passkeyNameOptional => 'Name (optional)';

  @override
  String get passkeyNameHint => 'YubiKey, MacBook Touch ID, …';

  @override
  String passkeyDefaultName(int number) {
    return 'Security key $number';
  }

  @override
  String get passkeyRenameTitle => 'Rename security key';

  @override
  String get passkeyName => 'Name';

  @override
  String get passkeyRemove => 'Remove';

  @override
  String settingsPasskeyRemoveTitle(String name) {
    return 'Remove “$name”?';
  }

  @override
  String get settingsPasskeyRemoveMessage =>
      'This key can no longer sign in to your account. Keep at least one other way in.';

  @override
  String passkeyAddedLastUsed(String added, String lastUsed) {
    return 'Added $added · Last used $lastUsed';
  }

  @override
  String get passkeyNotRecorded => 'Not recorded';

  @override
  String get passkeyNeverUsed => 'Never used';

  @override
  String get passkeyUnsupported =>
      'This device cannot register security keys. Open NeoCloud in a browser over HTTPS to add one.';

  @override
  String get passkeyAddInBrowser =>
      'Security keys and passkeys are added in the NeoCloud web app in a browser. Keys added there are listed here, and you can rename or remove them.';

  @override
  String get navSharedWithMe => 'Shared with me';

  @override
  String get sharedWithMeSubtitle =>
      'Files and folders other people on NeoCloud shared with you.';

  @override
  String get sharedWithMeEmpty => 'Nothing shared with you yet';

  @override
  String get sharedWithMeEmptySubtitle =>
      'When someone shares a file or folder with you, it shows up here.';

  @override
  String sharedBy(String owner) {
    return 'Shared by $owner';
  }

  @override
  String sharedByCanEdit(String owner) {
    return 'Shared by $owner · you can edit';
  }

  @override
  String sharedByReadOnly(String owner) {
    return 'Shared by $owner · view only';
  }

  @override
  String get sharedWithPeople => 'Shared with people';

  @override
  String get sharedLeave => 'Leave share';

  @override
  String sharedLeaveConfirm(String name, String owner) {
    return 'You will no longer see “$name”. Only $owner can share it with you again.';
  }

  @override
  String get sharedSaveToMyFiles => 'Save to my files';

  @override
  String sharedSavedToMyFiles(String name) {
    return '“$name” copied to your files.';
  }

  @override
  String get sharedReadOnlyError =>
      'This folder is shared with you to view only.';

  @override
  String get sharedBoundaryError =>
      'Items can’t be moved into or out of a shared folder. Copy them instead.';

  @override
  String get sharedRootError =>
      'The shared folder itself belongs to its owner.';

  @override
  String get sharedZipMixedError =>
      'Download your own files and shared files separately.';

  @override
  String get sharePeopleTitle => 'Share with people';

  @override
  String get sharePeopleHelp =>
      'Enter the exact email address or username of someone who uses this NeoCloud.';

  @override
  String get sharePublicLinkTitle => 'Public link';

  @override
  String get memberRecipientHint => 'Email address or username';

  @override
  String get memberAdd => 'Add';

  @override
  String get memberCanView => 'Can view';

  @override
  String get memberCanEdit => 'Can edit';

  @override
  String get memberChange => 'Change access';

  @override
  String get memberRemove => 'Remove access';

  @override
  String get memberExpired => 'Expired';

  @override
  String get membersEmpty => 'Not shared with anyone yet.';

  @override
  String get memberNotFound =>
      'No account found that this can be shared with. Check the exact email address or username.';

  @override
  String get memberAlreadyAdded => 'Already shared with this person.';

  @override
  String get authOrUsePassword => 'or use your password';

  @override
  String get qrLoginScanTitle => 'Scan with NeoCloud on your phone';

  @override
  String get qrLoginScanBody =>
      'Open the NeoCloud app on a signed-in phone and tap Scan QR code.';

  @override
  String qrLoginRefreshesIn(int seconds) {
    return 'New code in ${seconds}s';
  }

  @override
  String get qrLoginPreparing => 'Preparing code…';

  @override
  String get qrLoginNewCode => 'New code';

  @override
  String get qrLoginShowCode => 'Sign in with your phone';

  @override
  String get qrLoginWaitingTitle => 'Check your phone';

  @override
  String get qrLoginWaitingBody =>
      'Approve the sign-in on your phone to continue here.';

  @override
  String get qrLoginSigningInOn => 'Signing in on';

  @override
  String get qrLoginFrom => 'From';

  @override
  String get qrLoginWaitingForApproval => 'Waiting for approval';

  @override
  String get qrLoginCancelAndUsePassword => 'Cancel and use password';

  @override
  String get qrLoginCouldNotStart => 'The QR code couldn\'t be prepared.';

  @override
  String get qrLoginRateLimited =>
      'Too many QR codes in a short time. Wait a moment, then try again.';

  @override
  String get qrLoginDeclinedOnPhone => 'The sign-in was declined on the phone.';

  @override
  String get qrLoginExpired => 'That code expired. Scan the new one.';

  @override
  String get scanQrSubtitle => 'Sign in a browser or approve a device';

  @override
  String get scanAnyHint =>
      'Point the camera at the QR code on the other device';

  @override
  String get qrLoginApproveTitle => 'Approve sign-in?';

  @override
  String qrLoginApproveHint(String account) {
    return 'Only approve if you started this sign-in yourself. It will be signed in as $account.';
  }

  @override
  String get qrLoginApprove => 'Approve';

  @override
  String get qrLoginDecline => 'Decline';

  @override
  String qrLoginRequestedAt(String time) {
    return 'Requested $time';
  }

  @override
  String get qrLoginPreviewExpired =>
      'This request has expired. Show a new code on the other device.';

  @override
  String get qrLoginPreviewUsed => 'This request has already been used.';

  @override
  String qrLoginApproved(String device) {
    return '$device is signed in.';
  }

  @override
  String get qrLoginDeclined => 'Sign-in declined.';

  @override
  String get qrLoginCouldNotApprove => 'The sign-in couldn\'t be approved.';

  @override
  String scanOtherServer(String server) {
    return 'This code belongs to another server ($server).';
  }

  @override
  String get cameraOfferTitle => 'Back up your photos?';

  @override
  String get cameraOfferBody =>
      'NeoCloud can upload new photos and videos from this phone automatically, encrypted like every other file.';

  @override
  String get cameraOfferSetUp => 'Set up';

  @override
  String get notNow => 'Not now';

  @override
  String get connectWithBrowser => 'Sign in with the browser';

  @override
  String get connectChangeServer => 'Change server';

  @override
  String get signInInvalidCredentials => 'Wrong username, email or password.';

  @override
  String get signInInvalidCode =>
      'That code isn\'t right. Check your authenticator app and try again.';

  @override
  String signInLockedMinutes(int minutes) {
    return 'Too many failed sign-in attempts for this account. Try again in $minutes min.';
  }

  @override
  String signInLockedSeconds(int seconds) {
    return 'Too many failed sign-in attempts for this account. Try again in $seconds s.';
  }

  @override
  String get signInRateLimited =>
      'Too many attempts. Wait a few minutes, then try again.';

  @override
  String get approvalNeedsServerUpdate =>
      'Approving sign-ins in the app isn\'t available yet. Approve in a browser where you\'re signed in instead.';

  @override
  String get serverUpdateNeeded => 'This feature isn\'t available yet.';

  @override
  String get approvalNeedsPhone =>
      'Approve sign-ins from the NeoCloud phone app or a browser where you\'re signed in.';

  @override
  String get connectWelcome => 'WELCOME TO NEOCLOUD';

  @override
  String get connectReadyToSignIn => 'Ready to sign in';

  @override
  String get connectEnterAddress => 'Enter an address manually';

  @override
  String get connectEnterAddressHint =>
      'Your server\'s address, such as cloud.example.com';

  @override
  String get connectServerAddress => 'NeoCloud server address';

  @override
  String get connectConnecting => 'Connecting…';

  @override
  String get connectToThisServer => 'Connect to this server';

  @override
  String get connectForgetServerBody =>
      'Remove this server from the list? You can still enter its address.';

  @override
  String authSigningInTo(String host) {
    return 'Signing in to $host';
  }

  @override
  String get authShowPassword => 'Show password';

  @override
  String get authHidePassword => 'Hide password';

  @override
  String get authForgotPasswordAskAdmin =>
      'Forgot your password? Ask the person who runs your NeoCloud to reset it.';

  @override
  String get authPhoneSignInTitle => 'Sign in to NeoCloud';

  @override
  String get authPhoneSignInSubtitle =>
      'Scan the code on a signed-in computer, or use your password.';

  @override
  String get aiPage => 'AI';

  @override
  String get aiPageDescription =>
      'Let AI suggest names, folders and tags for your files.';

  @override
  String get aiOptInTitle => 'AI features';

  @override
  String get aiOptIn => 'Use AI features';

  @override
  String get aiPrivacyNote =>
      'When AI looks at a file, its name, text and preview are sent to the AI service. Files in a vault never are, and nothing changes until you approve a suggestion.';

  @override
  String get aiModelsTitle => 'Models';

  @override
  String get aiModelsDescription =>
      'Which AI model handles each kind of work. Auto picks the best one available.';

  @override
  String get aiRoleText => 'Everyday tasks';

  @override
  String get aiRoleTextDescription => 'Names, tags and sorting.';

  @override
  String get aiRoleSmart => 'Summaries';

  @override
  String get aiRoleSmartDescription => 'Summaries and questions about a file.';

  @override
  String get aiRoleVision => 'Images';

  @override
  String get aiRoleVisionDescription =>
      'Reads photos, screenshots and scans. Only models that can see images.';

  @override
  String get aiRoleEmbedding => 'Similar files';

  @override
  String get aiRoleEmbeddingDescription =>
      'Finds similar files and folders for sorting.';

  @override
  String get aiRoleNotReady => 'No model is available for this yet.';

  @override
  String get aiAuto => 'Auto';

  @override
  String get aiAutoSubtitle => 'The best available model';

  @override
  String aiSelectModel(String name) {
    return 'Select $name';
  }

  @override
  String get aiSearchModels => 'Search…';

  @override
  String aiNoResults(String query) {
    return 'No results for \"$query\"';
  }

  @override
  String get aiNoModelsForRole => 'No model can do this yet.';

  @override
  String get aiAll => 'All';

  @override
  String get aiOther => 'Other';

  @override
  String get aiProviderCustom => 'Custom OpenAI-compatible';

  @override
  String get aiGranted => 'Granted';

  @override
  String get aiGrantedHint =>
      'This model was unlocked just for you. It is switched off for everyone else.';

  @override
  String get aiByokTag => 'Your key';

  @override
  String get aiPriceFree => 'Free';

  @override
  String get aiPriceUnknown => 'Price unknown';

  @override
  String aiPricePerMillion(String price) {
    return '\$$price / 1M input';
  }

  @override
  String get aiNoModelTitle => 'AI isn\'t set up yet';

  @override
  String get aiNoModelBody =>
      'Ask the person who runs your NeoCloud, or use your own NeoRouter account or API key under Advanced.';

  @override
  String get aiNoModelAdminHint =>
      'Set up AI for everyone in Admin, or use your own NeoRouter account or API key under Advanced.';

  @override
  String get aiOpenAdmin => 'Open Admin';

  @override
  String get aiDisabledTitle => 'AI is turned off';

  @override
  String get aiDisabledBody =>
      'AI features aren\'t turned on. Ask the person who runs your NeoCloud if you\'d like to use them.';

  @override
  String get aiDisabledAdminHint =>
      'AI features aren\'t turned on. You can turn them on for everyone in Admin.';

  @override
  String get aiNotAvailableTitle => 'AI isn\'t available yet';

  @override
  String get aiNotAvailableBody => 'This feature isn\'t available yet.';

  @override
  String get aiAdminNotAvailableTitle => 'AI isn\'t available on this server';

  @override
  String get aiAdminNotAvailableBody =>
      'Update NeoCloud on the server to use AI features.';

  @override
  String get aiFeaturesTitle => 'Suggestions';

  @override
  String get aiFeaturesDescription =>
      'What AI may suggest. You review every suggestion before anything changes.';

  @override
  String get aiFeatureNames => 'Suggest names';

  @override
  String get aiFeatureNamesDescription =>
      'For files with meaningless names like IMG_1234 or scan.pdf.';

  @override
  String get aiFeatureFolders => 'Suggest folders';

  @override
  String get aiFeatureFoldersDescription =>
      'Where a file belongs among your existing folders.';

  @override
  String get aiFeatureSummaries => 'Summaries';

  @override
  String get aiFeatureSummariesDescription =>
      'A short summary in a file\'s details.';

  @override
  String get aiFeatureOnUpload => 'On upload';

  @override
  String get aiFeatureOnUploadDescription =>
      'Suggest for new files as they arrive.';

  @override
  String get aiNamingTemplate => 'Naming template';

  @override
  String get aiNamingTemplateDescription =>
      'How suggested names are built from the date, issuer and subject the AI finds.';

  @override
  String get aiUsageTitle => 'AI usage this month';

  @override
  String aiUsageOf(String used, String limit) {
    return '$used of $limit tokens';
  }

  @override
  String aiUsageUnlimited(String used) {
    return '$used tokens · no limit';
  }

  @override
  String aiUsagePercent(int percent) {
    return '$percent% used · resets on the 1st';
  }

  @override
  String get aiUsageNoLimit => 'No monthly limit';

  @override
  String get aiAdvanced => 'Advanced';

  @override
  String get aiAdvancedDescription =>
      'Models, your own NeoRouter account and API keys.';

  @override
  String get aiByokTitle => 'Your own AI service';

  @override
  String get aiByokDescription =>
      'Use your own NeoRouter account or API keys for your AI features. Keys are stored encrypted and never shown again.';

  @override
  String get aiByokNoProviders => 'There\'s nothing to connect yet.';

  @override
  String get aiByokUsingYourKey => 'Using your key';

  @override
  String get aiNotSetUp => 'Not set up';

  @override
  String get aiUpdate => 'Update';

  @override
  String get aiConnectEndpoint => 'Connect endpoint';

  @override
  String get aiAddKey => 'Add key';

  @override
  String get aiRemove => 'Remove';

  @override
  String aiRemoveKeyTitle(String provider) {
    return 'Remove $provider key?';
  }

  @override
  String aiRemoveKeyBody(String provider) {
    return 'NeoCloud stops using your $provider key. This can’t be undone.';
  }

  @override
  String aiKeyRemoved(String provider) {
    return '$provider key removed.';
  }

  @override
  String get aiEnterKeyFirst => 'Enter an API key first.';

  @override
  String get aiConnectionGood => 'Connection looks good.';

  @override
  String get aiCouldNotConnect => 'Could not connect.';

  @override
  String get aiKeyRequired => 'An API key is required.';

  @override
  String get aiBaseUrlRequired => 'A base URL is required for this endpoint.';

  @override
  String get aiCustomEndpointTitle => 'Connect a custom endpoint';

  @override
  String get aiCustomEndpointBody =>
      'Point NeoCloud at any OpenAI-compatible Chat Completions API, such as a self-hosted model or another hosted service.';

  @override
  String aiYourKeyTitle(String provider) {
    return 'Your $provider key';
  }

  @override
  String aiYourKeyBody(String provider) {
    return 'Paste your $provider API key below. It’s stored encrypted and only used for your own AI features.';
  }

  @override
  String get aiNameOptional => 'Name (optional)';

  @override
  String get aiNameHint => 'e.g. My own model';

  @override
  String get aiBaseUrl => 'Base URL';

  @override
  String get aiBaseUrlOptional => 'Base URL (optional)';

  @override
  String get aiBaseUrlHelper =>
      'Must be reachable on the public internet. Local and private network addresses aren\'t allowed.';

  @override
  String get aiApiKey => 'API key';

  @override
  String get aiTestConnection => 'Test connection';

  @override
  String get aiByokLimitsNote =>
      'The monthly AI limit doesn\'t apply to models running on your own key.';

  @override
  String get aiShow => 'Show';

  @override
  String get aiHide => 'Hide';

  @override
  String get aiNeoRouterConnected => 'NeoRouter is connected.';

  @override
  String get aiNeoRouterDisconnectTitle => 'Disconnect NeoRouter?';

  @override
  String get aiNeoRouterDisconnectBody =>
      'NeoCloud stops using this NeoRouter. To withdraw its key as well, revoke it under API keys in NeoRouter.';

  @override
  String aiNeoRouterWaiting(String code) {
    return 'Approve NeoCloud in NeoRouter. It shows the code $code.';
  }

  @override
  String get aiNeoRouterOpenAgain => 'Open again';

  @override
  String get aiNeoRouterServerInUse =>
      'NeoCloud already uses a NeoRouter for everyone. Connect your own NeoRouter account to use your own models and limit.';

  @override
  String get aiNeoRouterUserDescription =>
      'Use your own NeoRouter account for the work you pick under Manage.';

  @override
  String get aiNeoRouterServerDescription =>
      'Connect your NeoRouter once and every account here uses it for the work switched on under Manage.';

  @override
  String get aiNeoRouterUnreachable => 'NeoRouter cannot be reached right now.';

  @override
  String get aiNeoRouterConnect => 'Connect NeoRouter';

  @override
  String get aiNeoRouterAddress => 'NeoRouter address';

  @override
  String get aiNeoRouterRoleChat => 'Text and vision';

  @override
  String get aiNeoRouterRoleEmbeddings => 'Similarity search';

  @override
  String get aiNeoRouterRoleTranscription => 'Speech to text';

  @override
  String get aiNeoRouterRoleImages => 'Image generation';

  @override
  String aiNeoRouterModelCount(int count) {
    return '$count available in your NeoRouter';
  }

  @override
  String aiNeoRouterNotAllowed(int count) {
    return '$count in your NeoRouter, but NeoCloud’s key may not use them yet (for example paid ones). Allow them for NeoCloud under API keys in NeoRouter.';
  }

  @override
  String get aiNeoRouterNoModels => 'Nothing available in your NeoRouter yet.';

  @override
  String get aiNeoRouterCancelled => 'The sign-in was cancelled.';

  @override
  String get aiNeoRouterExpired => 'This sign-in expired. Start again.';

  @override
  String get aiNeoRouterFailed => 'NeoRouter didn\'t approve the sign-in.';

  @override
  String get aiNeoRouterCouldNotOpen =>
      'NeoRouter couldn\'t be opened. Use Open again.';

  @override
  String get aiConnect => 'Connect';

  @override
  String get aiConnected => 'Connected';

  @override
  String get aiManage => 'Manage';

  @override
  String get aiAdminTitle => 'AI features';

  @override
  String get aiAdminDescription =>
      'Suggestions for names and folders, for accounts that opt in. Vault files are never processed.';

  @override
  String get aiAdminEnable => 'Enable AI features';

  @override
  String get aiAdminEnableDescription =>
      'Each account still turns them on for itself in Settings → AI.';

  @override
  String get aiAdminBudget => 'Monthly token budget';

  @override
  String get aiAdminBudgetDescription =>
      'Per account, unless an account has its own under Users. Empty means no limit.';

  @override
  String get aiBudgetInvalid => 'Enter a whole number of tokens.';

  @override
  String get aiNoLimit => 'No limit';

  @override
  String get aiProvidersTitle => 'Server provider credentials';

  @override
  String get aiProvidersDescription =>
      'API keys and endpoints for AI providers. They are shared by every account on this server; accounts can still add their own keys in Settings → AI.';

  @override
  String aiProvidersSetCount(int count, int total) {
    return '$count of $total set';
  }

  @override
  String get aiProvidersEmpty => 'This server reports no provider settings.';

  @override
  String get aiSet => 'Set';

  @override
  String get aiNotSet => 'Not set';

  @override
  String get aiReplace => 'Replace';

  @override
  String get aiChange => 'Change';

  @override
  String get aiSetUrl => 'Set URL';

  @override
  String get aiEndpointUrl => 'Endpoint URL';

  @override
  String get aiUrl => 'URL';

  @override
  String aiClearTitle(String name) {
    return 'Clear $name?';
  }

  @override
  String get aiClearUrlBody =>
      'This removes the address for every account on this server.';

  @override
  String get aiClearKeyBody =>
      'Every account on this server loses this shared key. Accounts that added their own key keep using it.';

  @override
  String aiCleared(String name) {
    return '$name cleared.';
  }

  @override
  String aiSavedName(String name) {
    return '$name saved.';
  }

  @override
  String get aiPasteAKey => 'Paste a key.';

  @override
  String get aiEnterAddress => 'Enter an address.';

  @override
  String get aiUrlInvalid =>
      'The address must be an http:// or https:// address.';

  @override
  String aiPasteNewKey(String hint) {
    return 'Paste a new key to replace $hint';
  }

  @override
  String get aiPasteTheKey => 'Paste the key';

  @override
  String aiProviderUrlBody(String name) {
    return 'Every account on this server uses this address for $name.';
  }

  @override
  String get aiProviderKeyBody =>
      'Used by every account on this server that has no key of its own. It is never shown again after saving.';

  @override
  String get aiModelAvailability => 'Model availability';

  @override
  String get aiModelAvailabilityDescription =>
      'Choose which models every account on this server can pick and run. While any model is switched off, models that providers add later start switched off too.';

  @override
  String get aiAutoEnable => 'Enable new models automatically';

  @override
  String get aiAutoEnableHint =>
      'When off, models a provider adds later stay disabled until you enable them.';

  @override
  String get aiSearchModelsOrProviders => 'Search models or providers';

  @override
  String get aiFilterEnabled => 'Enabled';

  @override
  String get aiFilterDisabled => 'Disabled';

  @override
  String aiShownCount(int count) {
    return '$count shown';
  }

  @override
  String get aiEnableAll => 'Enable all';

  @override
  String get aiEnableShown => 'Enable shown';

  @override
  String get aiDisableAll => 'Disable all';

  @override
  String get aiDisableShown => 'Disable shown';

  @override
  String get aiCollapseAll => 'Collapse all';

  @override
  String get aiExpandAll => 'Expand all';

  @override
  String get aiNoModelsYet =>
      'Add a provider credential first. Its models show up here once the provider answers.';

  @override
  String aiNoModelMatches(String query) {
    return 'No model matches \"$query\".';
  }

  @override
  String aiUnsavedChanges(int count) {
    return '$count unsaved changes';
  }

  @override
  String get aiDiscard => 'Discard';

  @override
  String get aiSaveChanges => 'Save changes';

  @override
  String get aiModelsSaved => 'Model availability saved.';

  @override
  String aiEnabledCount(int count, int total) {
    return '$count of $total enabled';
  }

  @override
  String get aiKindVision => 'Vision';

  @override
  String get aiKindEmbedding => 'Embedding';

  @override
  String get aiKindTranscription => 'Speech';

  @override
  String get aiAdminUsageDescription =>
      'Metadata only: who used which model and how many tokens, never what was sent.';

  @override
  String get aiUsageRequests => 'Requests';

  @override
  String get aiUsageTokens => 'Tokens';

  @override
  String get aiUsageErrors => 'Errors';

  @override
  String get aiUsageAccounts => 'Busiest accounts';

  @override
  String get aiUsageModels => 'Busiest models';

  @override
  String get aiUsageNone => 'No AI use this month yet.';

  @override
  String aiTokenCount(String count) {
    return '$count tokens';
  }

  @override
  String get aiAdminUserMenu => 'AI models and budget';

  @override
  String aiAdminUserTitle(String name) {
    return 'AI · $name';
  }

  @override
  String aiAdminUserSaved(String name) {
    return 'Saved AI settings for $name';
  }

  @override
  String get aiUserBudgetDefault => 'Server default';

  @override
  String get aiUserBudgetCustom => 'Custom';

  @override
  String aiUserBudgetDefaultValue(String value) {
    return 'Server default: $value';
  }

  @override
  String get aiTokensPerMonth => 'Tokens per month';

  @override
  String get aiExtraModels => 'Extra models';

  @override
  String aiExtraModelsIntro(String name) {
    return 'These models are switched off for everyone. Tick the ones $name may use anyway. Their use still counts toward this account\'s budget.';
  }

  @override
  String get aiExtraModelsEmpty =>
      'No models are switched off for everyone, so there is nothing extra to grant.';

  @override
  String get aiAdminTabProviders => 'Providers';

  @override
  String get aiAdminTabModels => 'Models';

  @override
  String get aiRoleRequestedUnavailable =>
      'Your choice isn’t available right now, so another model is used.';

  @override
  String get aiFeatureTags => 'Suggest tags';

  @override
  String get aiFeatureTagsDescription =>
      'Tags from a file’s content, like invoice or contract.';

  @override
  String get aiFeatureAutoApply =>
      'Apply high-confidence suggestions automatically';

  @override
  String get aiFeatureAutoApplyDescription =>
      'Otherwise every suggestion waits for you. Each applied change can be undone.';

  @override
  String get aiUsageFeatures => 'By feature';

  @override
  String get aiErrorDisabled => 'AI features aren\'t turned on.';

  @override
  String get aiErrorNotOptedIn => 'Turn on AI features in Settings → AI first.';

  @override
  String get aiErrorBudget =>
      'This month’s AI budget is used up. It resets on the 1st.';

  @override
  String get aiErrorNoModel => 'No AI model is available right now.';

  @override
  String get aiErrorProvider =>
      'The AI service didn’t answer properly. Try again later.';

  @override
  String get aiErrorVault => 'Files in a vault are never sent to AI.';

  @override
  String get aiErrorExcluded => 'This folder is excluded from AI.';

  @override
  String get aiErrorUrlNotAllowed =>
      'This address isn’t allowed. Use one on the public internet.';

  @override
  String get aiFolderExclude => 'Exclude from AI';

  @override
  String get aiFolderInclude => 'Include in AI';

  @override
  String aiFolderExcluded(String name) {
    return '$name is excluded from AI.';
  }

  @override
  String aiFolderIncluded(String name) {
    return '$name is included in AI again.';
  }

  @override
  String get aiFolderExcludedTag => 'excluded from AI';

  @override
  String get aiReviewNav => 'Review';

  @override
  String get aiReviewTitle => 'Review';

  @override
  String get aiReviewSubtitle =>
      'AI suggestions wait here until you decide. Nothing changes before you accept, and everything accepted can be undone.';

  @override
  String get aiReviewTabNames => 'Names';

  @override
  String get aiReviewTabFolders => 'Folders';

  @override
  String get aiReviewTabTags => 'Tags';

  @override
  String aiAcceptHigh(int count) {
    return 'Accept all high-confidence ($count)';
  }

  @override
  String aiAcceptHighTitle(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: 'Accept $count high-confidence suggestions?',
      one: 'Accept 1 high-confidence suggestion?',
    );
    return '$_temp0';
  }

  @override
  String get aiAcceptHighBody =>
      'Names, folders and tags across all tabs are applied at once. You can undo them right after.';

  @override
  String aiAcceptHighFailed(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count suggestions couldn’t be applied; they stay in the list.',
      one: '1 suggestion couldn’t be applied; it stays in the list.',
    );
    return '$_temp0';
  }

  @override
  String aiAcceptedMany(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count suggestions accepted',
      one: '1 suggestion accepted',
    );
    return '$_temp0';
  }

  @override
  String aiAcceptedRename(String name) {
    return 'Renamed to “$name”';
  }

  @override
  String aiAcceptedMove(String folder) {
    return 'Moved to $folder';
  }

  @override
  String aiAcceptedTags(String name) {
    return 'Tags added to “$name”';
  }

  @override
  String get aiAccept => 'Accept';

  @override
  String get aiReject => 'Reject';

  @override
  String get aiEditName => 'Use another name';

  @override
  String get aiEditFolder => 'Move somewhere else';

  @override
  String get aiEditTags => 'Edit tags';

  @override
  String get aiTagsLabel => 'Tags, separated by commas';

  @override
  String get aiTierHigh => 'High';

  @override
  String get aiTierMedium => 'Medium';

  @override
  String get aiTierLow => 'Low';

  @override
  String aiConfidence(int percent) {
    return '$percent % confident';
  }

  @override
  String get aiSourceAi => 'AI';

  @override
  String get aiSourceRule => 'Rule';

  @override
  String aiSourceModel(String model) {
    return 'Suggested by $model';
  }

  @override
  String get aiSourceRuleHint =>
      'One of your sorting rules matched; no model was asked.';

  @override
  String get aiNewFolder => 'new folder';

  @override
  String aiShowLow(int count) {
    return 'Show low-confidence ($count)';
  }

  @override
  String get aiHideLow => 'Hide low-confidence';

  @override
  String get aiLoadMore => 'Load more';

  @override
  String get aiReviewEmptyTitle => 'Nothing to review';

  @override
  String get aiReviewEmptyBody =>
      'New suggestions appear here. Ask for some from a file’s ⋯ menu, or with Organize this folder.';

  @override
  String get aiReviewOffTitle => 'AI is off for you';

  @override
  String get aiReviewOffBody =>
      'Turn on AI features in Settings › AI to get suggestions for names, folders and tags.';

  @override
  String get aiOpenSettings => 'Open settings';

  @override
  String get aiOpenReview => 'Open Review';

  @override
  String get aiSuggestionStale =>
      'The file changed since this was suggested, so the suggestion was removed.';

  @override
  String get aiSuggestionDecided =>
      'This suggestion was already decided elsewhere.';

  @override
  String aiUndoConflict(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count changes can’t be undone: the files changed since.',
      one: 'Can’t undo: the file changed since.',
    );
    return '$_temp0';
  }

  @override
  String aiRuleProposalTitle(String type, String issuer, String folder) {
    return 'Always move $type from $issuer to $folder?';
  }

  @override
  String aiRuleProposalBody(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other:
          'You accepted $count moves like this. A rule does it without asking AI.',
      one: 'You accepted 1 move like this.',
    );
    return '$_temp0';
  }

  @override
  String get aiCreateRule => 'Create rule';

  @override
  String get aiDismiss => 'Dismiss';

  @override
  String get aiRuleCreated => 'Rule created. Find it under Settings › AI.';

  @override
  String get aiSuggestionChip => 'AI suggestion';

  @override
  String get aiSuggestionChipHint => 'Show the AI suggestion';

  @override
  String get aiSuggestionTitle => 'AI suggestion';

  @override
  String get aiMenu => 'AI';

  @override
  String get aiSuggestName => 'Suggest a name';

  @override
  String get aiSuggestFolder => 'Suggest a folder';

  @override
  String get aiSuggestNames => 'Suggest names';

  @override
  String get aiSuggestFolders => 'Suggest folders';

  @override
  String get aiSuggestTags => 'Suggest tags';

  @override
  String get aiSummarize => 'Summarize';

  @override
  String aiRequested(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: 'Asked AI about $count files. Suggestions appear in Review.',
      one: 'Asked AI for a suggestion. It appears in Review.',
    );
    return '$_temp0';
  }

  @override
  String aiNothingSuggested(String name) {
    return 'AI has no suggestion for “$name”.';
  }

  @override
  String get aiOrganizeFolder => 'Organize this folder…';

  @override
  String aiOrganizeTitle(String name) {
    return 'Organize “$name”';
  }

  @override
  String get aiOrganizeIntro =>
      'AI looks at every file in this folder and its subfolders and suggests what you pick. You decide each suggestion in Review.';

  @override
  String get aiOrganizeNames => 'Names for files with meaningless names';

  @override
  String get aiOrganizeFolders => 'A better folder for each file';

  @override
  String get aiOrganizeTags => 'Tags';

  @override
  String get aiOrganizeKindOff => 'Off in Settings › AI';

  @override
  String get aiOrganizeNote =>
      'Vaults and folders excluded from AI are skipped. Up to 5,000 files.';

  @override
  String get aiOrganizeStart => 'Start';

  @override
  String get aiOrganizeRunning => 'AI is looking at the files…';

  @override
  String get aiOrganizeDone => 'Done';

  @override
  String get aiOrganizeQueued => 'Waiting to start…';

  @override
  String aiOrganizeProgress(int done, int total) {
    return '$done of $total files';
  }

  @override
  String aiOrganizeSkipped(int skipped, int failed) {
    return '$skipped skipped, $failed failed';
  }

  @override
  String get aiOrganizePaused => 'The work is paused for now.';

  @override
  String aiOrganizeWaiting(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count suggestions are waiting in Review.',
      one: '1 suggestion is waiting in Review.',
    );
    return '$_temp0';
  }

  @override
  String get aiOrganizeNothing => 'Nothing to suggest: these files look fine.';

  @override
  String get aiOrganizeBackground =>
      'You can close this; the work goes on and suggestions arrive in Review.';

  @override
  String get aiSummaryTitle => 'Summary';

  @override
  String get aiSummaryWorking => 'Summarizing…';

  @override
  String get aiSummaryRefresh => 'Summarize again';

  @override
  String aiSummaryBy(String model) {
    return 'Written by $model';
  }

  @override
  String get aiSummaryByAi => 'Written by AI';

  @override
  String get aiDetailsTitle => 'AI details';

  @override
  String get aiTagsTitle => 'Tags';

  @override
  String get aiFieldsTitle => 'Read from the file';

  @override
  String aiFieldsBy(String model) {
    return 'Read by $model';
  }

  @override
  String get aiFieldType => 'Type';

  @override
  String get aiFieldIssuer => 'From';

  @override
  String get aiFieldDate => 'Date';

  @override
  String get aiFieldSubject => 'Subject';

  @override
  String get aiFieldAmount => 'Amount';

  @override
  String get aiFieldCaption => 'Caption';

  @override
  String get aiDocTypeInvoice => 'Invoice';

  @override
  String get aiDocTypeReceipt => 'Receipt';

  @override
  String get aiDocTypeContract => 'Contract';

  @override
  String get aiDocTypePayslip => 'Payslip';

  @override
  String get aiDocTypeLetter => 'Letter';

  @override
  String get aiDocTypeStatement => 'Statement';

  @override
  String get aiDocTypeCertificate => 'Certificate';

  @override
  String get aiDocTypeManual => 'Manual';

  @override
  String get aiDocTypeReport => 'Report';

  @override
  String get aiDocTypeNotes => 'Notes';

  @override
  String get aiDocTypePresentation => 'Presentation';

  @override
  String get aiDocTypeSpreadsheet => 'Spreadsheet';

  @override
  String get aiDocTypeScreenshot => 'Screenshot';

  @override
  String get aiDocTypePhoto => 'Photo';

  @override
  String get aiDocTypeScan => 'Scan';

  @override
  String get aiDocTypeOther => 'Other';

  @override
  String get aiFeatureNewTags => 'Suggest new tags';

  @override
  String get aiFeatureNewTagsDescription =>
      'Otherwise AI only uses tags you already have.';

  @override
  String get aiFeatureNewFolders => 'Suggest new folders';

  @override
  String get aiFeatureNewFoldersDescription =>
      'Lets AI propose a folder that doesn’t exist yet; it is created when you accept.';

  @override
  String get aiReviewSettingsLabel => 'Suggestions to review';

  @override
  String aiReviewSettingsDescription(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count suggestions are waiting.',
      one: '1 suggestion is waiting.',
      zero: 'Nothing waiting right now.',
    );
    return '$_temp0';
  }

  @override
  String get aiRulesTitle => 'Sorting rules';

  @override
  String get aiRulesDescription =>
      'Checked before any model: a file that matches every condition of a rule is suggested for its folder.';

  @override
  String get aiRulesEmpty =>
      'No rules yet. Review offers one after you accept a few similar moves.';

  @override
  String get aiRuleAdd => 'Add rule';

  @override
  String get aiRuleEdit => 'Edit rule';

  @override
  String get aiRuleName => 'Name';

  @override
  String get aiRuleWhen => 'When a file is';

  @override
  String get aiRuleAnyType => 'Any type';

  @override
  String get aiRuleExtension => 'Extension';

  @override
  String get aiRulePattern => 'Name pattern (* and ?)';

  @override
  String get aiRuleTarget => 'Move it to';

  @override
  String get aiRuleChooseFolder => 'Choose folder';

  @override
  String aiRuleSummary(String conditions, String folder) {
    return '$conditions → $folder';
  }

  @override
  String get aiRuleNeedsName => 'Give the rule a name.';

  @override
  String get aiRuleNeedsCondition => 'Set at least one condition.';

  @override
  String get aiRuleNeedsFolder => 'Choose the folder files go to.';

  @override
  String get aiRuleDeleteTitle => 'Delete rule?';

  @override
  String aiRuleDeleteBody(String name) {
    return '“$name” stops sorting files. Suggestions it already made stay.';
  }

  @override
  String get aiErrorFeatureOff =>
      'This AI feature is turned off in Settings › AI.';

  @override
  String get aiErrorNoContent => 'There is no text in this file to work with.';

  @override
  String get aiErrorTooManyFiles =>
      'That folder has too many files for one go. Pick a smaller folder.';

  @override
  String get aiErrorTargetNotAllowed =>
      'AI can’t put files there: it is a vault or excluded from AI.';

  @override
  String get aiErrorShareBoundary =>
      'That move would change who can see the file, so AI doesn’t make it.';

  @override
  String get aiErrorNotUndoable => 'This change can’t be undone.';
}
