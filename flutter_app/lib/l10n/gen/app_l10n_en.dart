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
  String get connectionError => 'Can’t reach the NeoCloud server.';

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
      'This account will administer NeoCloud on this server.';

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
      'NeoCloud doesn\'t send emails yet. Ask an administrator of this server to set a new password for you (Admin › Users).';

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
  String get filesSubtitle =>
      'Everything you keep here is encrypted on your server.';

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
  String get settingsScopeAccount => 'ACCOUNT';

  @override
  String get settingsScopeApp => 'THIS DEVICE';

  @override
  String get settingsAccountHint => 'Saved to your account';

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
  String get mediaUnavailable => 'This file can’t be played in the browser.';

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
      'Enter the address of your NeoCloud server. You then approve this device in your browser.';

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
      'Vault videos and audio can’t be played in the browser. Download the file to play it.';

  @override
  String get shareLink => 'Share';

  @override
  String get newVault => 'New vault';

  @override
  String get vaultLock => 'Lock';

  @override
  String get vaultSubtitle =>
      'End-to-end encrypted. Names and contents are encrypted on this device; the server can’t read them.';

  @override
  String get vaultIntro =>
      'A vault is a folder whose names and contents are encrypted on your device with a passphrase only you know.';

  @override
  String get vaultPassphrase => 'Passphrase';

  @override
  String get vaultPassphraseRepeat => 'Repeat passphrase';

  @override
  String get vaultNoRecovery =>
      'The passphrase can’t be recovered or reset, not even by an admin. If you forget it, the files in this vault are lost.';

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
    return 'This device forgets its access to $server and camera upload turns off. To connect again you approve it once more in the browser.';
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
      'NeoCloud reads new photos and videos from your library to upload them to your server. Nothing else leaves the phone. You can choose “All photos” or a selection in the next step.';

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
      'Only you hold the key. If you lose both the passphrase and the recovery key, nobody — not even an admin — can open this vault again.';

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
      'The server’s disk is full. Ask the admin to free up space; nothing was saved.';

  @override
  String get securityServerStopping =>
      'The server is restarting. Try again in a moment.';

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
  String get cameraStatusUnreachable => 'Server unreachable';

  @override
  String get cameraStatusSignedOut => 'Signed out';

  @override
  String get cameraStatusServerFull => 'Server full';

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
  String get mobileNavFiles => 'Files';

  @override
  String get mobileNavCamera => 'Camera';

  @override
  String get mobileMore => 'More';

  @override
  String get mobileRecentServers => 'Recent servers';

  @override
  String get mobileForgetServer => 'Forget';

  @override
  String get filesQuickAccess => 'Quick access';

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
  String get qrPairing => 'QR Pairing';

  @override
  String get pairPhoneDescription =>
      'Show a QR code that signs the NeoCloud app on a phone in to this account, without typing the server address or a password.';

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
  String get scanPrimeTitle => 'Use the camera to pair';

  @override
  String get scanPrimeBody =>
      'NeoCloud needs the camera only to read the pairing code shown in the web app. Nothing is recorded or uploaded.';

  @override
  String get scanPrimeAllow => 'Allow camera';

  @override
  String get scanHint =>
      'Point the camera at the QR code in Settings › Sign-in & security';

  @override
  String get scanWrongCode => 'That\'s not a NeoCloud pairing code';

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
      'The server indexes the text of your documents so search finds the words in them. Turn this off to keep your files out of the index.';

  @override
  String get searchSettingsContentsOff =>
      'Full-text search is off on this server. If an admin turns it on, this decides whether your files are indexed.';

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
      'Only an admin can import from a server on your local network.';

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
  String get navSharedWithMe => 'Shared with me';

  @override
  String get sharedWithMeSubtitle =>
      'Files and folders other people on this server shared with you.';

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
      'Enter the exact email address or username of someone with an account on this server.';

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
}
