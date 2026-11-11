import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:intl/intl.dart' as intl;

import 'app_l10n_de.dart';
import 'app_l10n_en.dart';

// ignore_for_file: type=lint

/// Callers can lookup localized strings with an instance of AppL10n
/// returned by `AppL10n.of(context)`.
///
/// Applications need to include `AppL10n.delegate()` in their app's
/// `localizationDelegates` list, and the locales they support in the app's
/// `supportedLocales` list. For example:
///
/// ```dart
/// import 'gen/app_l10n.dart';
///
/// return MaterialApp(
///   localizationsDelegates: AppL10n.localizationsDelegates,
///   supportedLocales: AppL10n.supportedLocales,
///   home: MyApplicationHome(),
/// );
/// ```
///
/// ## Update pubspec.yaml
///
/// Please make sure to update your pubspec.yaml to include the following
/// packages:
///
/// ```yaml
/// dependencies:
///   # Internationalization support.
///   flutter_localizations:
///     sdk: flutter
///   intl: any # Use the pinned version from flutter_localizations
///
///   # Rest of dependencies
/// ```
///
/// ## iOS Applications
///
/// iOS applications define key application metadata, including supported
/// locales, in an Info.plist file that is built into the application bundle.
/// To configure the locales supported by your app, you’ll need to edit this
/// file.
///
/// First, open your project’s ios/Runner.xcworkspace Xcode workspace file.
/// Then, in the Project Navigator, open the Info.plist file under the Runner
/// project’s Runner folder.
///
/// Next, select the Information Property List item, select Add Item from the
/// Editor menu, then select Localizations from the pop-up menu.
///
/// Select and expand the newly-created Localizations item then, for each
/// locale your application supports, add a new item and select the locale
/// you wish to add from the pop-up menu in the Value field. This list should
/// be consistent with the languages listed in the AppL10n.supportedLocales
/// property.
abstract class AppL10n {
  AppL10n(String locale)
    : localeName = intl.Intl.canonicalizedLocale(locale.toString());

  final String localeName;

  static AppL10n of(BuildContext context) {
    return Localizations.of<AppL10n>(context, AppL10n)!;
  }

  static const LocalizationsDelegate<AppL10n> delegate = _AppL10nDelegate();

  /// A list of this localizations delegate along with the default localizations
  /// delegates.
  ///
  /// Returns a list of localizations delegates containing this delegate along with
  /// GlobalMaterialLocalizations.delegate, GlobalCupertinoLocalizations.delegate,
  /// and GlobalWidgetsLocalizations.delegate.
  ///
  /// Additional delegates can be added by appending to this list in
  /// MaterialApp. This list does not have to be used at all if a custom list
  /// of delegates is preferred or required.
  static const List<LocalizationsDelegate<dynamic>> localizationsDelegates =
      <LocalizationsDelegate<dynamic>>[
        delegate,
        GlobalMaterialLocalizations.delegate,
        GlobalCupertinoLocalizations.delegate,
        GlobalWidgetsLocalizations.delegate,
      ];

  /// A list of this localizations delegate's supported locales.
  static const List<Locale> supportedLocales = <Locale>[
    Locale('de'),
    Locale('en'),
  ];

  /// No description provided for @appName.
  ///
  /// In en, this message translates to:
  /// **'NeoCloud'**
  String get appName;

  /// No description provided for @appEyebrow.
  ///
  /// In en, this message translates to:
  /// **'PRIVATE CLOUD'**
  String get appEyebrow;

  /// No description provided for @cancel.
  ///
  /// In en, this message translates to:
  /// **'Cancel'**
  String get cancel;

  /// No description provided for @save.
  ///
  /// In en, this message translates to:
  /// **'Save'**
  String get save;

  /// No description provided for @saving.
  ///
  /// In en, this message translates to:
  /// **'Saving…'**
  String get saving;

  /// No description provided for @saved.
  ///
  /// In en, this message translates to:
  /// **'Saved'**
  String get saved;

  /// No description provided for @close.
  ///
  /// In en, this message translates to:
  /// **'Close'**
  String get close;

  /// No description provided for @dismiss.
  ///
  /// In en, this message translates to:
  /// **'Dismiss'**
  String get dismiss;

  /// No description provided for @retry.
  ///
  /// In en, this message translates to:
  /// **'Try again'**
  String get retry;

  /// No description provided for @refresh.
  ///
  /// In en, this message translates to:
  /// **'Refresh'**
  String get refresh;

  /// No description provided for @create.
  ///
  /// In en, this message translates to:
  /// **'Create'**
  String get create;

  /// No description provided for @delete.
  ///
  /// In en, this message translates to:
  /// **'Delete'**
  String get delete;

  /// No description provided for @clear.
  ///
  /// In en, this message translates to:
  /// **'Clear'**
  String get clear;

  /// No description provided for @copy.
  ///
  /// In en, this message translates to:
  /// **'Copy'**
  String get copy;

  /// No description provided for @copied.
  ///
  /// In en, this message translates to:
  /// **'Copied'**
  String get copied;

  /// No description provided for @continueLabel.
  ///
  /// In en, this message translates to:
  /// **'Continue'**
  String get continueLabel;

  /// No description provided for @genericError.
  ///
  /// In en, this message translates to:
  /// **'Something went wrong. Please try again.'**
  String get genericError;

  /// No description provided for @connectionError.
  ///
  /// In en, this message translates to:
  /// **'Can’t reach the NeoCloud server.'**
  String get connectionError;

  /// No description provided for @signIn.
  ///
  /// In en, this message translates to:
  /// **'Sign in'**
  String get signIn;

  /// No description provided for @authSignInTitle.
  ///
  /// In en, this message translates to:
  /// **'Welcome back'**
  String get authSignInTitle;

  /// No description provided for @authSignInSubtitle.
  ///
  /// In en, this message translates to:
  /// **'Sign in to continue.'**
  String get authSignInSubtitle;

  /// No description provided for @authCreateAccount.
  ///
  /// In en, this message translates to:
  /// **'Create account'**
  String get authCreateAccount;

  /// No description provided for @createTheFirstAccount.
  ///
  /// In en, this message translates to:
  /// **'Create the first account'**
  String get createTheFirstAccount;

  /// No description provided for @firstAccountSubtitle.
  ///
  /// In en, this message translates to:
  /// **'This account will administer NeoCloud on this server.'**
  String get firstAccountSubtitle;

  /// No description provided for @createAnotherAccount.
  ///
  /// In en, this message translates to:
  /// **'Create another NeoCloud account.'**
  String get createAnotherAccount;

  /// No description provided for @usernameOrEmail.
  ///
  /// In en, this message translates to:
  /// **'Username or email'**
  String get usernameOrEmail;

  /// No description provided for @username.
  ///
  /// In en, this message translates to:
  /// **'Username'**
  String get username;

  /// No description provided for @email.
  ///
  /// In en, this message translates to:
  /// **'Email'**
  String get email;

  /// No description provided for @password.
  ///
  /// In en, this message translates to:
  /// **'Password'**
  String get password;

  /// No description provided for @confirmPassword.
  ///
  /// In en, this message translates to:
  /// **'Confirm password'**
  String get confirmPassword;

  /// No description provided for @forgotPasswordAskAdmin.
  ///
  /// In en, this message translates to:
  /// **'NeoCloud doesn\'t send emails yet. Ask an administrator of this server to set a new password for you (Admin › Users).'**
  String get forgotPasswordAskAdmin;

  /// No description provided for @alreadyHaveAnAccount.
  ///
  /// In en, this message translates to:
  /// **'Already have an account? Sign in'**
  String get alreadyHaveAnAccount;

  /// No description provided for @needANewAccount.
  ///
  /// In en, this message translates to:
  /// **'Need a new account? Register'**
  String get needANewAccount;

  /// No description provided for @verification.
  ///
  /// In en, this message translates to:
  /// **'Verification'**
  String get verification;

  /// No description provided for @enter2faCode.
  ///
  /// In en, this message translates to:
  /// **'Enter your 2FA code'**
  String get enter2faCode;

  /// No description provided for @openYourAuthenticator.
  ///
  /// In en, this message translates to:
  /// **'Open your authenticator app and enter the 6-digit code, or use a recovery code.'**
  String get openYourAuthenticator;

  /// No description provided for @twoFactorOrRecoveryCode.
  ///
  /// In en, this message translates to:
  /// **'2FA or recovery code'**
  String get twoFactorOrRecoveryCode;

  /// No description provided for @verify.
  ///
  /// In en, this message translates to:
  /// **'Verify'**
  String get verify;

  /// No description provided for @backToSignIn.
  ///
  /// In en, this message translates to:
  /// **'Back to sign in'**
  String get backToSignIn;

  /// No description provided for @enterAUsername.
  ///
  /// In en, this message translates to:
  /// **'Enter a username.'**
  String get enterAUsername;

  /// No description provided for @usernameTooShort.
  ///
  /// In en, this message translates to:
  /// **'Use at least 3 characters.'**
  String get usernameTooShort;

  /// No description provided for @enterAValidEmail.
  ///
  /// In en, this message translates to:
  /// **'Enter a valid email address.'**
  String get enterAValidEmail;

  /// No description provided for @enterAPassword.
  ///
  /// In en, this message translates to:
  /// **'Enter a password.'**
  String get enterAPassword;

  /// No description provided for @passwordsDoNotMatch.
  ///
  /// In en, this message translates to:
  /// **'Passwords don’t match.'**
  String get passwordsDoNotMatch;

  /// No description provided for @enterYourUsernameOrEmail.
  ///
  /// In en, this message translates to:
  /// **'Enter your username or email.'**
  String get enterYourUsernameOrEmail;

  /// No description provided for @enterYour2faCode.
  ///
  /// In en, this message translates to:
  /// **'Enter your 2FA or recovery code.'**
  String get enterYour2faCode;

  /// No description provided for @showPassword.
  ///
  /// In en, this message translates to:
  /// **'Show password'**
  String get showPassword;

  /// No description provided for @hidePassword.
  ///
  /// In en, this message translates to:
  /// **'Hide password'**
  String get hidePassword;

  /// No description provided for @passwordStrength.
  ///
  /// In en, this message translates to:
  /// **'Password strength: {label}'**
  String passwordStrength(String label);

  /// No description provided for @strengthEmpty.
  ///
  /// In en, this message translates to:
  /// **'empty'**
  String get strengthEmpty;

  /// No description provided for @strengthRejected.
  ///
  /// In en, this message translates to:
  /// **'too weak, will be rejected'**
  String get strengthRejected;

  /// No description provided for @strengthGood.
  ///
  /// In en, this message translates to:
  /// **'good'**
  String get strengthGood;

  /// No description provided for @strengthStrong.
  ///
  /// In en, this message translates to:
  /// **'strong'**
  String get strengthStrong;

  /// No description provided for @passwordHintEmpty.
  ///
  /// In en, this message translates to:
  /// **'Use 8+ characters. Longer passphrases work well.'**
  String get passwordHintEmpty;

  /// No description provided for @passwordHintLength.
  ///
  /// In en, this message translates to:
  /// **'Use at least 8 characters.'**
  String get passwordHintLength;

  /// No description provided for @passwordHintUserInfo.
  ///
  /// In en, this message translates to:
  /// **'Do not include your username or email.'**
  String get passwordHintUserInfo;

  /// No description provided for @passwordHintPattern.
  ///
  /// In en, this message translates to:
  /// **'Avoid repeated characters and obvious sequences.'**
  String get passwordHintPattern;

  /// No description provided for @passwordHintMore.
  ///
  /// In en, this message translates to:
  /// **'Add more length or another character type.'**
  String get passwordHintMore;

  /// No description provided for @passwordHintStrong.
  ///
  /// In en, this message translates to:
  /// **'Strong password.'**
  String get passwordHintStrong;

  /// No description provided for @passwordHintGood.
  ///
  /// In en, this message translates to:
  /// **'Good password. A little more length makes it stronger.'**
  String get passwordHintGood;

  /// No description provided for @navFiles.
  ///
  /// In en, this message translates to:
  /// **'Files'**
  String get navFiles;

  /// No description provided for @navAllFiles.
  ///
  /// In en, this message translates to:
  /// **'All files'**
  String get navAllFiles;

  /// No description provided for @navRecent.
  ///
  /// In en, this message translates to:
  /// **'Recent'**
  String get navRecent;

  /// No description provided for @navTrash.
  ///
  /// In en, this message translates to:
  /// **'Trash'**
  String get navTrash;

  /// No description provided for @navAdmin.
  ///
  /// In en, this message translates to:
  /// **'Admin'**
  String get navAdmin;

  /// No description provided for @navSettings.
  ///
  /// In en, this message translates to:
  /// **'Settings'**
  String get navSettings;

  /// No description provided for @storageUsedOf.
  ///
  /// In en, this message translates to:
  /// **'{used} of {total}'**
  String storageUsedOf(String used, String total);

  /// No description provided for @storageUsedUnlimited.
  ///
  /// In en, this message translates to:
  /// **'{used} used'**
  String storageUsedUnlimited(String used);

  /// No description provided for @storageLabel.
  ///
  /// In en, this message translates to:
  /// **'Storage'**
  String get storageLabel;

  /// No description provided for @unlimited.
  ///
  /// In en, this message translates to:
  /// **'Unlimited'**
  String get unlimited;

  /// No description provided for @filesTitle.
  ///
  /// In en, this message translates to:
  /// **'Files'**
  String get filesTitle;

  /// No description provided for @filesSubtitle.
  ///
  /// In en, this message translates to:
  /// **'Everything you keep here is encrypted on your server.'**
  String get filesSubtitle;

  /// No description provided for @upload.
  ///
  /// In en, this message translates to:
  /// **'Upload'**
  String get upload;

  /// No description provided for @newFolder.
  ///
  /// In en, this message translates to:
  /// **'New folder'**
  String get newFolder;

  /// No description provided for @folderName.
  ///
  /// In en, this message translates to:
  /// **'Folder name'**
  String get folderName;

  /// No description provided for @rename.
  ///
  /// In en, this message translates to:
  /// **'Rename'**
  String get rename;

  /// No description provided for @newName.
  ///
  /// In en, this message translates to:
  /// **'New name'**
  String get newName;

  /// No description provided for @moveTo.
  ///
  /// In en, this message translates to:
  /// **'Move to…'**
  String get moveTo;

  /// No description provided for @moveHere.
  ///
  /// In en, this message translates to:
  /// **'Move here'**
  String get moveHere;

  /// No description provided for @makeCopy.
  ///
  /// In en, this message translates to:
  /// **'Make a copy'**
  String get makeCopy;

  /// No description provided for @download.
  ///
  /// In en, this message translates to:
  /// **'Download'**
  String get download;

  /// No description provided for @downloadZip.
  ///
  /// In en, this message translates to:
  /// **'Download as ZIP'**
  String get downloadZip;

  /// No description provided for @versions.
  ///
  /// In en, this message translates to:
  /// **'Versions'**
  String get versions;

  /// No description provided for @open.
  ///
  /// In en, this message translates to:
  /// **'Open'**
  String get open;

  /// No description provided for @moveToTrash.
  ///
  /// In en, this message translates to:
  /// **'Move to trash'**
  String get moveToTrash;

  /// No description provided for @home.
  ///
  /// In en, this message translates to:
  /// **'Home'**
  String get home;

  /// No description provided for @columnName.
  ///
  /// In en, this message translates to:
  /// **'Name'**
  String get columnName;

  /// No description provided for @columnSize.
  ///
  /// In en, this message translates to:
  /// **'Size'**
  String get columnSize;

  /// No description provided for @columnModified.
  ///
  /// In en, this message translates to:
  /// **'Modified'**
  String get columnModified;

  /// No description provided for @itemsCount.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =0{No items} =1{1 item} other{{count} items}}'**
  String itemsCount(int count);

  /// No description provided for @selectedCount.
  ///
  /// In en, this message translates to:
  /// **'{count} selected'**
  String selectedCount(int count);

  /// No description provided for @emptyFolderTitle.
  ///
  /// In en, this message translates to:
  /// **'This folder is empty'**
  String get emptyFolderTitle;

  /// No description provided for @emptyFolderSubtitle.
  ///
  /// In en, this message translates to:
  /// **'Drop files anywhere on this page, or use Upload.'**
  String get emptyFolderSubtitle;

  /// No description provided for @dropToUpload.
  ///
  /// In en, this message translates to:
  /// **'Drop to upload to “{folder}”'**
  String dropToUpload(String folder);

  /// No description provided for @searchFiles.
  ///
  /// In en, this message translates to:
  /// **'Search files and folders'**
  String get searchFiles;

  /// No description provided for @noSearchResults.
  ///
  /// In en, this message translates to:
  /// **'Nothing matches “{query}”.'**
  String noSearchResults(String query);

  /// No description provided for @trashConfirmTitle.
  ///
  /// In en, this message translates to:
  /// **'Move to trash?'**
  String get trashConfirmTitle;

  /// No description provided for @trashConfirmMessage.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{“{name}” moves to the trash. You can restore it from there.} other{{count} items move to the trash. You can restore them from there.}}'**
  String trashConfirmMessage(int count, String name);

  /// No description provided for @nameExistsTitle.
  ///
  /// In en, this message translates to:
  /// **'“{name}” already exists'**
  String nameExistsTitle(String name);

  /// No description provided for @nameExistsMessage.
  ///
  /// In en, this message translates to:
  /// **'A file with this name is already in this folder.'**
  String get nameExistsMessage;

  /// No description provided for @replace.
  ///
  /// In en, this message translates to:
  /// **'Replace'**
  String get replace;

  /// No description provided for @keepBoth.
  ///
  /// In en, this message translates to:
  /// **'Keep both'**
  String get keepBoth;

  /// No description provided for @skip.
  ///
  /// In en, this message translates to:
  /// **'Skip'**
  String get skip;

  /// No description provided for @applyToAll.
  ///
  /// In en, this message translates to:
  /// **'Do this for the remaining files'**
  String get applyToAll;

  /// No description provided for @uploadsTitle.
  ///
  /// In en, this message translates to:
  /// **'Uploads'**
  String get uploadsTitle;

  /// No description provided for @uploadsActive.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{Uploading 1 file} other{Uploading {count} files}}'**
  String uploadsActive(int count);

  /// No description provided for @uploadsDone.
  ///
  /// In en, this message translates to:
  /// **'Uploads finished'**
  String get uploadsDone;

  /// No description provided for @uploadWaiting.
  ///
  /// In en, this message translates to:
  /// **'Waiting'**
  String get uploadWaiting;

  /// No description provided for @uploadDone.
  ///
  /// In en, this message translates to:
  /// **'Done'**
  String get uploadDone;

  /// No description provided for @uploadSkipped.
  ///
  /// In en, this message translates to:
  /// **'Skipped'**
  String get uploadSkipped;

  /// No description provided for @uploadFailed.
  ///
  /// In en, this message translates to:
  /// **'Failed'**
  String get uploadFailed;

  /// No description provided for @uploadCancel.
  ///
  /// In en, this message translates to:
  /// **'Cancel upload'**
  String get uploadCancel;

  /// No description provided for @uploadClearFinished.
  ///
  /// In en, this message translates to:
  /// **'Clear finished'**
  String get uploadClearFinished;

  /// No description provided for @recentTitle.
  ///
  /// In en, this message translates to:
  /// **'Recent'**
  String get recentTitle;

  /// No description provided for @recentSubtitle.
  ///
  /// In en, this message translates to:
  /// **'Files you changed or added most recently.'**
  String get recentSubtitle;

  /// No description provided for @recentEmpty.
  ///
  /// In en, this message translates to:
  /// **'No files yet'**
  String get recentEmpty;

  /// No description provided for @recentEmptySubtitle.
  ///
  /// In en, this message translates to:
  /// **'Upload something and it shows up here.'**
  String get recentEmptySubtitle;

  /// No description provided for @trashTitle.
  ///
  /// In en, this message translates to:
  /// **'Trash'**
  String get trashTitle;

  /// No description provided for @trashSubtitle.
  ///
  /// In en, this message translates to:
  /// **'Deleted items wait here before they are removed for good.'**
  String get trashSubtitle;

  /// No description provided for @trashRetention.
  ///
  /// In en, this message translates to:
  /// **'Items are removed automatically after {days} days.'**
  String trashRetention(int days);

  /// No description provided for @restore.
  ///
  /// In en, this message translates to:
  /// **'Restore'**
  String get restore;

  /// No description provided for @deleteForever.
  ///
  /// In en, this message translates to:
  /// **'Delete forever'**
  String get deleteForever;

  /// No description provided for @emptyTrash.
  ///
  /// In en, this message translates to:
  /// **'Empty trash'**
  String get emptyTrash;

  /// No description provided for @emptyTrashConfirm.
  ///
  /// In en, this message translates to:
  /// **'Everything in the trash is deleted for good. This can’t be undone.'**
  String get emptyTrashConfirm;

  /// No description provided for @deleteForeverConfirm.
  ///
  /// In en, this message translates to:
  /// **'“{name}” is deleted for good. This can’t be undone.'**
  String deleteForeverConfirm(String name);

  /// No description provided for @trashIsEmpty.
  ///
  /// In en, this message translates to:
  /// **'The trash is empty'**
  String get trashIsEmpty;

  /// No description provided for @trashIsEmptySubtitle.
  ///
  /// In en, this message translates to:
  /// **'Deleted files and folders show up here.'**
  String get trashIsEmptySubtitle;

  /// No description provided for @deletedOn.
  ///
  /// In en, this message translates to:
  /// **'Deleted {date} · from {path}'**
  String deletedOn(String date, String path);

  /// No description provided for @versionsTitle.
  ///
  /// In en, this message translates to:
  /// **'Versions of “{name}”'**
  String versionsTitle(String name);

  /// No description provided for @versionCurrent.
  ///
  /// In en, this message translates to:
  /// **'Current version'**
  String get versionCurrent;

  /// No description provided for @versionNumber.
  ///
  /// In en, this message translates to:
  /// **'Version {number}'**
  String versionNumber(int number);

  /// No description provided for @noVersions.
  ///
  /// In en, this message translates to:
  /// **'No earlier versions yet. Replacing the file keeps the old one here.'**
  String get noVersions;

  /// No description provided for @noPreview.
  ///
  /// In en, this message translates to:
  /// **'No preview for this file type yet.'**
  String get noPreview;

  /// No description provided for @previewTooLarge.
  ///
  /// In en, this message translates to:
  /// **'This file is too large to preview here.'**
  String get previewTooLarge;

  /// No description provided for @details.
  ///
  /// In en, this message translates to:
  /// **'Details'**
  String get details;

  /// No description provided for @settingsTitle.
  ///
  /// In en, this message translates to:
  /// **'Settings'**
  String get settingsTitle;

  /// No description provided for @settingsScopeAccount.
  ///
  /// In en, this message translates to:
  /// **'ACCOUNT'**
  String get settingsScopeAccount;

  /// No description provided for @settingsScopeApp.
  ///
  /// In en, this message translates to:
  /// **'THIS DEVICE'**
  String get settingsScopeApp;

  /// No description provided for @settingsAccountHint.
  ///
  /// In en, this message translates to:
  /// **'Saved to your account'**
  String get settingsAccountHint;

  /// No description provided for @settingsSaveHint.
  ///
  /// In en, this message translates to:
  /// **'Changes save automatically'**
  String get settingsSaveHint;

  /// No description provided for @settingsSaveFailed.
  ///
  /// In en, this message translates to:
  /// **'Couldn’t save the last change'**
  String get settingsSaveFailed;

  /// No description provided for @pageProfile.
  ///
  /// In en, this message translates to:
  /// **'Profile'**
  String get pageProfile;

  /// No description provided for @pageProfileDescription.
  ///
  /// In en, this message translates to:
  /// **'Your name and sign-in details.'**
  String get pageProfileDescription;

  /// No description provided for @pageSecurity.
  ///
  /// In en, this message translates to:
  /// **'Sign-in & security'**
  String get pageSecurity;

  /// No description provided for @pageSecurityDescription.
  ///
  /// In en, this message translates to:
  /// **'Password, two-factor authentication and active sessions.'**
  String get pageSecurityDescription;

  /// No description provided for @pageDevices.
  ///
  /// In en, this message translates to:
  /// **'Devices'**
  String get pageDevices;

  /// No description provided for @pageDevicesDescription.
  ///
  /// In en, this message translates to:
  /// **'Desktop and mobile apps connected to your account.'**
  String get pageDevicesDescription;

  /// No description provided for @pageStorage.
  ///
  /// In en, this message translates to:
  /// **'Storage'**
  String get pageStorage;

  /// No description provided for @pageStorageDescription.
  ///
  /// In en, this message translates to:
  /// **'How much space you use and what takes it.'**
  String get pageStorageDescription;

  /// No description provided for @pageGeneral.
  ///
  /// In en, this message translates to:
  /// **'General'**
  String get pageGeneral;

  /// No description provided for @pageGeneralDescription.
  ///
  /// In en, this message translates to:
  /// **'Appearance, language and how files are shown.'**
  String get pageGeneralDescription;

  /// No description provided for @displayName.
  ///
  /// In en, this message translates to:
  /// **'Display name'**
  String get displayName;

  /// No description provided for @displayNameHint.
  ///
  /// In en, this message translates to:
  /// **'Shown instead of your username.'**
  String get displayNameHint;

  /// No description provided for @accountUsername.
  ///
  /// In en, this message translates to:
  /// **'Username'**
  String get accountUsername;

  /// No description provided for @accountEmail.
  ///
  /// In en, this message translates to:
  /// **'Email'**
  String get accountEmail;

  /// No description provided for @accountRole.
  ///
  /// In en, this message translates to:
  /// **'Role'**
  String get accountRole;

  /// No description provided for @roleAdmin.
  ///
  /// In en, this message translates to:
  /// **'Administrator'**
  String get roleAdmin;

  /// No description provided for @roleUser.
  ///
  /// In en, this message translates to:
  /// **'User'**
  String get roleUser;

  /// No description provided for @signOut.
  ///
  /// In en, this message translates to:
  /// **'Sign out'**
  String get signOut;

  /// No description provided for @signOutDescription.
  ///
  /// In en, this message translates to:
  /// **'End this session on this device.'**
  String get signOutDescription;

  /// No description provided for @changePassword.
  ///
  /// In en, this message translates to:
  /// **'Change password'**
  String get changePassword;

  /// No description provided for @currentPassword.
  ///
  /// In en, this message translates to:
  /// **'Current password'**
  String get currentPassword;

  /// No description provided for @newPassword.
  ///
  /// In en, this message translates to:
  /// **'New password'**
  String get newPassword;

  /// No description provided for @passwordChanged.
  ///
  /// In en, this message translates to:
  /// **'Password changed.'**
  String get passwordChanged;

  /// No description provided for @twoFactorTitle.
  ///
  /// In en, this message translates to:
  /// **'Two-factor authentication'**
  String get twoFactorTitle;

  /// No description provided for @twoFactorOn.
  ///
  /// In en, this message translates to:
  /// **'On · {count} recovery codes left'**
  String twoFactorOn(int count);

  /// No description provided for @twoFactorOff.
  ///
  /// In en, this message translates to:
  /// **'Off. Add a code from an authenticator app to every sign-in.'**
  String get twoFactorOff;

  /// No description provided for @twoFactorEnable.
  ///
  /// In en, this message translates to:
  /// **'Set up'**
  String get twoFactorEnable;

  /// No description provided for @twoFactorDisable.
  ///
  /// In en, this message translates to:
  /// **'Turn off'**
  String get twoFactorDisable;

  /// No description provided for @twoFactorSetupStep.
  ///
  /// In en, this message translates to:
  /// **'Add this key to your authenticator app, then enter the code it shows.'**
  String get twoFactorSetupStep;

  /// No description provided for @twoFactorCode.
  ///
  /// In en, this message translates to:
  /// **'6-digit code'**
  String get twoFactorCode;

  /// No description provided for @recoveryCodesTitle.
  ///
  /// In en, this message translates to:
  /// **'Save your recovery codes'**
  String get recoveryCodesTitle;

  /// No description provided for @recoveryCodesMessage.
  ///
  /// In en, this message translates to:
  /// **'Each code signs you in once if you lose your phone. They won’t be shown again.'**
  String get recoveryCodesMessage;

  /// No description provided for @sessionsTitle.
  ///
  /// In en, this message translates to:
  /// **'Active sessions'**
  String get sessionsTitle;

  /// No description provided for @sessionCurrent.
  ///
  /// In en, this message translates to:
  /// **'This session'**
  String get sessionCurrent;

  /// No description provided for @sessionLastSeen.
  ///
  /// In en, this message translates to:
  /// **'Last active {date}'**
  String sessionLastSeen(String date);

  /// No description provided for @sessionRevoke.
  ///
  /// In en, this message translates to:
  /// **'Sign out'**
  String get sessionRevoke;

  /// No description provided for @devicesEmpty.
  ///
  /// In en, this message translates to:
  /// **'No devices connected yet.'**
  String get devicesEmpty;

  /// No description provided for @deviceConnect.
  ///
  /// In en, this message translates to:
  /// **'Connect a device'**
  String get deviceConnect;

  /// No description provided for @deviceName.
  ///
  /// In en, this message translates to:
  /// **'Device name'**
  String get deviceName;

  /// No description provided for @deviceTokenTitle.
  ///
  /// In en, this message translates to:
  /// **'Device token'**
  String get deviceTokenTitle;

  /// No description provided for @deviceTokenMessage.
  ///
  /// In en, this message translates to:
  /// **'Paste this token into the NeoCloud app on that device. It is shown only once.'**
  String get deviceTokenMessage;

  /// No description provided for @deviceRevoke.
  ///
  /// In en, this message translates to:
  /// **'Disconnect'**
  String get deviceRevoke;

  /// No description provided for @deviceLastSeen.
  ///
  /// In en, this message translates to:
  /// **'Last seen {date}'**
  String deviceLastSeen(String date);

  /// No description provided for @deviceNeverSeen.
  ///
  /// In en, this message translates to:
  /// **'Not used yet'**
  String get deviceNeverSeen;

  /// No description provided for @storageUsed.
  ///
  /// In en, this message translates to:
  /// **'Used'**
  String get storageUsed;

  /// No description provided for @storageQuota.
  ///
  /// In en, this message translates to:
  /// **'Quota'**
  String get storageQuota;

  /// No description provided for @storageCurrentFiles.
  ///
  /// In en, this message translates to:
  /// **'Current files'**
  String get storageCurrentFiles;

  /// No description provided for @storageVersions.
  ///
  /// In en, this message translates to:
  /// **'Earlier versions'**
  String get storageVersions;

  /// No description provided for @storageTrash.
  ///
  /// In en, this message translates to:
  /// **'Trash'**
  String get storageTrash;

  /// No description provided for @theme.
  ///
  /// In en, this message translates to:
  /// **'Theme'**
  String get theme;

  /// No description provided for @themeSystem.
  ///
  /// In en, this message translates to:
  /// **'System'**
  String get themeSystem;

  /// No description provided for @themeLight.
  ///
  /// In en, this message translates to:
  /// **'Light'**
  String get themeLight;

  /// No description provided for @themeDark.
  ///
  /// In en, this message translates to:
  /// **'Dark'**
  String get themeDark;

  /// No description provided for @language.
  ///
  /// In en, this message translates to:
  /// **'Language'**
  String get language;

  /// No description provided for @fileView.
  ///
  /// In en, this message translates to:
  /// **'File view'**
  String get fileView;

  /// No description provided for @fileViewList.
  ///
  /// In en, this message translates to:
  /// **'List'**
  String get fileViewList;

  /// No description provided for @fileViewGrid.
  ///
  /// In en, this message translates to:
  /// **'Grid'**
  String get fileViewGrid;

  /// No description provided for @uploadConflict.
  ///
  /// In en, this message translates to:
  /// **'When a file already exists'**
  String get uploadConflict;

  /// No description provided for @uploadConflictAsk.
  ///
  /// In en, this message translates to:
  /// **'Ask'**
  String get uploadConflictAsk;

  /// No description provided for @adminTitle.
  ///
  /// In en, this message translates to:
  /// **'Admin'**
  String get adminTitle;

  /// No description provided for @adminSubtitle.
  ///
  /// In en, this message translates to:
  /// **'Accounts, storage limits and how this server runs.'**
  String get adminSubtitle;

  /// No description provided for @adminTabUsers.
  ///
  /// In en, this message translates to:
  /// **'Users'**
  String get adminTabUsers;

  /// No description provided for @adminTabStorage.
  ///
  /// In en, this message translates to:
  /// **'Storage'**
  String get adminTabStorage;

  /// No description provided for @adminTabAccess.
  ///
  /// In en, this message translates to:
  /// **'Access'**
  String get adminTabAccess;

  /// No description provided for @adminTabServer.
  ///
  /// In en, this message translates to:
  /// **'Server'**
  String get adminTabServer;

  /// No description provided for @adminUsersTitle.
  ///
  /// In en, this message translates to:
  /// **'Accounts'**
  String get adminUsersTitle;

  /// No description provided for @adminSearchUsers.
  ///
  /// In en, this message translates to:
  /// **'Search by name or email'**
  String get adminSearchUsers;

  /// No description provided for @adminQuotaDefault.
  ///
  /// In en, this message translates to:
  /// **'Default ({size})'**
  String adminQuotaDefault(String size);

  /// No description provided for @adminSetQuota.
  ///
  /// In en, this message translates to:
  /// **'Storage limit'**
  String get adminSetQuota;

  /// No description provided for @adminQuotaModeDefault.
  ///
  /// In en, this message translates to:
  /// **'Server default'**
  String get adminQuotaModeDefault;

  /// No description provided for @adminQuotaModeCustom.
  ///
  /// In en, this message translates to:
  /// **'Custom'**
  String get adminQuotaModeCustom;

  /// No description provided for @adminQuotaModeUnlimited.
  ///
  /// In en, this message translates to:
  /// **'Unlimited'**
  String get adminQuotaModeUnlimited;

  /// No description provided for @adminQuotaCustomHint.
  ///
  /// In en, this message translates to:
  /// **'e.g. 50GB or 500MB'**
  String get adminQuotaCustomHint;

  /// No description provided for @adminMakeAdmin.
  ///
  /// In en, this message translates to:
  /// **'Make admin'**
  String get adminMakeAdmin;

  /// No description provided for @adminRemoveAdmin.
  ///
  /// In en, this message translates to:
  /// **'Remove admin'**
  String get adminRemoveAdmin;

  /// No description provided for @adminResetPassword.
  ///
  /// In en, this message translates to:
  /// **'Reset password'**
  String get adminResetPassword;

  /// No description provided for @adminResetPasswordMessage.
  ///
  /// In en, this message translates to:
  /// **'Sets a new password for {name} and signs them out everywhere.'**
  String adminResetPasswordMessage(String name);

  /// No description provided for @adminDeleteUser.
  ///
  /// In en, this message translates to:
  /// **'Delete account'**
  String get adminDeleteUser;

  /// No description provided for @adminDeleteUserMessage.
  ///
  /// In en, this message translates to:
  /// **'{name} and all their files are deleted for good.'**
  String adminDeleteUserMessage(String name);

  /// No description provided for @adminFilesCount.
  ///
  /// In en, this message translates to:
  /// **'{count} files'**
  String adminFilesCount(int count);

  /// No description provided for @adminStorageTitle.
  ///
  /// In en, this message translates to:
  /// **'Storage policy'**
  String get adminStorageTitle;

  /// No description provided for @adminDefaultQuota.
  ///
  /// In en, this message translates to:
  /// **'Default quota per account'**
  String get adminDefaultQuota;

  /// No description provided for @adminDefaultQuotaDescription.
  ///
  /// In en, this message translates to:
  /// **'A size like 10GB, or “unlimited”.'**
  String get adminDefaultQuotaDescription;

  /// No description provided for @adminMaxUpload.
  ///
  /// In en, this message translates to:
  /// **'Largest single file'**
  String get adminMaxUpload;

  /// No description provided for @adminMaxUploadDescription.
  ///
  /// In en, this message translates to:
  /// **'0 means no limit beyond the quota.'**
  String get adminMaxUploadDescription;

  /// No description provided for @adminTrashDays.
  ///
  /// In en, this message translates to:
  /// **'Days in trash'**
  String get adminTrashDays;

  /// No description provided for @adminTrashDaysDescription.
  ///
  /// In en, this message translates to:
  /// **'0 keeps items until the trash is emptied.'**
  String get adminTrashDaysDescription;

  /// No description provided for @adminVersions.
  ///
  /// In en, this message translates to:
  /// **'Versions kept per file'**
  String get adminVersions;

  /// No description provided for @adminRecalculate.
  ///
  /// In en, this message translates to:
  /// **'Recalculate usage'**
  String get adminRecalculate;

  /// No description provided for @adminAccessTitle.
  ///
  /// In en, this message translates to:
  /// **'Registration'**
  String get adminAccessTitle;

  /// No description provided for @adminAllowSignup.
  ///
  /// In en, this message translates to:
  /// **'Open sign-up'**
  String get adminAllowSignup;

  /// No description provided for @adminAllowSignupDescription.
  ///
  /// In en, this message translates to:
  /// **'When off, nobody can register. The first account on a new server always can.'**
  String get adminAllowSignupDescription;

  /// No description provided for @adminServerTitle.
  ///
  /// In en, this message translates to:
  /// **'Server'**
  String get adminServerTitle;

  /// No description provided for @adminVersion.
  ///
  /// In en, this message translates to:
  /// **'Version'**
  String get adminVersion;

  /// No description provided for @adminUptime.
  ///
  /// In en, this message translates to:
  /// **'Uptime'**
  String get adminUptime;

  /// No description provided for @adminDatabase.
  ///
  /// In en, this message translates to:
  /// **'Database'**
  String get adminDatabase;

  /// No description provided for @adminAccounts.
  ///
  /// In en, this message translates to:
  /// **'Accounts'**
  String get adminAccounts;

  /// No description provided for @adminFiles.
  ///
  /// In en, this message translates to:
  /// **'Files'**
  String get adminFiles;

  /// No description provided for @adminStored.
  ///
  /// In en, this message translates to:
  /// **'Stored'**
  String get adminStored;

  /// No description provided for @adminDiskFree.
  ///
  /// In en, this message translates to:
  /// **'Free on storage disk'**
  String get adminDiskFree;

  /// No description provided for @adminLogs.
  ///
  /// In en, this message translates to:
  /// **'Recent logs'**
  String get adminLogs;

  /// No description provided for @adminMasterKey.
  ///
  /// In en, this message translates to:
  /// **'Master key'**
  String get adminMasterKey;

  /// No description provided for @adminMasterKeyDescription.
  ///
  /// In en, this message translates to:
  /// **'Every file is encrypted under this key. Keep a copy offline: without it, files and backups can’t be decrypted.'**
  String get adminMasterKeyDescription;

  /// No description provided for @adminShowMasterKey.
  ///
  /// In en, this message translates to:
  /// **'Show master key'**
  String get adminShowMasterKey;

  /// No description provided for @adminConfirmPassword.
  ///
  /// In en, this message translates to:
  /// **'Confirm with your password'**
  String get adminConfirmPassword;

  /// No description provided for @officePreviewUnavailable.
  ///
  /// In en, this message translates to:
  /// **'There’s no preview for this file. Download it to open it.'**
  String get officePreviewUnavailable;

  /// No description provided for @play.
  ///
  /// In en, this message translates to:
  /// **'Play'**
  String get play;

  /// No description provided for @pause.
  ///
  /// In en, this message translates to:
  /// **'Pause'**
  String get pause;

  /// No description provided for @mediaUnavailable.
  ///
  /// In en, this message translates to:
  /// **'This file can’t be played in the browser.'**
  String get mediaUnavailable;

  /// No description provided for @adminTabBackups.
  ///
  /// In en, this message translates to:
  /// **'Backups'**
  String get adminTabBackups;

  /// No description provided for @backupTargets.
  ///
  /// In en, this message translates to:
  /// **'Backup targets'**
  String get backupTargets;

  /// No description provided for @backupTargetsDescription.
  ///
  /// In en, this message translates to:
  /// **'Copies of every file and the database, encrypted, on another disk or server. Only new files are uploaded each time.'**
  String get backupTargetsDescription;

  /// No description provided for @backupAddTarget.
  ///
  /// In en, this message translates to:
  /// **'Add target'**
  String get backupAddTarget;

  /// No description provided for @backupEditTarget.
  ///
  /// In en, this message translates to:
  /// **'Edit target'**
  String get backupEditTarget;

  /// No description provided for @backupNoTargets.
  ///
  /// In en, this message translates to:
  /// **'No backup targets yet.'**
  String get backupNoTargets;

  /// No description provided for @backupName.
  ///
  /// In en, this message translates to:
  /// **'Name'**
  String get backupName;

  /// No description provided for @backupType.
  ///
  /// In en, this message translates to:
  /// **'Type'**
  String get backupType;

  /// No description provided for @backupTypeLocal.
  ///
  /// In en, this message translates to:
  /// **'Folder / NAS'**
  String get backupTypeLocal;

  /// No description provided for @backupTypeSftp.
  ///
  /// In en, this message translates to:
  /// **'SFTP'**
  String get backupTypeSftp;

  /// No description provided for @backupTypeS3.
  ///
  /// In en, this message translates to:
  /// **'S3-compatible'**
  String get backupTypeS3;

  /// No description provided for @backupTypeWebdav.
  ///
  /// In en, this message translates to:
  /// **'WebDAV'**
  String get backupTypeWebdav;

  /// No description provided for @backupPath.
  ///
  /// In en, this message translates to:
  /// **'Folder path'**
  String get backupPath;

  /// No description provided for @backupPathHint.
  ///
  /// In en, this message translates to:
  /// **'e.g. /mnt/nas/neocloud'**
  String get backupPathHint;

  /// No description provided for @backupHost.
  ///
  /// In en, this message translates to:
  /// **'Host'**
  String get backupHost;

  /// No description provided for @backupPort.
  ///
  /// In en, this message translates to:
  /// **'Port'**
  String get backupPort;

  /// No description provided for @backupUsername.
  ///
  /// In en, this message translates to:
  /// **'Username'**
  String get backupUsername;

  /// No description provided for @backupPassword.
  ///
  /// In en, this message translates to:
  /// **'Password'**
  String get backupPassword;

  /// No description provided for @backupPrivateKey.
  ///
  /// In en, this message translates to:
  /// **'Private key (optional)'**
  String get backupPrivateKey;

  /// No description provided for @backupSecretKept.
  ///
  /// In en, this message translates to:
  /// **'Leave empty to keep the saved one'**
  String get backupSecretKept;

  /// No description provided for @backupEndpoint.
  ///
  /// In en, this message translates to:
  /// **'Endpoint (empty for AWS)'**
  String get backupEndpoint;

  /// No description provided for @backupRegion.
  ///
  /// In en, this message translates to:
  /// **'Region'**
  String get backupRegion;

  /// No description provided for @backupBucket.
  ///
  /// In en, this message translates to:
  /// **'Bucket'**
  String get backupBucket;

  /// No description provided for @backupAccessKey.
  ///
  /// In en, this message translates to:
  /// **'Access key'**
  String get backupAccessKey;

  /// No description provided for @backupSecretKey.
  ///
  /// In en, this message translates to:
  /// **'Secret key'**
  String get backupSecretKey;

  /// No description provided for @backupPrefix.
  ///
  /// In en, this message translates to:
  /// **'Prefix (optional)'**
  String get backupPrefix;

  /// No description provided for @backupPathStyle.
  ///
  /// In en, this message translates to:
  /// **'Path-style URLs (MinIO)'**
  String get backupPathStyle;

  /// No description provided for @backupUrl.
  ///
  /// In en, this message translates to:
  /// **'WebDAV folder URL'**
  String get backupUrl;

  /// No description provided for @backupSchedule.
  ///
  /// In en, this message translates to:
  /// **'Schedule'**
  String get backupSchedule;

  /// No description provided for @backupScheduleManual.
  ///
  /// In en, this message translates to:
  /// **'Manual'**
  String get backupScheduleManual;

  /// No description provided for @backupScheduleDaily.
  ///
  /// In en, this message translates to:
  /// **'Daily'**
  String get backupScheduleDaily;

  /// No description provided for @backupScheduleWeekly.
  ///
  /// In en, this message translates to:
  /// **'Weekly'**
  String get backupScheduleWeekly;

  /// No description provided for @backupHour.
  ///
  /// In en, this message translates to:
  /// **'At hour (server time)'**
  String get backupHour;

  /// No description provided for @backupWeekday.
  ///
  /// In en, this message translates to:
  /// **'Weekday'**
  String get backupWeekday;

  /// No description provided for @backupRetention.
  ///
  /// In en, this message translates to:
  /// **'Snapshots to keep'**
  String get backupRetention;

  /// No description provided for @backupRunNow.
  ///
  /// In en, this message translates to:
  /// **'Back up now'**
  String get backupRunNow;

  /// No description provided for @backupTest.
  ///
  /// In en, this message translates to:
  /// **'Test connection'**
  String get backupTest;

  /// No description provided for @backupTestOk.
  ///
  /// In en, this message translates to:
  /// **'Connection works.'**
  String get backupTestOk;

  /// No description provided for @backupStarted.
  ///
  /// In en, this message translates to:
  /// **'Backup started.'**
  String get backupStarted;

  /// No description provided for @backupNeverRun.
  ///
  /// In en, this message translates to:
  /// **'Never backed up'**
  String get backupNeverRun;

  /// No description provided for @backupLastOk.
  ///
  /// In en, this message translates to:
  /// **'Last backup {date} · {count} new files'**
  String backupLastOk(String date, int count);

  /// No description provided for @backupLastFailed.
  ///
  /// In en, this message translates to:
  /// **'Last backup failed: {error}'**
  String backupLastFailed(String error);

  /// No description provided for @backupRunning.
  ///
  /// In en, this message translates to:
  /// **'Backing up…'**
  String get backupRunning;

  /// No description provided for @backupDeleteMessage.
  ///
  /// In en, this message translates to:
  /// **'The target is removed from NeoCloud. Backups already on it stay there.'**
  String get backupDeleteMessage;

  /// No description provided for @backupHistory.
  ///
  /// In en, this message translates to:
  /// **'Recent runs'**
  String get backupHistory;

  /// No description provided for @backupRestoreHint.
  ///
  /// In en, this message translates to:
  /// **'Restore with the CLI while NeoCloud is stopped: neocloud backup restore <id> <snapshot>. You need the master key.'**
  String get backupRestoreHint;

  /// No description provided for @adminPreviews.
  ///
  /// In en, this message translates to:
  /// **'Previews'**
  String get adminPreviews;

  /// No description provided for @adminPreviewsValue.
  ///
  /// In en, this message translates to:
  /// **'Images: yes · Office: {office}'**
  String adminPreviewsValue(String office);

  /// No description provided for @yes.
  ///
  /// In en, this message translates to:
  /// **'yes'**
  String get yes;

  /// No description provided for @noLibreOffice.
  ///
  /// In en, this message translates to:
  /// **'no (install LibreOffice)'**
  String get noLibreOffice;

  /// No description provided for @forgotPassword.
  ///
  /// In en, this message translates to:
  /// **'Forgot password?'**
  String get forgotPassword;

  /// No description provided for @forgotPasswordTitle.
  ///
  /// In en, this message translates to:
  /// **'Reset your password'**
  String get forgotPasswordTitle;

  /// No description provided for @newMenu.
  ///
  /// In en, this message translates to:
  /// **'New'**
  String get newMenu;

  /// No description provided for @uploadFiles.
  ///
  /// In en, this message translates to:
  /// **'Upload files'**
  String get uploadFiles;

  /// No description provided for @sortBy.
  ///
  /// In en, this message translates to:
  /// **'Sort'**
  String get sortBy;

  /// No description provided for @sortName.
  ///
  /// In en, this message translates to:
  /// **'Name'**
  String get sortName;

  /// No description provided for @sortSize.
  ///
  /// In en, this message translates to:
  /// **'Size'**
  String get sortSize;

  /// No description provided for @sortModified.
  ///
  /// In en, this message translates to:
  /// **'Last modified'**
  String get sortModified;

  /// No description provided for @sortType.
  ///
  /// In en, this message translates to:
  /// **'Type'**
  String get sortType;

  /// No description provided for @sortAscending.
  ///
  /// In en, this message translates to:
  /// **'Ascending'**
  String get sortAscending;

  /// No description provided for @sortDescending.
  ///
  /// In en, this message translates to:
  /// **'Descending'**
  String get sortDescending;

  /// No description provided for @movedTo.
  ///
  /// In en, this message translates to:
  /// **'Moved to “{folder}”.'**
  String movedTo(String folder);

  /// No description provided for @dragItems.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 item} other{{count} items}}'**
  String dragItems(int count);

  /// No description provided for @selectAll.
  ///
  /// In en, this message translates to:
  /// **'Select all'**
  String get selectAll;

  /// No description provided for @openFolder.
  ///
  /// In en, this message translates to:
  /// **'Open folder'**
  String get openFolder;

  /// No description provided for @showInFolder.
  ///
  /// In en, this message translates to:
  /// **'Show in folder'**
  String get showInFolder;

  /// No description provided for @copied2.
  ///
  /// In en, this message translates to:
  /// **'Copied.'**
  String get copied2;

  /// No description provided for @deviceApprovalTitle.
  ///
  /// In en, this message translates to:
  /// **'Connect a device'**
  String get deviceApprovalTitle;

  /// No description provided for @deviceApprovalSubtitle.
  ///
  /// In en, this message translates to:
  /// **'A device asks for access to your files.'**
  String get deviceApprovalSubtitle;

  /// No description provided for @deviceApprovalEnterCode.
  ///
  /// In en, this message translates to:
  /// **'Enter the code the device shows.'**
  String get deviceApprovalEnterCode;

  /// No description provided for @deviceCodeLabel.
  ///
  /// In en, this message translates to:
  /// **'Device code'**
  String get deviceCodeLabel;

  /// No description provided for @deviceCodeUnknown.
  ///
  /// In en, this message translates to:
  /// **'This code is unknown or has expired. Start the sign-in on the device again.'**
  String get deviceCodeUnknown;

  /// No description provided for @deviceApproveHint.
  ///
  /// In en, this message translates to:
  /// **'Only approve it if you started the sign-in yourself. It will reach the files of {account}.'**
  String deviceApproveHint(String account);

  /// No description provided for @deviceApprove.
  ///
  /// In en, this message translates to:
  /// **'Approve'**
  String get deviceApprove;

  /// No description provided for @deviceDeny.
  ///
  /// In en, this message translates to:
  /// **'Deny'**
  String get deviceDeny;

  /// No description provided for @deviceApproved.
  ///
  /// In en, this message translates to:
  /// **'{name} is connected. You can return to the device.'**
  String deviceApproved(String name);

  /// No description provided for @deviceDenied.
  ///
  /// In en, this message translates to:
  /// **'{name} was denied access.'**
  String deviceDenied(String name);

  /// No description provided for @openNeoCloud.
  ///
  /// In en, this message translates to:
  /// **'Open NeoCloud'**
  String get openNeoCloud;

  /// No description provided for @connectTitle.
  ///
  /// In en, this message translates to:
  /// **'Connect to NeoCloud'**
  String get connectTitle;

  /// No description provided for @connectSubtitle.
  ///
  /// In en, this message translates to:
  /// **'Enter the address of your NeoCloud server. You then approve this device in your browser.'**
  String get connectSubtitle;

  /// No description provided for @connectServerLabel.
  ///
  /// In en, this message translates to:
  /// **'Server address'**
  String get connectServerLabel;

  /// No description provided for @connectButton.
  ///
  /// In en, this message translates to:
  /// **'Connect'**
  String get connectButton;

  /// No description provided for @connectInvalidServer.
  ///
  /// In en, this message translates to:
  /// **'Enter a server address such as https://cloud.example.com.'**
  String get connectInvalidServer;

  /// No description provided for @connectApproveTitle.
  ///
  /// In en, this message translates to:
  /// **'Approve in your browser'**
  String get connectApproveTitle;

  /// No description provided for @connectApproveSubtitle.
  ///
  /// In en, this message translates to:
  /// **'Sign in to NeoCloud in the browser window that opened and check that it shows this code.'**
  String get connectApproveSubtitle;

  /// No description provided for @connectWaiting.
  ///
  /// In en, this message translates to:
  /// **'Waiting for approval…'**
  String get connectWaiting;

  /// No description provided for @connectOpenBrowser.
  ///
  /// In en, this message translates to:
  /// **'Open browser again'**
  String get connectOpenBrowser;

  /// No description provided for @connectDenied.
  ///
  /// In en, this message translates to:
  /// **'The sign-in was denied in the browser.'**
  String get connectDenied;

  /// No description provided for @connectExpired.
  ///
  /// In en, this message translates to:
  /// **'The code expired before it was approved. Connect again.'**
  String get connectExpired;

  /// No description provided for @deviceRevokedNotice.
  ///
  /// In en, this message translates to:
  /// **'This device was signed out. Connect it again to continue.'**
  String get deviceRevokedNotice;

  /// No description provided for @vaultBoundaryError.
  ///
  /// In en, this message translates to:
  /// **'Items can’t be moved into or out of a vault. Download and upload them instead.'**
  String get vaultBoundaryError;

  /// No description provided for @shareTargetTitle.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{Save 1 file to NeoCloud} other{Save {count} files to NeoCloud}}'**
  String shareTargetTitle(int count);

  /// No description provided for @uploadHere.
  ///
  /// In en, this message translates to:
  /// **'Upload here'**
  String get uploadHere;

  /// No description provided for @shareRejected.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 shared item couldn’t be received.} other{{count} shared items couldn’t be received.}}'**
  String shareRejected(int count);

  /// No description provided for @shareTooLarge.
  ///
  /// In en, this message translates to:
  /// **'This share is larger than 2 GB and wasn’t received. Upload big files from the Files page.'**
  String get shareTooLarge;

  /// No description provided for @edit.
  ///
  /// In en, this message translates to:
  /// **'Edit'**
  String get edit;

  /// No description provided for @editorDone.
  ///
  /// In en, this message translates to:
  /// **'Done'**
  String get editorDone;

  /// No description provided for @editorSaved.
  ///
  /// In en, this message translates to:
  /// **'Saved as a new version'**
  String get editorSaved;

  /// No description provided for @editorUnsaved.
  ///
  /// In en, this message translates to:
  /// **'Unsaved changes'**
  String get editorUnsaved;

  /// No description provided for @editorDiscardTitle.
  ///
  /// In en, this message translates to:
  /// **'Discard your changes?'**
  String get editorDiscardTitle;

  /// No description provided for @editorDiscardMessage.
  ///
  /// In en, this message translates to:
  /// **'What you changed since the last save will be lost.'**
  String get editorDiscardMessage;

  /// No description provided for @editorKeepEditing.
  ///
  /// In en, this message translates to:
  /// **'Keep editing'**
  String get editorKeepEditing;

  /// No description provided for @editorDiscard.
  ///
  /// In en, this message translates to:
  /// **'Discard'**
  String get editorDiscard;

  /// No description provided for @editorConflictTitle.
  ///
  /// In en, this message translates to:
  /// **'Changed elsewhere'**
  String get editorConflictTitle;

  /// No description provided for @editorConflictMessage.
  ///
  /// In en, this message translates to:
  /// **'{name} was changed since you opened it. Save anyway? The other version stays in the version history.'**
  String editorConflictMessage(String name);

  /// No description provided for @editorOverwrite.
  ///
  /// In en, this message translates to:
  /// **'Save anyway'**
  String get editorOverwrite;

  /// No description provided for @vaultNoMediaPreview.
  ///
  /// In en, this message translates to:
  /// **'Vault videos and audio can’t be played in the browser. Download the file to play it.'**
  String get vaultNoMediaPreview;

  /// No description provided for @shareLink.
  ///
  /// In en, this message translates to:
  /// **'Share'**
  String get shareLink;

  /// No description provided for @newVault.
  ///
  /// In en, this message translates to:
  /// **'New vault'**
  String get newVault;

  /// No description provided for @vaultLock.
  ///
  /// In en, this message translates to:
  /// **'Lock'**
  String get vaultLock;

  /// No description provided for @vaultSubtitle.
  ///
  /// In en, this message translates to:
  /// **'End-to-end encrypted. Names and contents are encrypted on this device; the server can’t read them.'**
  String get vaultSubtitle;

  /// No description provided for @vaultIntro.
  ///
  /// In en, this message translates to:
  /// **'A vault is a folder whose names and contents are encrypted on your device with a passphrase only you know.'**
  String get vaultIntro;

  /// No description provided for @vaultPassphrase.
  ///
  /// In en, this message translates to:
  /// **'Passphrase'**
  String get vaultPassphrase;

  /// No description provided for @vaultPassphraseRepeat.
  ///
  /// In en, this message translates to:
  /// **'Repeat passphrase'**
  String get vaultPassphraseRepeat;

  /// No description provided for @vaultNoRecovery.
  ///
  /// In en, this message translates to:
  /// **'The passphrase can’t be recovered or reset, not even by an admin. If you forget it, the files in this vault are lost.'**
  String get vaultNoRecovery;

  /// No description provided for @vaultCreate.
  ///
  /// In en, this message translates to:
  /// **'Create vault'**
  String get vaultCreate;

  /// No description provided for @vaultNameRequired.
  ///
  /// In en, this message translates to:
  /// **'Give the vault a name.'**
  String get vaultNameRequired;

  /// No description provided for @vaultPassphraseTooShort.
  ///
  /// In en, this message translates to:
  /// **'Use at least {count} characters.'**
  String vaultPassphraseTooShort(int count);

  /// No description provided for @vaultWrongPassphrase.
  ///
  /// In en, this message translates to:
  /// **'That passphrase doesn’t open this vault.'**
  String get vaultWrongPassphrase;

  /// No description provided for @vaultLockedTitle.
  ///
  /// In en, this message translates to:
  /// **'{name} is locked'**
  String vaultLockedTitle(String name);

  /// No description provided for @vaultLockedSubtitle.
  ///
  /// In en, this message translates to:
  /// **'Enter the passphrase to see and add files. It stays unlocked until you lock it or close NeoCloud.'**
  String get vaultLockedSubtitle;

  /// No description provided for @vaultUnlock.
  ///
  /// In en, this message translates to:
  /// **'Unlock'**
  String get vaultUnlock;

  /// No description provided for @shareLinkTitle.
  ///
  /// In en, this message translates to:
  /// **'Share “{name}”'**
  String shareLinkTitle(String name);

  /// No description provided for @shareExpiry.
  ///
  /// In en, this message translates to:
  /// **'Link expires'**
  String get shareExpiry;

  /// No description provided for @shareExpiryNever.
  ///
  /// In en, this message translates to:
  /// **'Never'**
  String get shareExpiryNever;

  /// No description provided for @shareExpiryDay.
  ///
  /// In en, this message translates to:
  /// **'1 day'**
  String get shareExpiryDay;

  /// No description provided for @shareExpiryWeek.
  ///
  /// In en, this message translates to:
  /// **'7 days'**
  String get shareExpiryWeek;

  /// No description provided for @shareExpiryMonth.
  ///
  /// In en, this message translates to:
  /// **'30 days'**
  String get shareExpiryMonth;

  /// No description provided for @sharePasswordToggle.
  ///
  /// In en, this message translates to:
  /// **'Protect with a password'**
  String get sharePasswordToggle;

  /// No description provided for @sharePasswordHint.
  ///
  /// In en, this message translates to:
  /// **'Visitors need it to open the link.'**
  String get sharePasswordHint;

  /// No description provided for @sharePassword.
  ///
  /// In en, this message translates to:
  /// **'Link password'**
  String get sharePassword;

  /// No description provided for @sharePasswordRequired.
  ///
  /// In en, this message translates to:
  /// **'Enter a password or turn the protection off.'**
  String get sharePasswordRequired;

  /// No description provided for @shareAllowUpload.
  ///
  /// In en, this message translates to:
  /// **'Allow uploads'**
  String get shareAllowUpload;

  /// No description provided for @shareAllowUploadHint.
  ///
  /// In en, this message translates to:
  /// **'Anyone with the link can add files to this folder.'**
  String get shareAllowUploadHint;

  /// No description provided for @shareCreate.
  ///
  /// In en, this message translates to:
  /// **'Create link'**
  String get shareCreate;

  /// No description provided for @shareExpired.
  ///
  /// In en, this message translates to:
  /// **'Expired'**
  String get shareExpired;

  /// No description provided for @shareNeverExpires.
  ///
  /// In en, this message translates to:
  /// **'Never expires'**
  String get shareNeverExpires;

  /// No description provided for @shareExpiresOn.
  ///
  /// In en, this message translates to:
  /// **'Expires {date}'**
  String shareExpiresOn(String date);

  /// No description provided for @sharePasswordTag.
  ///
  /// In en, this message translates to:
  /// **'Password'**
  String get sharePasswordTag;

  /// No description provided for @shareUploadTag.
  ///
  /// In en, this message translates to:
  /// **'Uploads allowed'**
  String get shareUploadTag;

  /// No description provided for @shareFolderTag.
  ///
  /// In en, this message translates to:
  /// **'Folder'**
  String get shareFolderTag;

  /// No description provided for @shareRevoke.
  ///
  /// In en, this message translates to:
  /// **'Revoke'**
  String get shareRevoke;

  /// No description provided for @shareRevokeConfirm.
  ///
  /// In en, this message translates to:
  /// **'The link to {name} stops working right away for everyone who has it.'**
  String shareRevokeConfirm(String name);

  /// No description provided for @shareRevoked.
  ///
  /// In en, this message translates to:
  /// **'Link revoked.'**
  String get shareRevoked;

  /// No description provided for @shareCopied.
  ///
  /// In en, this message translates to:
  /// **'Link copied.'**
  String get shareCopied;

  /// No description provided for @pageSharedLinks.
  ///
  /// In en, this message translates to:
  /// **'Shared links'**
  String get pageSharedLinks;

  /// No description provided for @pageSharedLinksDescription.
  ///
  /// In en, this message translates to:
  /// **'Public links to your files and folders.'**
  String get pageSharedLinksDescription;

  /// No description provided for @sharedLinksHint.
  ///
  /// In en, this message translates to:
  /// **'Create a link from a file’s actions. Revoking stops it at once.'**
  String get sharedLinksHint;

  /// No description provided for @sharedLinksEmpty.
  ///
  /// In en, this message translates to:
  /// **'No shared links yet.'**
  String get sharedLinksEmpty;

  /// No description provided for @pageCameraUpload.
  ///
  /// In en, this message translates to:
  /// **'Camera upload'**
  String get pageCameraUpload;

  /// No description provided for @pageCameraUploadDescription.
  ///
  /// In en, this message translates to:
  /// **'Upload new photos and videos from this phone automatically.'**
  String get pageCameraUploadDescription;

  /// No description provided for @disconnect.
  ///
  /// In en, this message translates to:
  /// **'Disconnect'**
  String get disconnect;

  /// No description provided for @disconnectTitle.
  ///
  /// In en, this message translates to:
  /// **'Disconnect this device?'**
  String get disconnectTitle;

  /// No description provided for @disconnectMessage.
  ///
  /// In en, this message translates to:
  /// **'This device forgets its access to {server} and camera upload turns off. To connect again you approve it once more in the browser.'**
  String disconnectMessage(String server);

  /// No description provided for @syncChooseThisFolder.
  ///
  /// In en, this message translates to:
  /// **'Choose this folder'**
  String get syncChooseThisFolder;

  /// No description provided for @syncChoose.
  ///
  /// In en, this message translates to:
  /// **'Choose'**
  String get syncChoose;

  /// No description provided for @syncNothingChosen.
  ///
  /// In en, this message translates to:
  /// **'Not chosen yet'**
  String get syncNothingChosen;

  /// No description provided for @cameraAccessTitle.
  ///
  /// In en, this message translates to:
  /// **'Allow access to your photos'**
  String get cameraAccessTitle;

  /// No description provided for @cameraAccessMessage.
  ///
  /// In en, this message translates to:
  /// **'NeoCloud reads new photos and videos from your library to upload them to your server. Nothing else leaves the phone. You can choose “All photos” or a selection in the next step.'**
  String get cameraAccessMessage;

  /// No description provided for @cameraAccessDenied.
  ///
  /// In en, this message translates to:
  /// **'NeoCloud has no access to your photos. Allow it in the system settings to use camera upload.'**
  String get cameraAccessDenied;

  /// No description provided for @cameraUploadOn.
  ///
  /// In en, this message translates to:
  /// **'Camera upload is on. New photos are uploaded from now on.'**
  String get cameraUploadOn;

  /// No description provided for @cameraReconfirmTitle.
  ///
  /// In en, this message translates to:
  /// **'Turn camera upload back on?'**
  String get cameraReconfirmTitle;

  /// No description provided for @cameraReconfirmMessage.
  ///
  /// In en, this message translates to:
  /// **'Camera upload was turned off because this device signed out or connected to another server or account. Photos go to {server} only after you set it up again.'**
  String cameraReconfirmMessage(String server);

  /// No description provided for @cameraReconfirmLater.
  ///
  /// In en, this message translates to:
  /// **'Not now'**
  String get cameraReconfirmLater;

  /// No description provided for @cameraReconfirmSetUp.
  ///
  /// In en, this message translates to:
  /// **'Set up'**
  String get cameraReconfirmSetUp;

  /// No description provided for @cameraUploadFolder.
  ///
  /// In en, this message translates to:
  /// **'Upload to'**
  String get cameraUploadFolder;

  /// No description provided for @cameraUploadEnable.
  ///
  /// In en, this message translates to:
  /// **'Upload new photos'**
  String get cameraUploadEnable;

  /// No description provided for @cameraUploadEnableDescription.
  ///
  /// In en, this message translates to:
  /// **'Photos taken after you turn this on; older ones stay on the phone only.'**
  String get cameraUploadEnableDescription;

  /// No description provided for @cameraUploadWifiOnly.
  ///
  /// In en, this message translates to:
  /// **'Only on Wi-Fi'**
  String get cameraUploadWifiOnly;

  /// No description provided for @cameraUploadVideos.
  ///
  /// In en, this message translates to:
  /// **'Include videos'**
  String get cameraUploadVideos;

  /// No description provided for @cameraUploadStatus.
  ///
  /// In en, this message translates to:
  /// **'Status'**
  String get cameraUploadStatus;

  /// No description provided for @cameraUploadBackgroundHint.
  ///
  /// In en, this message translates to:
  /// **'In the background the system decides when NeoCloud may run: on Android about hourly at most, on iOS a few times a day. Opening the app uploads right away.'**
  String get cameraUploadBackgroundHint;

  /// No description provided for @cameraUploadOffState.
  ///
  /// In en, this message translates to:
  /// **'Off'**
  String get cameraUploadOffState;

  /// No description provided for @cameraUploadFailedState.
  ///
  /// In en, this message translates to:
  /// **'Last run failed'**
  String get cameraUploadFailedState;

  /// No description provided for @cameraUploadFailedHint.
  ///
  /// In en, this message translates to:
  /// **'Check the network and that NeoCloud may still read your photos.'**
  String get cameraUploadFailedHint;

  /// No description provided for @cameraUploadCount.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =0{Nothing uploaded yet} =1{1 file uploaded} other{{count} files uploaded}}'**
  String cameraUploadCount(int count);

  /// No description provided for @cameraUploadSince.
  ///
  /// In en, this message translates to:
  /// **'since {date}'**
  String cameraUploadSince(String date);

  /// No description provided for @cameraUploadLastRun.
  ///
  /// In en, this message translates to:
  /// **'last run {date}'**
  String cameraUploadLastRun(String date);

  /// No description provided for @cameraUploadRunNow.
  ///
  /// In en, this message translates to:
  /// **'Upload now'**
  String get cameraUploadRunNow;

  /// No description provided for @cameraAccessSettings.
  ///
  /// In en, this message translates to:
  /// **'Photo access'**
  String get cameraAccessSettings;

  /// No description provided for @cameraOpenSettings.
  ///
  /// In en, this message translates to:
  /// **'Open settings'**
  String get cameraOpenSettings;

  /// No description provided for @shareSummaryView.
  ///
  /// In en, this message translates to:
  /// **'Anyone with this link can view and download'**
  String get shareSummaryView;

  /// No description provided for @shareSummaryViewUpload.
  ///
  /// In en, this message translates to:
  /// **'Anyone with this link can view, download and add files'**
  String get shareSummaryViewUpload;

  /// No description provided for @shareSummaryUploadOnly.
  ///
  /// In en, this message translates to:
  /// **'Anyone with this link can add files but can’t see what’s in the folder'**
  String get shareSummaryUploadOnly;

  /// No description provided for @shareSummaryPassword.
  ///
  /// In en, this message translates to:
  /// **'password required'**
  String get shareSummaryPassword;

  /// No description provided for @shareSummaryExpires.
  ///
  /// In en, this message translates to:
  /// **'expires {date}'**
  String shareSummaryExpires(String date);

  /// No description provided for @shareSummaryNoExpiry.
  ///
  /// In en, this message translates to:
  /// **'no expiry'**
  String get shareSummaryNoExpiry;

  /// No description provided for @shareSummaryExpired.
  ///
  /// In en, this message translates to:
  /// **'expired'**
  String get shareSummaryExpired;

  /// No description provided for @shareSummaryOneTime.
  ///
  /// In en, this message translates to:
  /// **'one-time link'**
  String get shareSummaryOneTime;

  /// No description provided for @shareSummaryDownloadsLeft.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 download left} other{{count} downloads left}}'**
  String shareSummaryDownloadsLeft(int count);

  /// No description provided for @shareSummaryUsedUp.
  ///
  /// In en, this message translates to:
  /// **'download limit reached'**
  String get shareSummaryUsedUp;

  /// No description provided for @shareDownloadedTimes.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =0{Not downloaded yet} other{Downloaded {count}×}}'**
  String shareDownloadedTimes(int count);

  /// No description provided for @shareExpiresInDays.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{expires tomorrow} other{expires in {count} days}}'**
  String shareExpiresInDays(int count);

  /// No description provided for @shareExpiresInHours.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =0{expires within the hour} =1{expires in 1 hour} other{expires in {count} hours}}'**
  String shareExpiresInHours(int count);

  /// No description provided for @shareExpiredOn.
  ///
  /// In en, this message translates to:
  /// **'expired {date}'**
  String shareExpiredOn(String date);

  /// No description provided for @shareModeTitle.
  ///
  /// In en, this message translates to:
  /// **'Visitors can'**
  String get shareModeTitle;

  /// No description provided for @shareModeView.
  ///
  /// In en, this message translates to:
  /// **'View'**
  String get shareModeView;

  /// No description provided for @shareModeViewUpload.
  ///
  /// In en, this message translates to:
  /// **'View + upload'**
  String get shareModeViewUpload;

  /// No description provided for @shareModeUploadOnly.
  ///
  /// In en, this message translates to:
  /// **'File request'**
  String get shareModeUploadOnly;

  /// No description provided for @shareModeViewHint.
  ///
  /// In en, this message translates to:
  /// **'See, preview and download what you share.'**
  String get shareModeViewHint;

  /// No description provided for @shareModeViewUploadHint.
  ///
  /// In en, this message translates to:
  /// **'Also add files to this folder. Nothing is ever replaced.'**
  String get shareModeViewUploadHint;

  /// No description provided for @shareModeUploadOnlyHint.
  ///
  /// In en, this message translates to:
  /// **'Only add files. Visitors don’t see the folder’s contents, not even what others uploaded.'**
  String get shareModeUploadOnlyHint;

  /// No description provided for @shareExpiryCustom.
  ///
  /// In en, this message translates to:
  /// **'Custom…'**
  String get shareExpiryCustom;

  /// No description provided for @shareExpiryPick.
  ///
  /// In en, this message translates to:
  /// **'Expires on'**
  String get shareExpiryPick;

  /// No description provided for @sharePasswordGenerate.
  ///
  /// In en, this message translates to:
  /// **'Generate'**
  String get sharePasswordGenerate;

  /// No description provided for @sharePasswordCopy.
  ///
  /// In en, this message translates to:
  /// **'Copy password'**
  String get sharePasswordCopy;

  /// No description provided for @sharePasswordCopied.
  ///
  /// In en, this message translates to:
  /// **'Password copied.'**
  String get sharePasswordCopied;

  /// No description provided for @sharePasswordSendSeparately.
  ///
  /// In en, this message translates to:
  /// **'Send the password another way than the link, e.g. by phone or another messenger.'**
  String get sharePasswordSendSeparately;

  /// No description provided for @sharePasswordTooShort.
  ///
  /// In en, this message translates to:
  /// **'Use at least {count} characters.'**
  String sharePasswordTooShort(int count);

  /// No description provided for @sharePasswordKeep.
  ///
  /// In en, this message translates to:
  /// **'Leave empty to keep the current password.'**
  String get sharePasswordKeep;

  /// No description provided for @shareLimitTitle.
  ///
  /// In en, this message translates to:
  /// **'Download limit'**
  String get shareLimitTitle;

  /// No description provided for @shareLimitNone.
  ///
  /// In en, this message translates to:
  /// **'No limit'**
  String get shareLimitNone;

  /// No description provided for @shareLimitOnce.
  ///
  /// In en, this message translates to:
  /// **'One-time'**
  String get shareLimitOnce;

  /// No description provided for @shareLimitCustom.
  ///
  /// In en, this message translates to:
  /// **'Custom'**
  String get shareLimitCustom;

  /// No description provided for @shareLimitCount.
  ///
  /// In en, this message translates to:
  /// **'Downloads allowed'**
  String get shareLimitCount;

  /// No description provided for @shareLimitHint.
  ///
  /// In en, this message translates to:
  /// **'Every file or ZIP download counts. The link stops working after the last one.'**
  String get shareLimitHint;

  /// No description provided for @shareLimitInvalid.
  ///
  /// In en, this message translates to:
  /// **'Enter a number of downloads from 1 to 1,000,000.'**
  String get shareLimitInvalid;

  /// No description provided for @shareNote.
  ///
  /// In en, this message translates to:
  /// **'Note for visitors (optional)'**
  String get shareNote;

  /// No description provided for @shareNoteHint.
  ///
  /// In en, this message translates to:
  /// **'Shown on the share page.'**
  String get shareNoteHint;

  /// No description provided for @shareSave.
  ///
  /// In en, this message translates to:
  /// **'Save changes'**
  String get shareSave;

  /// No description provided for @shareSaved.
  ///
  /// In en, this message translates to:
  /// **'Link updated.'**
  String get shareSaved;

  /// No description provided for @shareEdit.
  ///
  /// In en, this message translates to:
  /// **'Edit link'**
  String get shareEdit;

  /// No description provided for @shareNewLink.
  ///
  /// In en, this message translates to:
  /// **'New link'**
  String get shareNewLink;

  /// No description provided for @shareMoreLinks.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 more active link for this item in Settings › Shared links.} other{{count} more active links for this item in Settings › Shared links.}}'**
  String shareMoreLinks(int count);

  /// No description provided for @shareGroupActive.
  ///
  /// In en, this message translates to:
  /// **'Active'**
  String get shareGroupActive;

  /// No description provided for @shareGroupInactive.
  ///
  /// In en, this message translates to:
  /// **'Expired or used up'**
  String get shareGroupInactive;

  /// No description provided for @shareNoteTag.
  ///
  /// In en, this message translates to:
  /// **'Note'**
  String get shareNoteTag;

  /// No description provided for @shareModeFileRequestTag.
  ///
  /// In en, this message translates to:
  /// **'File request'**
  String get shareModeFileRequestTag;

  /// No description provided for @shareExpiryInPast.
  ///
  /// In en, this message translates to:
  /// **'Pick an expiry date in the future.'**
  String get shareExpiryInPast;

  /// No description provided for @vaultShowPassphrase.
  ///
  /// In en, this message translates to:
  /// **'Show or hide'**
  String get vaultShowPassphrase;

  /// No description provided for @vaultGeneratePassphrase.
  ///
  /// In en, this message translates to:
  /// **'Generate passphrase'**
  String get vaultGeneratePassphrase;

  /// No description provided for @vaultGeneratedHint.
  ///
  /// In en, this message translates to:
  /// **'Six random words. Write them down — the vault can only be opened with them or the recovery key.'**
  String get vaultGeneratedHint;

  /// No description provided for @vaultCopyPassphrase.
  ///
  /// In en, this message translates to:
  /// **'Copy passphrase'**
  String get vaultCopyPassphrase;

  /// No description provided for @vaultStrengthTooWeak.
  ///
  /// In en, this message translates to:
  /// **'Too weak'**
  String get vaultStrengthTooWeak;

  /// No description provided for @vaultStrengthWeak.
  ///
  /// In en, this message translates to:
  /// **'Weak'**
  String get vaultStrengthWeak;

  /// No description provided for @vaultStrengthGood.
  ///
  /// In en, this message translates to:
  /// **'Good'**
  String get vaultStrengthGood;

  /// No description provided for @vaultStrengthStrong.
  ///
  /// In en, this message translates to:
  /// **'Strong'**
  String get vaultStrengthStrong;

  /// No description provided for @vaultStrengthLabel.
  ///
  /// In en, this message translates to:
  /// **'Strength: {label}'**
  String vaultStrengthLabel(String label);

  /// No description provided for @vaultHintTooShort.
  ///
  /// In en, this message translates to:
  /// **'Use at least {count} characters, or generate a passphrase.'**
  String vaultHintTooShort(int count);

  /// No description provided for @vaultHintTooWeak.
  ///
  /// In en, this message translates to:
  /// **'Easy to guess. Add more words or characters, avoid names, years and common passwords.'**
  String get vaultHintTooWeak;

  /// No description provided for @vaultHintGood.
  ///
  /// In en, this message translates to:
  /// **'Good. Longer is stronger: four or more unrelated words work well.'**
  String get vaultHintGood;

  /// No description provided for @vaultHintStrong.
  ///
  /// In en, this message translates to:
  /// **'Strong passphrase.'**
  String get vaultHintStrong;

  /// No description provided for @vaultWarningCallout.
  ///
  /// In en, this message translates to:
  /// **'Only you hold the key. If you lose both the passphrase and the recovery key, nobody — not even an admin — can open this vault again.'**
  String get vaultWarningCallout;

  /// No description provided for @vaultNext.
  ///
  /// In en, this message translates to:
  /// **'Next'**
  String get vaultNext;

  /// No description provided for @vaultBack.
  ///
  /// In en, this message translates to:
  /// **'Back'**
  String get vaultBack;

  /// No description provided for @vaultStepDetails.
  ///
  /// In en, this message translates to:
  /// **'Step 1 of 3 · Name and passphrase'**
  String get vaultStepDetails;

  /// No description provided for @vaultStepRecovery.
  ///
  /// In en, this message translates to:
  /// **'Step 2 of 3 · Recovery key'**
  String get vaultStepRecovery;

  /// No description provided for @vaultStepConfirm.
  ///
  /// In en, this message translates to:
  /// **'Step 3 of 3 · Check the recovery key'**
  String get vaultStepConfirm;

  /// No description provided for @vaultRecoveryTitle.
  ///
  /// In en, this message translates to:
  /// **'Recovery key'**
  String get vaultRecoveryTitle;

  /// No description provided for @vaultRecoveryIntro.
  ///
  /// In en, this message translates to:
  /// **'These 24 words open the vault if you forget the passphrase. Write them down or print the sheet and keep it somewhere safe, offline.'**
  String get vaultRecoveryIntro;

  /// No description provided for @vaultRecoveryDontStore.
  ///
  /// In en, this message translates to:
  /// **'Don’t store the recovery key in NeoCloud. Anyone who has it can open the vault.'**
  String get vaultRecoveryDontStore;

  /// No description provided for @vaultRecoveryCopy.
  ///
  /// In en, this message translates to:
  /// **'Copy words'**
  String get vaultRecoveryCopy;

  /// No description provided for @vaultRecoveryDownload.
  ///
  /// In en, this message translates to:
  /// **'Download recovery sheet'**
  String get vaultRecoveryDownload;

  /// No description provided for @vaultRecoverySaved.
  ///
  /// In en, this message translates to:
  /// **'I saved the recovery key somewhere safe'**
  String get vaultRecoverySaved;

  /// No description provided for @vaultRecoveryCheckIntro.
  ///
  /// In en, this message translates to:
  /// **'Type these words from your recovery key to make sure you have it.'**
  String get vaultRecoveryCheckIntro;

  /// No description provided for @vaultRecoveryWordNumber.
  ///
  /// In en, this message translates to:
  /// **'Word {number}'**
  String vaultRecoveryWordNumber(String number);

  /// No description provided for @vaultRecoveryCheckWrong.
  ///
  /// In en, this message translates to:
  /// **'That doesn’t match the recovery key. Check what you saved.'**
  String get vaultRecoveryCheckWrong;

  /// No description provided for @vaultSheetTitle.
  ///
  /// In en, this message translates to:
  /// **'NeoCloud vault recovery key'**
  String get vaultSheetTitle;

  /// No description provided for @vaultSheetVault.
  ///
  /// In en, this message translates to:
  /// **'Vault: {name}'**
  String vaultSheetVault(String name);

  /// No description provided for @vaultSheetServer.
  ///
  /// In en, this message translates to:
  /// **'Server: {server}'**
  String vaultSheetServer(String server);

  /// No description provided for @vaultSheetCreated.
  ///
  /// In en, this message translates to:
  /// **'Created: {date}'**
  String vaultSheetCreated(String date);

  /// No description provided for @vaultSheetUse.
  ///
  /// In en, this message translates to:
  /// **'If you forget the passphrase: open the vault in NeoCloud, choose “Forgot passphrase?” and enter these 24 words in order.'**
  String get vaultSheetUse;

  /// No description provided for @vaultSheetKeep.
  ///
  /// In en, this message translates to:
  /// **'Keep this sheet offline, e.g. printed with your important papers. Don’t store it in NeoCloud. Anyone with these words can open the vault.'**
  String get vaultSheetKeep;

  /// No description provided for @vaultRecoveryCopied.
  ///
  /// In en, this message translates to:
  /// **'Recovery words copied. Paste them somewhere offline and clear the clipboard.'**
  String get vaultRecoveryCopied;

  /// No description provided for @vaultForgot.
  ///
  /// In en, this message translates to:
  /// **'Forgot passphrase?'**
  String get vaultForgot;

  /// No description provided for @vaultRecoverTitle.
  ///
  /// In en, this message translates to:
  /// **'Reset the passphrase'**
  String get vaultRecoverTitle;

  /// No description provided for @vaultRecoverIntro.
  ///
  /// In en, this message translates to:
  /// **'Enter the 24 words of the recovery key you saved when you created this vault, then choose a new passphrase.'**
  String get vaultRecoverIntro;

  /// No description provided for @vaultRecoverWords.
  ///
  /// In en, this message translates to:
  /// **'Recovery key (24 words)'**
  String get vaultRecoverWords;

  /// No description provided for @vaultRecoverWordCount.
  ///
  /// In en, this message translates to:
  /// **'{count} of 24 words'**
  String vaultRecoverWordCount(int count);

  /// No description provided for @vaultRecoverUnknownWord.
  ///
  /// In en, this message translates to:
  /// **'Word {number} isn’t a recovery word. Check the spelling.'**
  String vaultRecoverUnknownWord(String number);

  /// No description provided for @vaultRecoverChecksum.
  ///
  /// In en, this message translates to:
  /// **'These words don’t form a valid recovery key. Check their order and spelling.'**
  String get vaultRecoverChecksum;

  /// No description provided for @vaultRecoverWordsOk.
  ///
  /// In en, this message translates to:
  /// **'Recovery key looks right.'**
  String get vaultRecoverWordsOk;

  /// No description provided for @vaultRecoverMismatch.
  ///
  /// In en, this message translates to:
  /// **'This recovery key belongs to a different vault.'**
  String get vaultRecoverMismatch;

  /// No description provided for @vaultNewPassphrase.
  ///
  /// In en, this message translates to:
  /// **'New passphrase'**
  String get vaultNewPassphrase;

  /// No description provided for @vaultRecoverSubmit.
  ///
  /// In en, this message translates to:
  /// **'Reset and unlock'**
  String get vaultRecoverSubmit;

  /// No description provided for @vaultRecovered.
  ///
  /// In en, this message translates to:
  /// **'New passphrase set. The vault is unlocked; your recovery key stays the same.'**
  String get vaultRecovered;

  /// No description provided for @vaultShowRecovery.
  ///
  /// In en, this message translates to:
  /// **'Show recovery key'**
  String get vaultShowRecovery;

  /// No description provided for @vaultShowRecoveryIntro.
  ///
  /// In en, this message translates to:
  /// **'Enter the vault’s passphrase to see its recovery key.'**
  String get vaultShowRecoveryIntro;

  /// No description provided for @vaultBanner.
  ///
  /// In en, this message translates to:
  /// **'No previews, search or sharing in vaults — by design. Names and contents are encrypted on this device before upload.'**
  String get vaultBanner;

  /// No description provided for @vaultUnlocking.
  ///
  /// In en, this message translates to:
  /// **'Unlocking…'**
  String get vaultUnlocking;

  /// No description provided for @securityChecklistTitle.
  ///
  /// In en, this message translates to:
  /// **'Make this server recoverable'**
  String get securityChecklistTitle;

  /// No description provided for @securityChecklistSubtitle.
  ///
  /// In en, this message translates to:
  /// **'Three things that decide whether your files survive a lost disk or server.'**
  String get securityChecklistSubtitle;

  /// No description provided for @securityMasterKeyItem.
  ///
  /// In en, this message translates to:
  /// **'Master key exported'**
  String get securityMasterKeyItem;

  /// No description provided for @securityMasterKeyItemHint.
  ///
  /// In en, this message translates to:
  /// **'Every file and backup is encrypted under it. Keep a copy offline, outside this server.'**
  String get securityMasterKeyItemHint;

  /// No description provided for @securityBackupTargetItem.
  ///
  /// In en, this message translates to:
  /// **'Backup target set up'**
  String get securityBackupTargetItem;

  /// No description provided for @securityBackupTargetItemHint.
  ///
  /// In en, this message translates to:
  /// **'A second place – another disk, SFTP, S3 or WebDAV – that gets an encrypted copy.'**
  String get securityBackupTargetItemHint;

  /// No description provided for @securityBackupVerifiedItem.
  ///
  /// In en, this message translates to:
  /// **'Backup verified'**
  String get securityBackupVerifiedItem;

  /// No description provided for @securityBackupVerifiedItemHint.
  ///
  /// In en, this message translates to:
  /// **'A test restore of the latest snapshot worked.'**
  String get securityBackupVerifiedItemHint;

  /// No description provided for @securityBackupRanItem.
  ///
  /// In en, this message translates to:
  /// **'A backup ran successfully'**
  String get securityBackupRanItem;

  /// No description provided for @securityExportKey.
  ///
  /// In en, this message translates to:
  /// **'Export master key'**
  String get securityExportKey;

  /// No description provided for @securitySetUpBackup.
  ///
  /// In en, this message translates to:
  /// **'Set up backups'**
  String get securitySetUpBackup;

  /// No description provided for @securityVerifyNow.
  ///
  /// In en, this message translates to:
  /// **'Verify now'**
  String get securityVerifyNow;

  /// No description provided for @securityVerifyOk.
  ///
  /// In en, this message translates to:
  /// **'Backup verified.'**
  String get securityVerifyOk;

  /// No description provided for @securityVerifyFailed.
  ///
  /// In en, this message translates to:
  /// **'Verification failed: {error}'**
  String securityVerifyFailed(String error);

  /// No description provided for @securityDone.
  ///
  /// In en, this message translates to:
  /// **'Done'**
  String get securityDone;

  /// No description provided for @securityOpen.
  ///
  /// In en, this message translates to:
  /// **'Open'**
  String get securityOpen;

  /// No description provided for @securityBanner.
  ///
  /// In en, this message translates to:
  /// **'Export the master key and keep it offline. Without it, files and backups can’t be decrypted if this server is lost.'**
  String get securityBanner;

  /// No description provided for @securityKeyStored.
  ///
  /// In en, this message translates to:
  /// **'I stored the master key somewhere safe, outside this server'**
  String get securityKeyStored;

  /// No description provided for @securityKeyDownload.
  ///
  /// In en, this message translates to:
  /// **'Download as file'**
  String get securityKeyDownload;

  /// No description provided for @securityDatabaseOk.
  ///
  /// In en, this message translates to:
  /// **'OK'**
  String get securityDatabaseOk;

  /// No description provided for @securityDatabasePending.
  ///
  /// In en, this message translates to:
  /// **'Checking…'**
  String get securityDatabasePending;

  /// No description provided for @securityDatabaseDamaged.
  ///
  /// In en, this message translates to:
  /// **'Damaged'**
  String get securityDatabaseDamaged;

  /// No description provided for @securityDatabaseCheckedAt.
  ///
  /// In en, this message translates to:
  /// **'checked {date}'**
  String securityDatabaseCheckedAt(String date);

  /// No description provided for @securityDamagedBlobs.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 damaged file found by the background check} other{{count} damaged files found by the background check}}'**
  String securityDamagedBlobs(int count);

  /// No description provided for @securityDiskFull.
  ///
  /// In en, this message translates to:
  /// **'The server’s disk is full. Ask the admin to free up space; nothing was saved.'**
  String get securityDiskFull;

  /// No description provided for @securityServerStopping.
  ///
  /// In en, this message translates to:
  /// **'The server is restarting. Try again in a moment.'**
  String get securityServerStopping;

  /// No description provided for @filesFavoritesEmpty.
  ///
  /// In en, this message translates to:
  /// **'No favorites yet'**
  String get filesFavoritesEmpty;

  /// No description provided for @filesFavoritesEmptySubtitle.
  ///
  /// In en, this message translates to:
  /// **'Star files and folders you use often; they show up here.'**
  String get filesFavoritesEmptySubtitle;

  /// No description provided for @filesFavoritesSubtitle.
  ///
  /// In en, this message translates to:
  /// **'Files and folders you starred.'**
  String get filesFavoritesSubtitle;

  /// No description provided for @filesNavFavorites.
  ///
  /// In en, this message translates to:
  /// **'Favorites'**
  String get filesNavFavorites;

  /// No description provided for @filesTrashExpiresIn.
  ///
  /// In en, this message translates to:
  /// **'{days, plural, =0{deleted permanently today} =1{deleted permanently in 1 day} other{deleted permanently in {days} days}}'**
  String filesTrashExpiresIn(int days);

  /// No description provided for @filesAndMore.
  ///
  /// In en, this message translates to:
  /// **'{names} and {count} more'**
  String filesAndMore(String names, int count);

  /// No description provided for @filesCut.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 item cut · paste it in another folder} other{{count} items cut · paste them in another folder}}'**
  String filesCut(int count);

  /// No description provided for @filesCutAction.
  ///
  /// In en, this message translates to:
  /// **'Cut'**
  String get filesCutAction;

  /// No description provided for @filesFavorite.
  ///
  /// In en, this message translates to:
  /// **'favorite'**
  String get filesFavorite;

  /// No description provided for @filesFavoriteFailed.
  ///
  /// In en, this message translates to:
  /// **'Couldn\'t change the star of {names}.'**
  String filesFavoriteFailed(String names);

  /// No description provided for @filesKindFolder.
  ///
  /// In en, this message translates to:
  /// **'folder'**
  String get filesKindFolder;

  /// No description provided for @filesModifiedOn.
  ///
  /// In en, this message translates to:
  /// **'modified {date}'**
  String filesModifiedOn(String date);

  /// No description provided for @filesModifiedToday.
  ///
  /// In en, this message translates to:
  /// **'modified today'**
  String get filesModifiedToday;

  /// No description provided for @filesModifiedYesterday.
  ///
  /// In en, this message translates to:
  /// **'modified yesterday'**
  String get filesModifiedYesterday;

  /// No description provided for @filesMoveFailed.
  ///
  /// In en, this message translates to:
  /// **'Couldn\'t move {names} to “{folder}”.'**
  String filesMoveFailed(String names, String folder);

  /// No description provided for @filesPasteHere.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{Paste 1 item here} other{Paste {count} items here}}'**
  String filesPasteHere(int count);

  /// No description provided for @filesRenameFailed.
  ///
  /// In en, this message translates to:
  /// **'Couldn\'t rename “{name}”.'**
  String filesRenameFailed(String name);

  /// No description provided for @filesRestoreFailed.
  ///
  /// In en, this message translates to:
  /// **'Couldn\'t restore {names}.'**
  String filesRestoreFailed(String names);

  /// No description provided for @filesShortcutsHint.
  ///
  /// In en, this message translates to:
  /// **'Press ? for keyboard shortcuts · Ctrl/⌘+K for commands · drag items onto a folder to move them'**
  String get filesShortcutsHint;

  /// No description provided for @filesStar.
  ///
  /// In en, this message translates to:
  /// **'Add to favorites'**
  String get filesStar;

  /// No description provided for @filesStarred.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{Added to favorites} other{{count} items added to favorites}}'**
  String filesStarred(int count);

  /// No description provided for @filesTrashFailed.
  ///
  /// In en, this message translates to:
  /// **'Couldn\'t move {names} to the trash.'**
  String filesTrashFailed(String names);

  /// No description provided for @filesUndoFailed.
  ///
  /// In en, this message translates to:
  /// **'Couldn\'t undo for {names}.'**
  String filesUndoFailed(String names);

  /// No description provided for @filesUnstar.
  ///
  /// In en, this message translates to:
  /// **'Remove from favorites'**
  String get filesUnstar;

  /// No description provided for @filesUnstarred.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{Removed from favorites} other{{count} items removed from favorites}}'**
  String filesUnstarred(int count);

  /// No description provided for @filesWelcomeTitle.
  ///
  /// In en, this message translates to:
  /// **'Welcome to your cloud'**
  String get filesWelcomeTitle;

  /// No description provided for @filesWelcomeUploadBody.
  ///
  /// In en, this message translates to:
  /// **'Upload files or drop them anywhere on this page.'**
  String get filesWelcomeUploadBody;

  /// No description provided for @filesWelcomeDesktopBody.
  ///
  /// In en, this message translates to:
  /// **'Keep a folder on your computer in sync.'**
  String get filesWelcomeDesktopBody;

  /// No description provided for @filesWelcomeDesktopAction.
  ///
  /// In en, this message translates to:
  /// **'Get the app'**
  String get filesWelcomeDesktopAction;

  /// No description provided for @filesWelcomeKeyBody.
  ///
  /// In en, this message translates to:
  /// **'Without it, a restored server can\'t read any file. Store it somewhere safe.'**
  String get filesWelcomeKeyBody;

  /// No description provided for @filesWelcomeKeyAction.
  ///
  /// In en, this message translates to:
  /// **'Open admin'**
  String get filesWelcomeKeyAction;

  /// No description provided for @paletteActions.
  ///
  /// In en, this message translates to:
  /// **'Actions'**
  String get paletteActions;

  /// No description provided for @paletteFolders.
  ///
  /// In en, this message translates to:
  /// **'Go to folder'**
  String get paletteFolders;

  /// No description provided for @paletteHint.
  ///
  /// In en, this message translates to:
  /// **'Type a command, folder or page'**
  String get paletteHint;

  /// No description provided for @paletteNothing.
  ///
  /// In en, this message translates to:
  /// **'Nothing matches.'**
  String get paletteNothing;

  /// No description provided for @palettePlaces.
  ///
  /// In en, this message translates to:
  /// **'Places'**
  String get palettePlaces;

  /// No description provided for @paletteRecent.
  ///
  /// In en, this message translates to:
  /// **'Recent files'**
  String get paletteRecent;

  /// No description provided for @paletteSearchFiles.
  ///
  /// In en, this message translates to:
  /// **'Search files'**
  String get paletteSearchFiles;

  /// No description provided for @paletteShortcuts.
  ///
  /// In en, this message translates to:
  /// **'Keyboard shortcuts'**
  String get paletteShortcuts;

  /// No description provided for @paletteTitle.
  ///
  /// In en, this message translates to:
  /// **'Command palette'**
  String get paletteTitle;

  /// No description provided for @paletteShortcutClear.
  ///
  /// In en, this message translates to:
  /// **'Clear selection or search'**
  String get paletteShortcutClear;

  /// No description provided for @paletteShortcutCutPaste.
  ///
  /// In en, this message translates to:
  /// **'Cut and paste into another folder'**
  String get paletteShortcutCutPaste;

  /// No description provided for @paletteShortcutExtend.
  ///
  /// In en, this message translates to:
  /// **'Extend the selection'**
  String get paletteShortcutExtend;

  /// No description provided for @paletteShortcutHomeEnd.
  ///
  /// In en, this message translates to:
  /// **'First or last item'**
  String get paletteShortcutHomeEnd;

  /// No description provided for @paletteShortcutMove.
  ///
  /// In en, this message translates to:
  /// **'Move between items'**
  String get paletteShortcutMove;

  /// No description provided for @paletteShortcutSelectAll.
  ///
  /// In en, this message translates to:
  /// **'Select all'**
  String get paletteShortcutSelectAll;

  /// No description provided for @paletteShortcutToggle.
  ///
  /// In en, this message translates to:
  /// **'Select or deselect'**
  String get paletteShortcutToggle;

  /// No description provided for @paletteShortcutTypeAhead.
  ///
  /// In en, this message translates to:
  /// **'Jump to a name by typing it'**
  String get paletteShortcutTypeAhead;

  /// No description provided for @searchAny.
  ///
  /// In en, this message translates to:
  /// **'Any'**
  String get searchAny;

  /// No description provided for @searchClearFilters.
  ///
  /// In en, this message translates to:
  /// **'Clear filters'**
  String get searchClearFilters;

  /// No description provided for @searchEmptyHint.
  ///
  /// In en, this message translates to:
  /// **'Try fewer words, other filters, or search all folders.'**
  String get searchEmptyHint;

  /// No description provided for @searchEverywhere.
  ///
  /// In en, this message translates to:
  /// **'Search all folders'**
  String get searchEverywhere;

  /// No description provided for @searchLookInTrash.
  ///
  /// In en, this message translates to:
  /// **'Look in the trash'**
  String get searchLookInTrash;

  /// No description provided for @searchModified.
  ///
  /// In en, this message translates to:
  /// **'Modified'**
  String get searchModified;

  /// No description provided for @searchModifiedDay.
  ///
  /// In en, this message translates to:
  /// **'Last 24 hours'**
  String get searchModifiedDay;

  /// No description provided for @searchModifiedWeek.
  ///
  /// In en, this message translates to:
  /// **'Last 7 days'**
  String get searchModifiedWeek;

  /// No description provided for @searchModifiedMonth.
  ///
  /// In en, this message translates to:
  /// **'Last 30 days'**
  String get searchModifiedMonth;

  /// No description provided for @searchModifiedYear.
  ///
  /// In en, this message translates to:
  /// **'Last year'**
  String get searchModifiedYear;

  /// No description provided for @searchNothingFiltered.
  ///
  /// In en, this message translates to:
  /// **'Nothing matches these filters.'**
  String get searchNothingFiltered;

  /// No description provided for @searchOpenFolder.
  ///
  /// In en, this message translates to:
  /// **'Open containing folder'**
  String get searchOpenFolder;

  /// No description provided for @searchResultsCount.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =0{No results} =1{1 result} other{{count} results}}'**
  String searchResultsCount(int count);

  /// No description provided for @searchSize.
  ///
  /// In en, this message translates to:
  /// **'Size'**
  String get searchSize;

  /// No description provided for @searchSize10m.
  ///
  /// In en, this message translates to:
  /// **'Over 10 MB'**
  String get searchSize10m;

  /// No description provided for @searchSize100m.
  ///
  /// In en, this message translates to:
  /// **'Over 100 MB'**
  String get searchSize100m;

  /// No description provided for @searchSize1g.
  ///
  /// In en, this message translates to:
  /// **'Over 1 GB'**
  String get searchSize1g;

  /// No description provided for @searchThisFolder.
  ///
  /// In en, this message translates to:
  /// **'This folder'**
  String get searchThisFolder;

  /// No description provided for @searchType.
  ///
  /// In en, this message translates to:
  /// **'Type'**
  String get searchType;

  /// No description provided for @searchTypeArchives.
  ///
  /// In en, this message translates to:
  /// **'Archives'**
  String get searchTypeArchives;

  /// No description provided for @searchTypeAudio.
  ///
  /// In en, this message translates to:
  /// **'Audio'**
  String get searchTypeAudio;

  /// No description provided for @searchTypeDocuments.
  ///
  /// In en, this message translates to:
  /// **'Documents'**
  String get searchTypeDocuments;

  /// No description provided for @searchTypeFolders.
  ///
  /// In en, this message translates to:
  /// **'Folders'**
  String get searchTypeFolders;

  /// No description provided for @searchTypeImages.
  ///
  /// In en, this message translates to:
  /// **'Images'**
  String get searchTypeImages;

  /// No description provided for @searchTypePdf.
  ///
  /// In en, this message translates to:
  /// **'PDFs'**
  String get searchTypePdf;

  /// No description provided for @searchTypeVideos.
  ///
  /// In en, this message translates to:
  /// **'Videos'**
  String get searchTypeVideos;

  /// No description provided for @storageAlmostFull.
  ///
  /// In en, this message translates to:
  /// **'Almost full'**
  String get storageAlmostFull;

  /// No description provided for @storageByType.
  ///
  /// In en, this message translates to:
  /// **'By type'**
  String get storageByType;

  /// No description provided for @storageDetailsUnavailable.
  ///
  /// In en, this message translates to:
  /// **'A detailed breakdown isn’t available right now.'**
  String get storageDetailsUnavailable;

  /// No description provided for @storageDuplicateGroup.
  ///
  /// In en, this message translates to:
  /// **'{count} identical copies · {size} each'**
  String storageDuplicateGroup(int count, String size);

  /// No description provided for @storageDuplicates.
  ///
  /// In en, this message translates to:
  /// **'Duplicates'**
  String get storageDuplicates;

  /// No description provided for @storageKeepNewest.
  ///
  /// In en, this message translates to:
  /// **'Keep newest'**
  String get storageKeepNewest;

  /// No description provided for @storageLargest.
  ///
  /// In en, this message translates to:
  /// **'Largest files'**
  String get storageLargest;

  /// No description provided for @storageNewest.
  ///
  /// In en, this message translates to:
  /// **'Newest'**
  String get storageNewest;

  /// No description provided for @storageNoDuplicates.
  ///
  /// In en, this message translates to:
  /// **'No identical files found.'**
  String get storageNoDuplicates;

  /// No description provided for @storageReclaimable.
  ///
  /// In en, this message translates to:
  /// **'{size} can be freed'**
  String storageReclaimable(String size);

  /// No description provided for @storageTypeOther.
  ///
  /// In en, this message translates to:
  /// **'Other'**
  String get storageTypeOther;

  /// No description provided for @storageTypeVaults.
  ///
  /// In en, this message translates to:
  /// **'Vaults'**
  String get storageTypeVaults;

  /// No description provided for @undoAction.
  ///
  /// In en, this message translates to:
  /// **'Undo'**
  String get undoAction;

  /// No description provided for @undoMoved.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 item moved to “{folder}”} other{{count} items moved to “{folder}”}}'**
  String undoMoved(int count, String folder);

  /// No description provided for @undoRenamed.
  ///
  /// In en, this message translates to:
  /// **'Renamed to “{name}”'**
  String undoRenamed(String name);

  /// No description provided for @undoRestored.
  ///
  /// In en, this message translates to:
  /// **'“{name}” restored'**
  String undoRestored(String name);

  /// No description provided for @undoTrashed.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{“{name}” moved to trash} other{{count} items moved to trash}}'**
  String undoTrashed(int count, String name);

  /// No description provided for @uploadAnnounceFailed.
  ///
  /// In en, this message translates to:
  /// **'Upload failed: {name}'**
  String uploadAnnounceFailed(String name);

  /// No description provided for @uploadAnnounceFinishedWithFailures.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{Uploads finished, 1 failed} other{Uploads finished, {count} failed}}'**
  String uploadAnnounceFinishedWithFailures(int count);

  /// No description provided for @uploadConflictApplyToAll.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{Do this for the 1 remaining upload} other{Do this for all {count} remaining uploads}}'**
  String uploadConflictApplyToAll(int count);

  /// No description provided for @uploadConflictExisting.
  ///
  /// In en, this message translates to:
  /// **'ALREADY THERE'**
  String get uploadConflictExisting;

  /// No description provided for @uploadConflictIncoming.
  ///
  /// In en, this message translates to:
  /// **'NEW UPLOAD'**
  String get uploadConflictIncoming;

  /// No description provided for @uploadConflictKeepBothAs.
  ///
  /// In en, this message translates to:
  /// **'Keep both saves the new file as “{name}”.'**
  String uploadConflictKeepBothAs(String name);

  /// No description provided for @uploadDismiss.
  ///
  /// In en, this message translates to:
  /// **'Dismiss'**
  String get uploadDismiss;

  /// No description provided for @uploadEtaHours.
  ///
  /// In en, this message translates to:
  /// **'about {count} h'**
  String uploadEtaHours(int count);

  /// No description provided for @uploadEtaMinutes.
  ///
  /// In en, this message translates to:
  /// **'about {count} min'**
  String uploadEtaMinutes(int count);

  /// No description provided for @uploadEtaUnderMinute.
  ///
  /// In en, this message translates to:
  /// **'less than a minute'**
  String get uploadEtaUnderMinute;

  /// No description provided for @uploadFinalizing.
  ///
  /// In en, this message translates to:
  /// **'Finishing…'**
  String get uploadFinalizing;

  /// No description provided for @uploadInterrupted.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 upload was interrupted} other{{count} uploads were interrupted}}'**
  String uploadInterrupted(int count);

  /// No description provided for @uploadInterruptedHint.
  ///
  /// In en, this message translates to:
  /// **'Pick the same files again to continue where they stopped.'**
  String get uploadInterruptedHint;

  /// No description provided for @uploadPause.
  ///
  /// In en, this message translates to:
  /// **'Pause'**
  String get uploadPause;

  /// No description provided for @uploadPauseAll.
  ///
  /// In en, this message translates to:
  /// **'Pause all'**
  String get uploadPauseAll;

  /// No description provided for @uploadPaused.
  ///
  /// In en, this message translates to:
  /// **'Paused'**
  String get uploadPaused;

  /// No description provided for @uploadProgressCount.
  ///
  /// In en, this message translates to:
  /// **'{done} of {total}'**
  String uploadProgressCount(int done, int total);

  /// No description provided for @uploadResume.
  ///
  /// In en, this message translates to:
  /// **'Resume'**
  String get uploadResume;

  /// No description provided for @uploadResumeAll.
  ///
  /// In en, this message translates to:
  /// **'Resume all'**
  String get uploadResumeAll;

  /// No description provided for @uploadResumeReselect.
  ///
  /// In en, this message translates to:
  /// **'Resume (re-select files)'**
  String get uploadResumeReselect;

  /// No description provided for @uploadWaitingForConnection.
  ///
  /// In en, this message translates to:
  /// **'Waiting for connection'**
  String get uploadWaitingForConnection;

  /// No description provided for @cameraStatusOff.
  ///
  /// In en, this message translates to:
  /// **'Off'**
  String get cameraStatusOff;

  /// No description provided for @cameraStatusUpToDate.
  ///
  /// In en, this message translates to:
  /// **'Up to date'**
  String get cameraStatusUpToDate;

  /// No description provided for @cameraStatusUploading.
  ///
  /// In en, this message translates to:
  /// **'Uploading {current} of {total}'**
  String cameraStatusUploading(int current, int total);

  /// No description provided for @cameraStatusPending.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 file waiting} other{{count} files waiting}}'**
  String cameraStatusPending(int count);

  /// No description provided for @cameraStatusWaitingWifi.
  ///
  /// In en, this message translates to:
  /// **'Waiting for Wi-Fi'**
  String get cameraStatusWaitingWifi;

  /// No description provided for @cameraStatusWaitingCharger.
  ///
  /// In en, this message translates to:
  /// **'Waiting for charger'**
  String get cameraStatusWaitingCharger;

  /// No description provided for @cameraStatusLimited.
  ///
  /// In en, this message translates to:
  /// **'Limited photo access'**
  String get cameraStatusLimited;

  /// No description provided for @cameraStatusNoAccess.
  ///
  /// In en, this message translates to:
  /// **'No photo access'**
  String get cameraStatusNoAccess;

  /// No description provided for @cameraStatusNoFolder.
  ///
  /// In en, this message translates to:
  /// **'No folder chosen'**
  String get cameraStatusNoFolder;

  /// No description provided for @cameraStatusUnreachable.
  ///
  /// In en, this message translates to:
  /// **'Server unreachable'**
  String get cameraStatusUnreachable;

  /// No description provided for @cameraStatusSignedOut.
  ///
  /// In en, this message translates to:
  /// **'Signed out'**
  String get cameraStatusSignedOut;

  /// No description provided for @cameraStatusServerFull.
  ///
  /// In en, this message translates to:
  /// **'Server full'**
  String get cameraStatusServerFull;

  /// No description provided for @cameraStatusFailed.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 failed} other{{count} failed}}'**
  String cameraStatusFailed(int count);

  /// No description provided for @cameraCountOnPhone.
  ///
  /// In en, this message translates to:
  /// **'On phone'**
  String get cameraCountOnPhone;

  /// No description provided for @cameraCountBackedUp.
  ///
  /// In en, this message translates to:
  /// **'Backed up'**
  String get cameraCountBackedUp;

  /// No description provided for @cameraCountPending.
  ///
  /// In en, this message translates to:
  /// **'Pending'**
  String get cameraCountPending;

  /// No description provided for @cameraCountFailed.
  ///
  /// In en, this message translates to:
  /// **'Failed'**
  String get cameraCountFailed;

  /// No description provided for @cameraWaitingCloud.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 waiting on iCloud} other{{count} waiting on iCloud}}'**
  String cameraWaitingCloud(int count);

  /// No description provided for @cameraBackupTitle.
  ///
  /// In en, this message translates to:
  /// **'Photo backup'**
  String get cameraBackupTitle;

  /// No description provided for @cameraBackupEnableDescription.
  ///
  /// In en, this message translates to:
  /// **'Back up the photos on this phone to NeoCloud. Nothing is ever deleted from the phone.'**
  String get cameraBackupEnableDescription;

  /// No description provided for @cameraScopeTitle.
  ///
  /// In en, this message translates to:
  /// **'What should be backed up?'**
  String get cameraScopeTitle;

  /// No description provided for @cameraScopeNew.
  ///
  /// In en, this message translates to:
  /// **'New photos only'**
  String get cameraScopeNew;

  /// No description provided for @cameraScopeNewHint.
  ///
  /// In en, this message translates to:
  /// **'Photos and videos taken from now on.'**
  String get cameraScopeNewHint;

  /// No description provided for @cameraScopeEverything.
  ///
  /// In en, this message translates to:
  /// **'Everything ({count} items, about {size})'**
  String cameraScopeEverything(int count, String size);

  /// No description provided for @cameraScopeEverythingCount.
  ///
  /// In en, this message translates to:
  /// **'Everything ({count} items)'**
  String cameraScopeEverythingCount(int count);

  /// No description provided for @cameraScopeEverythingHint.
  ///
  /// In en, this message translates to:
  /// **'Your whole library, newest first. The first run takes a while.'**
  String get cameraScopeEverythingHint;

  /// No description provided for @cameraScopeCounting.
  ///
  /// In en, this message translates to:
  /// **'Counting your library…'**
  String get cameraScopeCounting;

  /// No description provided for @cameraScopeSetting.
  ///
  /// In en, this message translates to:
  /// **'Back up'**
  String get cameraScopeSetting;

  /// No description provided for @cameraScopeNewShort.
  ///
  /// In en, this message translates to:
  /// **'New only'**
  String get cameraScopeNewShort;

  /// No description provided for @cameraScopeEverythingShort.
  ///
  /// In en, this message translates to:
  /// **'Everything'**
  String get cameraScopeEverythingShort;

  /// No description provided for @cameraChargingOnly.
  ///
  /// In en, this message translates to:
  /// **'Only while charging'**
  String get cameraChargingOnly;

  /// No description provided for @cameraChargingHint.
  ///
  /// In en, this message translates to:
  /// **'In the background Android waits for the charger. iOS can’t tell apps, so it backs up when you open NeoCloud or tap Back up now.'**
  String get cameraChargingHint;

  /// No description provided for @cameraVideosWifiOnly.
  ///
  /// In en, this message translates to:
  /// **'Videos only on Wi-Fi'**
  String get cameraVideosWifiOnly;

  /// No description provided for @cameraVideosWifiOnlyHint.
  ///
  /// In en, this message translates to:
  /// **'Photos may use mobile data, videos wait for Wi-Fi.'**
  String get cameraVideosWifiOnlyHint;

  /// No description provided for @cameraLimitedMessage.
  ///
  /// In en, this message translates to:
  /// **'NeoCloud can only see the photos you selected. The others aren’t backed up.'**
  String get cameraLimitedMessage;

  /// No description provided for @cameraAllowFullAccess.
  ///
  /// In en, this message translates to:
  /// **'Allow full access'**
  String get cameraAllowFullAccess;

  /// No description provided for @cameraIosBackgroundTip.
  ///
  /// In en, this message translates to:
  /// **'iOS runs background tasks rarely. Open NeoCloud now and then, best on Wi-Fi and while charging.'**
  String get cameraIosBackgroundTip;

  /// No description provided for @backgroundNotificationChannel.
  ///
  /// In en, this message translates to:
  /// **'Backup progress'**
  String get backgroundNotificationChannel;

  /// No description provided for @backgroundNotificationProgress.
  ///
  /// In en, this message translates to:
  /// **'Backing up {current} of {total}'**
  String backgroundNotificationProgress(int current, int total);

  /// No description provided for @backgroundNotificationPause.
  ///
  /// In en, this message translates to:
  /// **'Pause'**
  String get backgroundNotificationPause;

  /// No description provided for @backgroundCameraPaused.
  ///
  /// In en, this message translates to:
  /// **'Paused'**
  String get backgroundCameraPaused;

  /// No description provided for @backgroundTitle.
  ///
  /// In en, this message translates to:
  /// **'Keep backup running'**
  String get backgroundTitle;

  /// No description provided for @backgroundIntroAndroid.
  ///
  /// In en, this message translates to:
  /// **'Camera upload keeps going while NeoCloud is closed. To save battery, Android and your phone’s maker stop apps in the background; these settings keep NeoCloud running.'**
  String get backgroundIntroAndroid;

  /// No description provided for @backgroundIntroIos.
  ///
  /// In en, this message translates to:
  /// **'iOS decides when NeoCloud may run in the background: a few times a day, most often overnight on the charger. These settings give it every chance.'**
  String get backgroundIntroIos;

  /// No description provided for @backgroundStatusGranted.
  ///
  /// In en, this message translates to:
  /// **'Granted'**
  String get backgroundStatusGranted;

  /// No description provided for @backgroundStatusNeeded.
  ///
  /// In en, this message translates to:
  /// **'Needed'**
  String get backgroundStatusNeeded;

  /// No description provided for @backgroundStatusRecommended.
  ///
  /// In en, this message translates to:
  /// **'Recommended'**
  String get backgroundStatusRecommended;

  /// No description provided for @backgroundStatusOff.
  ///
  /// In en, this message translates to:
  /// **'Off'**
  String get backgroundStatusOff;

  /// No description provided for @backgroundPhotosTitle.
  ///
  /// In en, this message translates to:
  /// **'Photos and videos'**
  String get backgroundPhotosTitle;

  /// No description provided for @backgroundPhotosDetail.
  ///
  /// In en, this message translates to:
  /// **'Access to the whole library, not only selected photos, so every new photo is backed up.'**
  String get backgroundPhotosDetail;

  /// No description provided for @backgroundBatteryTitle.
  ///
  /// In en, this message translates to:
  /// **'Unrestricted battery'**
  String get backgroundBatteryTitle;

  /// No description provided for @backgroundBatteryDetail.
  ///
  /// In en, this message translates to:
  /// **'Lets Android run NeoCloud in the background and start a backup soon after you take a photo.'**
  String get backgroundBatteryDetail;

  /// No description provided for @backgroundManufacturerTitle.
  ///
  /// In en, this message translates to:
  /// **'{maker} settings'**
  String backgroundManufacturerTitle(String maker);

  /// No description provided for @backgroundManufacturerDetail.
  ///
  /// In en, this message translates to:
  /// **'{maker} stops apps in the background on top of Android. Change these so it leaves NeoCloud running:'**
  String backgroundManufacturerDetail(String maker);

  /// No description provided for @backgroundManufacturerGuide.
  ///
  /// In en, this message translates to:
  /// **'Guide for your phone on dontkillmyapp.com'**
  String get backgroundManufacturerGuide;

  /// No description provided for @backgroundNotificationsTitle.
  ///
  /// In en, this message translates to:
  /// **'Notifications'**
  String get backgroundNotificationsTitle;

  /// No description provided for @backgroundNotificationsDetailAndroid.
  ///
  /// In en, this message translates to:
  /// **'Shows the backup’s progress, with a Pause button.'**
  String get backgroundNotificationsDetailAndroid;

  /// No description provided for @backgroundNotificationsDetailIos.
  ///
  /// In en, this message translates to:
  /// **'Lets NeoCloud tell you when backup needs you.'**
  String get backgroundNotificationsDetailIos;

  /// No description provided for @backgroundDataTitle.
  ///
  /// In en, this message translates to:
  /// **'Background data'**
  String get backgroundDataTitle;

  /// No description provided for @backgroundDataDetail.
  ///
  /// In en, this message translates to:
  /// **'Data Saver blocks uploads while NeoCloud is closed unless it may use data without restriction.'**
  String get backgroundDataDetail;

  /// No description provided for @backgroundRefreshTitle.
  ///
  /// In en, this message translates to:
  /// **'Background App Refresh'**
  String get backgroundRefreshTitle;

  /// No description provided for @backgroundRefreshDetail.
  ///
  /// In en, this message translates to:
  /// **'Without it, iOS never runs NeoCloud in the background.'**
  String get backgroundRefreshDetail;

  /// No description provided for @backgroundRefreshRestricted.
  ///
  /// In en, this message translates to:
  /// **'Turned off by Screen Time or a device profile; it can only be turned on there.'**
  String get backgroundRefreshRestricted;

  /// No description provided for @backgroundLowPowerTitle.
  ///
  /// In en, this message translates to:
  /// **'Low Power Mode'**
  String get backgroundLowPowerTitle;

  /// No description provided for @backgroundLowPowerDetail.
  ///
  /// In en, this message translates to:
  /// **'While it is on, iOS pauses background tasks. Turn it off in Settings › Battery to let a backup run.'**
  String get backgroundLowPowerDetail;

  /// No description provided for @backgroundSwipeHint.
  ///
  /// In en, this message translates to:
  /// **'Don’t swipe NeoCloud away in the app switcher: iOS then stops its background tasks until you open it again.'**
  String get backgroundSwipeHint;

  /// No description provided for @backgroundAllow.
  ///
  /// In en, this message translates to:
  /// **'Allow'**
  String get backgroundAllow;

  /// No description provided for @backgroundAllowAll.
  ///
  /// In en, this message translates to:
  /// **'Allow all'**
  String get backgroundAllowAll;

  /// No description provided for @backgroundOpenSettings.
  ///
  /// In en, this message translates to:
  /// **'Open settings'**
  String get backgroundOpenSettings;

  /// No description provided for @backgroundNotNow.
  ///
  /// In en, this message translates to:
  /// **'Not now'**
  String get backgroundNotNow;

  /// No description provided for @backgroundBannerTitle.
  ///
  /// In en, this message translates to:
  /// **'Backup may not run in the background'**
  String get backgroundBannerTitle;

  /// No description provided for @backgroundBannerMissing.
  ///
  /// In en, this message translates to:
  /// **'Something NeoCloud needs to run in the background is turned off.'**
  String get backgroundBannerMissing;

  /// No description provided for @backgroundBannerStale.
  ///
  /// In en, this message translates to:
  /// **'No complete backup since {date}. The system may be stopping NeoCloud in the background.'**
  String backgroundBannerStale(String date);

  /// No description provided for @backgroundBannerFix.
  ///
  /// In en, this message translates to:
  /// **'Check settings'**
  String get backgroundBannerFix;

  /// No description provided for @backgroundReliabilityTitle.
  ///
  /// In en, this message translates to:
  /// **'Background reliability'**
  String get backgroundReliabilityTitle;

  /// No description provided for @backgroundReliabilityAllSet.
  ///
  /// In en, this message translates to:
  /// **'Everything NeoCloud needs is set.'**
  String get backgroundReliabilityAllSet;

  /// No description provided for @backgroundReliabilityOpen.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 setting to check} other{{count} settings to check}}'**
  String backgroundReliabilityOpen(int count);

  /// No description provided for @backgroundReliabilityCheck.
  ///
  /// In en, this message translates to:
  /// **'Check'**
  String get backgroundReliabilityCheck;

  /// No description provided for @backgroundStepSamsungUnrestricted.
  ///
  /// In en, this message translates to:
  /// **'Settings › Apps › NeoCloud › Battery › Unrestricted'**
  String get backgroundStepSamsungUnrestricted;

  /// No description provided for @backgroundStepSamsungNeverSleeping.
  ///
  /// In en, this message translates to:
  /// **'Settings › Battery › Background usage limits › Never sleeping apps › add NeoCloud'**
  String get backgroundStepSamsungNeverSleeping;

  /// No description provided for @backgroundStepXiaomiAutostart.
  ///
  /// In en, this message translates to:
  /// **'Settings › Apps › Manage apps › NeoCloud › turn on Autostart'**
  String get backgroundStepXiaomiAutostart;

  /// No description provided for @backgroundStepXiaomiNoRestrictions.
  ///
  /// In en, this message translates to:
  /// **'Settings › Apps › Manage apps › NeoCloud › Battery saver › No restrictions'**
  String get backgroundStepXiaomiNoRestrictions;

  /// No description provided for @backgroundStepHuaweiAppLaunch.
  ///
  /// In en, this message translates to:
  /// **'Settings › Battery › App launch › NeoCloud › Manage manually, with Auto-launch, Secondary launch and Run in background on'**
  String get backgroundStepHuaweiAppLaunch;

  /// No description provided for @backgroundStepOppoBatteryUsage.
  ///
  /// In en, this message translates to:
  /// **'Settings › Apps › NeoCloud › Battery usage › allow background activity and auto launch'**
  String get backgroundStepOppoBatteryUsage;

  /// No description provided for @backgroundStepOppoDontOptimise.
  ///
  /// In en, this message translates to:
  /// **'Settings › Battery › Battery optimisation › NeoCloud › Don’t optimise'**
  String get backgroundStepOppoDontOptimise;

  /// No description provided for @backgroundStepVivoBackgroundPower.
  ///
  /// In en, this message translates to:
  /// **'Settings › Battery › Background power consumption management › NeoCloud › Allow'**
  String get backgroundStepVivoBackgroundPower;

  /// No description provided for @backgroundStepVivoAutostart.
  ///
  /// In en, this message translates to:
  /// **'i Manager › App manager › Autostart manager › turn on NeoCloud'**
  String get backgroundStepVivoAutostart;

  /// No description provided for @backgroundStepAsusAutostart.
  ///
  /// In en, this message translates to:
  /// **'Mobile Manager › PowerMaster › Auto-start manager › NeoCloud › Allow'**
  String get backgroundStepAsusAutostart;

  /// No description provided for @backgroundStepLockInRecents.
  ///
  /// In en, this message translates to:
  /// **'Open the recent apps and lock NeoCloud there, so clearing them leaves it running'**
  String get backgroundStepLockInRecents;

  /// No description provided for @cameraBackUpNow.
  ///
  /// In en, this message translates to:
  /// **'Back up now'**
  String get cameraBackUpNow;

  /// No description provided for @cameraRetryFailed.
  ///
  /// In en, this message translates to:
  /// **'Retry failed'**
  String get cameraRetryFailed;

  /// No description provided for @cameraExportLog.
  ///
  /// In en, this message translates to:
  /// **'Export upload log'**
  String get cameraExportLog;

  /// No description provided for @cameraLogTitle.
  ///
  /// In en, this message translates to:
  /// **'Upload log'**
  String get cameraLogTitle;

  /// No description provided for @cameraLogEmpty.
  ///
  /// In en, this message translates to:
  /// **'Nothing logged yet.'**
  String get cameraLogEmpty;

  /// No description provided for @cameraListEmpty.
  ///
  /// In en, this message translates to:
  /// **'Nothing here.'**
  String get cameraListEmpty;

  /// No description provided for @cameraReasonWifi.
  ///
  /// In en, this message translates to:
  /// **'waiting for Wi-Fi'**
  String get cameraReasonWifi;

  /// No description provided for @cameraReasonIcloud.
  ///
  /// In en, this message translates to:
  /// **'waiting on iCloud'**
  String get cameraReasonIcloud;

  /// No description provided for @cameraReasonNotAvailable.
  ///
  /// In en, this message translates to:
  /// **'the system couldn’t provide this file'**
  String get cameraReasonNotAvailable;

  /// No description provided for @cameraAttempts.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 attempt} other{{count} attempts}}'**
  String cameraAttempts(int count);

  /// No description provided for @cameraNeverRun.
  ///
  /// In en, this message translates to:
  /// **'Not run yet'**
  String get cameraNeverRun;

  /// No description provided for @cameraUnnamed.
  ///
  /// In en, this message translates to:
  /// **'Not read yet'**
  String get cameraUnnamed;

  /// No description provided for @cameraOptions.
  ///
  /// In en, this message translates to:
  /// **'Options'**
  String get cameraOptions;

  /// No description provided for @mobileNavFiles.
  ///
  /// In en, this message translates to:
  /// **'Files'**
  String get mobileNavFiles;

  /// No description provided for @mobileNavCamera.
  ///
  /// In en, this message translates to:
  /// **'Camera'**
  String get mobileNavCamera;

  /// No description provided for @mobileMore.
  ///
  /// In en, this message translates to:
  /// **'More'**
  String get mobileMore;

  /// No description provided for @mobileRecentServers.
  ///
  /// In en, this message translates to:
  /// **'Recent servers'**
  String get mobileRecentServers;

  /// No description provided for @mobileForgetServer.
  ///
  /// In en, this message translates to:
  /// **'Forget'**
  String get mobileForgetServer;

  /// No description provided for @filesQuickAccess.
  ///
  /// In en, this message translates to:
  /// **'Quick access'**
  String get filesQuickAccess;

  /// No description provided for @done.
  ///
  /// In en, this message translates to:
  /// **'Done'**
  String get done;

  /// No description provided for @deviceRevokeTitle.
  ///
  /// In en, this message translates to:
  /// **'Disconnect {name}?'**
  String deviceRevokeTitle(String name);

  /// No description provided for @deviceRevokeMessage.
  ///
  /// In en, this message translates to:
  /// **'The app on that device is signed out and has to be connected again.'**
  String get deviceRevokeMessage;

  /// No description provided for @qrPairing.
  ///
  /// In en, this message translates to:
  /// **'QR Pairing'**
  String get qrPairing;

  /// No description provided for @pairPhoneDescription.
  ///
  /// In en, this message translates to:
  /// **'Show a QR code that signs the NeoCloud app on a phone in to this account, without typing the server address or a password.'**
  String get pairPhoneDescription;

  /// No description provided for @showQrCode.
  ///
  /// In en, this message translates to:
  /// **'Show QR code'**
  String get showQrCode;

  /// No description provided for @pairPhone.
  ///
  /// In en, this message translates to:
  /// **'Pair a phone'**
  String get pairPhone;

  /// No description provided for @pairTitle.
  ///
  /// In en, this message translates to:
  /// **'Pair a phone'**
  String get pairTitle;

  /// No description provided for @pairInstructions.
  ///
  /// In en, this message translates to:
  /// **'Open the NeoCloud app on your phone, tap “Scan QR code” and point the camera at this code.'**
  String get pairInstructions;

  /// No description provided for @pairQrSemantics.
  ///
  /// In en, this message translates to:
  /// **'QR code to pair a phone'**
  String get pairQrSemantics;

  /// No description provided for @pairWaiting.
  ///
  /// In en, this message translates to:
  /// **'Waiting for the phone'**
  String get pairWaiting;

  /// No description provided for @pairExpiresIn.
  ///
  /// In en, this message translates to:
  /// **'Expires in {time}'**
  String pairExpiresIn(String time);

  /// No description provided for @pairExpired.
  ///
  /// In en, this message translates to:
  /// **'Code expired'**
  String get pairExpired;

  /// No description provided for @pairNewCode.
  ///
  /// In en, this message translates to:
  /// **'New code'**
  String get pairNewCode;

  /// No description provided for @pairPairedWith.
  ///
  /// In en, this message translates to:
  /// **'Paired with {device} ✓'**
  String pairPairedWith(String device);

  /// No description provided for @pairUnknownDevice.
  ///
  /// In en, this message translates to:
  /// **'your phone'**
  String get pairUnknownDevice;

  /// No description provided for @pairCantScan.
  ///
  /// In en, this message translates to:
  /// **'Can\'t scan? Open this link on the phone:'**
  String get pairCantScan;

  /// No description provided for @pairLandingTitle.
  ///
  /// In en, this message translates to:
  /// **'Pair with the NeoCloud app'**
  String get pairLandingTitle;

  /// No description provided for @pairLandingPhoneBody.
  ///
  /// In en, this message translates to:
  /// **'This link signs in the NeoCloud app on this phone. Install the app if you haven\'t, then open the link in it. The code works for five minutes.'**
  String get pairLandingPhoneBody;

  /// No description provided for @pairLandingOpenApp.
  ///
  /// In en, this message translates to:
  /// **'Open in NeoCloud app'**
  String get pairLandingOpenApp;

  /// No description provided for @pairLandingAppStore.
  ///
  /// In en, this message translates to:
  /// **'Get it on the App Store'**
  String get pairLandingAppStore;

  /// No description provided for @pairLandingPlayStore.
  ///
  /// In en, this message translates to:
  /// **'Get it on Google Play'**
  String get pairLandingPlayStore;

  /// No description provided for @pairLandingComputerBody.
  ///
  /// In en, this message translates to:
  /// **'This link is meant for your phone. Open it there, or scan the QR code in Settings › Sign-in & security with the NeoCloud app.'**
  String get pairLandingComputerBody;

  /// No description provided for @pairLandingInvalid.
  ///
  /// In en, this message translates to:
  /// **'This pairing link is incomplete. Show a new code in Settings › Sign-in & security.'**
  String get pairLandingInvalid;

  /// No description provided for @connectOr.
  ///
  /// In en, this message translates to:
  /// **'or'**
  String get connectOr;

  /// No description provided for @connectScanQr.
  ///
  /// In en, this message translates to:
  /// **'Scan QR code'**
  String get connectScanQr;

  /// No description provided for @connectScanQrHint.
  ///
  /// In en, this message translates to:
  /// **'In the web app, open Settings › Sign-in & security › Pair a phone.'**
  String get connectScanQrHint;

  /// No description provided for @scanTitle.
  ///
  /// In en, this message translates to:
  /// **'Scan QR code'**
  String get scanTitle;

  /// No description provided for @scanPrimeTitle.
  ///
  /// In en, this message translates to:
  /// **'Use the camera to pair'**
  String get scanPrimeTitle;

  /// No description provided for @scanPrimeBody.
  ///
  /// In en, this message translates to:
  /// **'NeoCloud needs the camera only to read the pairing code shown in the web app. Nothing is recorded or uploaded.'**
  String get scanPrimeBody;

  /// No description provided for @scanPrimeAllow.
  ///
  /// In en, this message translates to:
  /// **'Allow camera'**
  String get scanPrimeAllow;

  /// No description provided for @scanHint.
  ///
  /// In en, this message translates to:
  /// **'Point the camera at the QR code in Settings › Sign-in & security'**
  String get scanHint;

  /// No description provided for @scanWrongCode.
  ///
  /// In en, this message translates to:
  /// **'That\'s not a NeoCloud pairing code'**
  String get scanWrongCode;

  /// No description provided for @scanTorchOn.
  ///
  /// In en, this message translates to:
  /// **'Turn on the light'**
  String get scanTorchOn;

  /// No description provided for @scanTorchOff.
  ///
  /// In en, this message translates to:
  /// **'Turn off the light'**
  String get scanTorchOff;

  /// No description provided for @scanDeniedTitle.
  ///
  /// In en, this message translates to:
  /// **'Camera access is off'**
  String get scanDeniedTitle;

  /// No description provided for @scanDeniedBodyIos.
  ///
  /// In en, this message translates to:
  /// **'Allow NeoCloud to use the camera in Settings, or connect with the server address instead.'**
  String get scanDeniedBodyIos;

  /// No description provided for @scanDeniedBodyAndroid.
  ///
  /// In en, this message translates to:
  /// **'Allow the camera when asked, or turn it on under Settings › Apps › NeoCloud › Permissions. You can also connect with the server address.'**
  String get scanDeniedBodyAndroid;

  /// No description provided for @scanOpenSettings.
  ///
  /// In en, this message translates to:
  /// **'Open Settings'**
  String get scanOpenSettings;

  /// No description provided for @scanFailedTitle.
  ///
  /// In en, this message translates to:
  /// **'The camera didn\'t start'**
  String get scanFailedTitle;

  /// No description provided for @scanFailedBody.
  ///
  /// In en, this message translates to:
  /// **'Close other apps that use the camera and try again.'**
  String get scanFailedBody;

  /// No description provided for @webDavTitle.
  ///
  /// In en, this message translates to:
  /// **'Connect apps (WebDAV)'**
  String get webDavTitle;

  /// No description provided for @webDavDescription.
  ///
  /// In en, this message translates to:
  /// **'Finder, Windows Explorer, rclone and many other apps open NeoCloud as a network drive. They sign in with an app password, not your account password.'**
  String get webDavDescription;

  /// No description provided for @webDavAddress.
  ///
  /// In en, this message translates to:
  /// **'WebDAV address'**
  String get webDavAddress;

  /// No description provided for @webDavPassword.
  ///
  /// In en, this message translates to:
  /// **'App password'**
  String get webDavPassword;

  /// No description provided for @webDavPasswordHint.
  ///
  /// In en, this message translates to:
  /// **'One per app, so you can disconnect each on its own. App passwords are listed above with a WebDAV tag.'**
  String get webDavPasswordHint;

  /// No description provided for @webDavCreatePassword.
  ///
  /// In en, this message translates to:
  /// **'Create app password'**
  String get webDavCreatePassword;

  /// No description provided for @webDavPasswordName.
  ///
  /// In en, this message translates to:
  /// **'Which app is it for?'**
  String get webDavPasswordName;

  /// No description provided for @webDavPasswordDefaultName.
  ///
  /// In en, this message translates to:
  /// **'WebDAV'**
  String get webDavPasswordDefaultName;

  /// No description provided for @webDavPasswordTitle.
  ///
  /// In en, this message translates to:
  /// **'Your app password'**
  String get webDavPasswordTitle;

  /// No description provided for @webDavPasswordMessage.
  ///
  /// In en, this message translates to:
  /// **'Enter it in the app together with your user name. It is shown only once.'**
  String get webDavPasswordMessage;

  /// No description provided for @webDavRevokeMessage.
  ///
  /// In en, this message translates to:
  /// **'Apps that use this app password can\'t reach your files any more.'**
  String get webDavRevokeMessage;

  /// No description provided for @webDavTag.
  ///
  /// In en, this message translates to:
  /// **'WebDAV'**
  String get webDavTag;

  /// No description provided for @webDavHowTo.
  ///
  /// In en, this message translates to:
  /// **'How to connect common apps'**
  String get webDavHowTo;

  /// No description provided for @webDavHintFinderTitle.
  ///
  /// In en, this message translates to:
  /// **'Finder (macOS)'**
  String get webDavHintFinderTitle;

  /// No description provided for @webDavHintFinder.
  ///
  /// In en, this message translates to:
  /// **'Go › Connect to Server (⌘K), paste the address, then sign in with your user name and an app password.'**
  String get webDavHintFinder;

  /// No description provided for @webDavHintWindowsTitle.
  ///
  /// In en, this message translates to:
  /// **'Windows'**
  String get webDavHintWindowsTitle;

  /// No description provided for @webDavHintWindows.
  ///
  /// In en, this message translates to:
  /// **'In File Explorer, right-click This PC › Map network drive, paste the address and tick “Connect using different credentials”. Needs HTTPS.'**
  String get webDavHintWindows;

  /// No description provided for @webDavHintRcloneTitle.
  ///
  /// In en, this message translates to:
  /// **'rclone'**
  String get webDavHintRcloneTitle;

  /// No description provided for @webDavHintRclone.
  ///
  /// In en, this message translates to:
  /// **'rclone config › New remote › WebDAV, vendor “owncloud”, then the address, user name and app password.'**
  String get webDavHintRclone;

  /// No description provided for @webDavHintObsidianTitle.
  ///
  /// In en, this message translates to:
  /// **'Obsidian'**
  String get webDavHintObsidianTitle;

  /// No description provided for @webDavHintObsidian.
  ///
  /// In en, this message translates to:
  /// **'Install the Remotely Save plugin, choose WebDAV and enter the address, user name and app password.'**
  String get webDavHintObsidian;

  /// No description provided for @webDavHintKeepassTitle.
  ///
  /// In en, this message translates to:
  /// **'KeePassXC and KeePass apps'**
  String get webDavHintKeepassTitle;

  /// No description provided for @webDavHintKeepass.
  ///
  /// In en, this message translates to:
  /// **'Open the database from the mounted drive in KeePassXC; on phones, KeePassDX and Strongbox open WebDAV addresses directly.'**
  String get webDavHintKeepass;

  /// No description provided for @pageImport.
  ///
  /// In en, this message translates to:
  /// **'Import'**
  String get pageImport;

  /// No description provided for @pageImportDescription.
  ///
  /// In en, this message translates to:
  /// **'Move your files over from Nextcloud or another WebDAV server.'**
  String get pageImportDescription;

  /// No description provided for @importNextcloudTitle.
  ///
  /// In en, this message translates to:
  /// **'Move from Nextcloud'**
  String get importNextcloudTitle;

  /// No description provided for @importNextcloudDescription.
  ///
  /// In en, this message translates to:
  /// **'NeoCloud copies your files with their folders and dates. Nothing on the other server is changed.'**
  String get importNextcloudDescription;

  /// No description provided for @importServerUrl.
  ///
  /// In en, this message translates to:
  /// **'Server address'**
  String get importServerUrl;

  /// No description provided for @importAppPassword.
  ///
  /// In en, this message translates to:
  /// **'App password'**
  String get importAppPassword;

  /// No description provided for @importAppPasswordHelpLink.
  ///
  /// In en, this message translates to:
  /// **'How do I create an app password?'**
  String get importAppPasswordHelpLink;

  /// No description provided for @importAppPasswordHelpTitle.
  ///
  /// In en, this message translates to:
  /// **'App password in Nextcloud'**
  String get importAppPasswordHelpTitle;

  /// No description provided for @importAppPasswordHelp.
  ///
  /// In en, this message translates to:
  /// **'In Nextcloud, open your avatar › Personal settings › Security. Under “Devices & sessions”, enter a name such as NeoCloud and click “Create new app password”. Copy the password shown and paste it here. NeoCloud keeps it encrypted and deletes it when the import ends.'**
  String get importAppPasswordHelp;

  /// No description provided for @importAppPasswordDocs.
  ///
  /// In en, this message translates to:
  /// **'Nextcloud help'**
  String get importAppPasswordDocs;

  /// No description provided for @importSource.
  ///
  /// In en, this message translates to:
  /// **'Source folder'**
  String get importSource;

  /// No description provided for @importSourceHint.
  ///
  /// In en, this message translates to:
  /// **'The folder on the other server; / takes everything.'**
  String get importSourceHint;

  /// No description provided for @importTarget.
  ///
  /// In en, this message translates to:
  /// **'Import into'**
  String get importTarget;

  /// No description provided for @importChooseFolder.
  ///
  /// In en, this message translates to:
  /// **'Choose folder'**
  String get importChooseFolder;

  /// No description provided for @importTest.
  ///
  /// In en, this message translates to:
  /// **'Test connection'**
  String get importTest;

  /// No description provided for @importTestFound.
  ///
  /// In en, this message translates to:
  /// **'Connected. {files} items in this folder, {size} in total.'**
  String importTestFound(String files, String size);

  /// No description provided for @importTestFailed.
  ///
  /// In en, this message translates to:
  /// **'The server could not be read. Check the address, user name and app password.'**
  String get importTestFailed;

  /// No description provided for @importQuotaWarning.
  ///
  /// In en, this message translates to:
  /// **'This won\'t fit: you need {size} more space than your storage has left. The import stops when it is full.'**
  String importQuotaWarning(String size);

  /// No description provided for @importStart.
  ///
  /// In en, this message translates to:
  /// **'Start import'**
  String get importStart;

  /// No description provided for @importStarted.
  ///
  /// In en, this message translates to:
  /// **'Import started. You can leave this page; it keeps running.'**
  String get importStarted;

  /// No description provided for @importJobsTitle.
  ///
  /// In en, this message translates to:
  /// **'Imports'**
  String get importJobsTitle;

  /// No description provided for @importJobsEmpty.
  ///
  /// In en, this message translates to:
  /// **'No imports yet.'**
  String get importJobsEmpty;

  /// No description provided for @importInto.
  ///
  /// In en, this message translates to:
  /// **'Into {folder}'**
  String importInto(String folder);

  /// No description provided for @importFilesOf.
  ///
  /// In en, this message translates to:
  /// **'{done} of {total} files'**
  String importFilesOf(String done, String total);

  /// No description provided for @importFilesDone.
  ///
  /// In en, this message translates to:
  /// **'{done} files'**
  String importFilesDone(String done);

  /// No description provided for @importBytesOf.
  ///
  /// In en, this message translates to:
  /// **'{done} of {total}'**
  String importBytesOf(String done, String total);

  /// No description provided for @importSkipped.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 skipped (already there)} other{{count} skipped (already there)}}'**
  String importSkipped(int count);

  /// No description provided for @importFailed.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 failed} other{{count} failed}}'**
  String importFailed(int count);

  /// No description provided for @importShowErrors.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{Show 1 error} other{Show {count} errors}}'**
  String importShowErrors(int count);

  /// No description provided for @importHideErrors.
  ///
  /// In en, this message translates to:
  /// **'Hide errors'**
  String get importHideErrors;

  /// No description provided for @importPause.
  ///
  /// In en, this message translates to:
  /// **'Pause'**
  String get importPause;

  /// No description provided for @importResume.
  ///
  /// In en, this message translates to:
  /// **'Resume'**
  String get importResume;

  /// No description provided for @importCancel.
  ///
  /// In en, this message translates to:
  /// **'Cancel import'**
  String get importCancel;

  /// No description provided for @importCancelTitle.
  ///
  /// In en, this message translates to:
  /// **'Cancel this import?'**
  String get importCancelTitle;

  /// No description provided for @importCancelMessage.
  ///
  /// In en, this message translates to:
  /// **'Files copied so far stay in NeoCloud. You can resume later; files already there are skipped.'**
  String get importCancelMessage;

  /// No description provided for @importOpenFolder.
  ///
  /// In en, this message translates to:
  /// **'Open folder'**
  String get importOpenFolder;

  /// No description provided for @importRemove.
  ///
  /// In en, this message translates to:
  /// **'Remove from list'**
  String get importRemove;

  /// No description provided for @importStatusQueued.
  ///
  /// In en, this message translates to:
  /// **'Queued'**
  String get importStatusQueued;

  /// No description provided for @importStatusScanning.
  ///
  /// In en, this message translates to:
  /// **'Scanning'**
  String get importStatusScanning;

  /// No description provided for @importStatusRunning.
  ///
  /// In en, this message translates to:
  /// **'Importing'**
  String get importStatusRunning;

  /// No description provided for @importStatusPaused.
  ///
  /// In en, this message translates to:
  /// **'Paused'**
  String get importStatusPaused;

  /// No description provided for @importStatusDone.
  ///
  /// In en, this message translates to:
  /// **'Done'**
  String get importStatusDone;

  /// No description provided for @importStatusFailed.
  ///
  /// In en, this message translates to:
  /// **'Failed'**
  String get importStatusFailed;

  /// No description provided for @importStatusCancelled.
  ///
  /// In en, this message translates to:
  /// **'Cancelled'**
  String get importStatusCancelled;

  /// No description provided for @filesWelcomeImportBody.
  ///
  /// In en, this message translates to:
  /// **'Import your files with their folders and dates.'**
  String get filesWelcomeImportBody;

  /// No description provided for @filesWelcomeImportAction.
  ///
  /// In en, this message translates to:
  /// **'Import your files'**
  String get filesWelcomeImportAction;

  /// No description provided for @searchFileContents.
  ///
  /// In en, this message translates to:
  /// **'File contents'**
  String get searchFileContents;

  /// No description provided for @searchFileContentsHint.
  ///
  /// In en, this message translates to:
  /// **'Also find files by the words inside them'**
  String get searchFileContentsHint;

  /// No description provided for @searchMatchName.
  ///
  /// In en, this message translates to:
  /// **'Name'**
  String get searchMatchName;

  /// No description provided for @searchMatchContent.
  ///
  /// In en, this message translates to:
  /// **'Content'**
  String get searchMatchContent;

  /// No description provided for @searchMatchBoth.
  ///
  /// In en, this message translates to:
  /// **'Name + content'**
  String get searchMatchBoth;

  /// No description provided for @searchSettingsTitle.
  ///
  /// In en, this message translates to:
  /// **'Search'**
  String get searchSettingsTitle;

  /// No description provided for @searchSettingsContents.
  ///
  /// In en, this message translates to:
  /// **'Search inside my files'**
  String get searchSettingsContents;

  /// No description provided for @searchSettingsContentsHint.
  ///
  /// In en, this message translates to:
  /// **'The server indexes the text of your documents so search finds the words in them. Turn this off to keep your files out of the index.'**
  String get searchSettingsContentsHint;

  /// No description provided for @searchSettingsContentsOff.
  ///
  /// In en, this message translates to:
  /// **'Full-text search is off on this server. If an admin turns it on, this decides whether your files are indexed.'**
  String get searchSettingsContentsOff;

  /// No description provided for @adminSearchTitle.
  ///
  /// In en, this message translates to:
  /// **'Full-text search'**
  String get adminSearchTitle;

  /// No description provided for @adminSearchDescription.
  ///
  /// In en, this message translates to:
  /// **'Lets people find files by the words inside them: text, PDF and Office documents. Vault files are never indexed.'**
  String get adminSearchDescription;

  /// No description provided for @adminSearchToggle.
  ///
  /// In en, this message translates to:
  /// **'Full-text search'**
  String get adminSearchToggle;

  /// No description provided for @adminSearchPrivacy.
  ///
  /// In en, this message translates to:
  /// **'Trade-off: the index stores which words each file contains. The text itself is encrypted, but anyone holding the database can tell whether a file contains a given word. Each user can opt out in their settings.'**
  String get adminSearchPrivacy;

  /// No description provided for @adminSearchState.
  ///
  /// In en, this message translates to:
  /// **'Index'**
  String get adminSearchState;

  /// No description provided for @adminSearchOff.
  ///
  /// In en, this message translates to:
  /// **'Off'**
  String get adminSearchOff;

  /// No description provided for @adminSearchIndexing.
  ///
  /// In en, this message translates to:
  /// **'Indexing'**
  String get adminSearchIndexing;

  /// No description provided for @adminSearchUpToDate.
  ///
  /// In en, this message translates to:
  /// **'Up to date'**
  String get adminSearchUpToDate;

  /// No description provided for @adminSearchIndexed.
  ///
  /// In en, this message translates to:
  /// **'Indexed files'**
  String get adminSearchIndexed;

  /// No description provided for @adminSearchPending.
  ///
  /// In en, this message translates to:
  /// **'Waiting'**
  String get adminSearchPending;

  /// No description provided for @adminSearchFailed.
  ///
  /// In en, this message translates to:
  /// **'Failed'**
  String get adminSearchFailed;

  /// No description provided for @adminSearchFailedHint.
  ///
  /// In en, this message translates to:
  /// **'Damaged or password-protected files can\'t be read.'**
  String get adminSearchFailedHint;

  /// No description provided for @adminSearchSize.
  ///
  /// In en, this message translates to:
  /// **'Index size'**
  String get adminSearchSize;

  /// No description provided for @adminSearchPdf.
  ///
  /// In en, this message translates to:
  /// **'PDF text'**
  String get adminSearchPdf;

  /// No description provided for @adminSearchPdfFallback.
  ///
  /// In en, this message translates to:
  /// **'Install poppler-utils for better PDF text; the built-in reader is used until then.'**
  String get adminSearchPdfFallback;

  /// No description provided for @adminSearchOffice.
  ///
  /// In en, this message translates to:
  /// **'Office documents'**
  String get adminSearchOffice;

  /// No description provided for @adminSearchToolPdftotext.
  ///
  /// In en, this message translates to:
  /// **'pdftotext'**
  String get adminSearchToolPdftotext;

  /// No description provided for @adminSearchToolAvailable.
  ///
  /// In en, this message translates to:
  /// **'Available'**
  String get adminSearchToolAvailable;

  /// No description provided for @adminSearchToolMissing.
  ///
  /// In en, this message translates to:
  /// **'Not installed'**
  String get adminSearchToolMissing;

  /// No description provided for @adminSearchRebuild.
  ///
  /// In en, this message translates to:
  /// **'Rebuild index'**
  String get adminSearchRebuild;

  /// No description provided for @adminSearchRebuildHint.
  ///
  /// In en, this message translates to:
  /// **'Reads every file again, in the background.'**
  String get adminSearchRebuildHint;

  /// No description provided for @adminSearchRebuildStarted.
  ///
  /// In en, this message translates to:
  /// **'The index is being rebuilt in the background.'**
  String get adminSearchRebuildStarted;

  /// No description provided for @importTargetHint.
  ///
  /// In en, this message translates to:
  /// **'Without a folder, files go into a new “Nextcloud” folder in Home.'**
  String get importTargetHint;

  /// No description provided for @importHostBlocked.
  ///
  /// In en, this message translates to:
  /// **'Only an admin can import from a server on your local network.'**
  String get importHostBlocked;

  /// No description provided for @filesWelcomeDontShowAgain.
  ///
  /// In en, this message translates to:
  /// **'Don’t show again'**
  String get filesWelcomeDontShowAgain;

  /// No description provided for @filesQuickAccessShow.
  ///
  /// In en, this message translates to:
  /// **'Show quick access'**
  String get filesQuickAccessShow;

  /// No description provided for @filesQuickAccessHide.
  ///
  /// In en, this message translates to:
  /// **'Hide quick access'**
  String get filesQuickAccessHide;

  /// No description provided for @adminSafetyAllDone.
  ///
  /// In en, this message translates to:
  /// **'All done'**
  String get adminSafetyAllDone;

  /// No description provided for @adminSafetyProgress.
  ///
  /// In en, this message translates to:
  /// **'{done} of {total} done'**
  String adminSafetyProgress(int done, int total);

  /// No description provided for @adminSafetyExpand.
  ///
  /// In en, this message translates to:
  /// **'Show checklist'**
  String get adminSafetyExpand;

  /// No description provided for @adminSafetyCollapse.
  ///
  /// In en, this message translates to:
  /// **'Hide checklist'**
  String get adminSafetyCollapse;

  /// No description provided for @authSignInWithSecurityKey.
  ///
  /// In en, this message translates to:
  /// **'Sign in with security key'**
  String get authSignInWithSecurityKey;

  /// No description provided for @authOrContinueWith.
  ///
  /// In en, this message translates to:
  /// **'or continue with'**
  String get authOrContinueWith;

  /// No description provided for @passkeyWebOnly.
  ///
  /// In en, this message translates to:
  /// **'Security keys are only available in the NeoCloud web app.'**
  String get passkeyWebOnly;

  /// No description provided for @passkeyNoneProvided.
  ///
  /// In en, this message translates to:
  /// **'No security key was provided.'**
  String get passkeyNoneProvided;

  /// No description provided for @passkeyPromptDismissed.
  ///
  /// In en, this message translates to:
  /// **'Security key prompt was dismissed.'**
  String get passkeyPromptDismissed;

  /// No description provided for @passkeyAlreadyRegistered.
  ///
  /// In en, this message translates to:
  /// **'That security key is already registered on this account.'**
  String get passkeyAlreadyRegistered;

  /// No description provided for @passkeyCouldNotBeUsed.
  ///
  /// In en, this message translates to:
  /// **'The security key could not be used.'**
  String get passkeyCouldNotBeUsed;

  /// No description provided for @passkeySignInFailed.
  ///
  /// In en, this message translates to:
  /// **'Security key sign-in could not be started.'**
  String get passkeySignInFailed;

  /// No description provided for @passkeyRegistrationFailed.
  ///
  /// In en, this message translates to:
  /// **'The security key registration could not be started.'**
  String get passkeyRegistrationFailed;

  /// No description provided for @passkeysTitle.
  ///
  /// In en, this message translates to:
  /// **'Security keys'**
  String get passkeysTitle;

  /// No description provided for @passkeysDescription.
  ///
  /// In en, this message translates to:
  /// **'Sign in with a hardware key or passkey instead of your password. A key that asks for a PIN or fingerprint also replaces your two-factor code.'**
  String get passkeysDescription;

  /// No description provided for @passkeysNone.
  ///
  /// In en, this message translates to:
  /// **'None'**
  String get passkeysNone;

  /// No description provided for @passkeysRegistered.
  ///
  /// In en, this message translates to:
  /// **'{count} registered'**
  String passkeysRegistered(int count);

  /// No description provided for @passkeyAdd.
  ///
  /// In en, this message translates to:
  /// **'Add security key'**
  String get passkeyAdd;

  /// No description provided for @passkeyNameOptional.
  ///
  /// In en, this message translates to:
  /// **'Name (optional)'**
  String get passkeyNameOptional;

  /// No description provided for @passkeyNameHint.
  ///
  /// In en, this message translates to:
  /// **'YubiKey, MacBook Touch ID, …'**
  String get passkeyNameHint;

  /// No description provided for @passkeyDefaultName.
  ///
  /// In en, this message translates to:
  /// **'Security key {number}'**
  String passkeyDefaultName(int number);

  /// No description provided for @passkeyRenameTitle.
  ///
  /// In en, this message translates to:
  /// **'Rename security key'**
  String get passkeyRenameTitle;

  /// No description provided for @passkeyName.
  ///
  /// In en, this message translates to:
  /// **'Name'**
  String get passkeyName;

  /// No description provided for @passkeyRemove.
  ///
  /// In en, this message translates to:
  /// **'Remove'**
  String get passkeyRemove;

  /// No description provided for @passkeyAddedLastUsed.
  ///
  /// In en, this message translates to:
  /// **'Added {added} · Last used {lastUsed}'**
  String passkeyAddedLastUsed(String added, String lastUsed);

  /// No description provided for @passkeyNotRecorded.
  ///
  /// In en, this message translates to:
  /// **'Not recorded'**
  String get passkeyNotRecorded;

  /// No description provided for @passkeyNeverUsed.
  ///
  /// In en, this message translates to:
  /// **'Never used'**
  String get passkeyNeverUsed;

  /// No description provided for @passkeyUnsupported.
  ///
  /// In en, this message translates to:
  /// **'This device cannot register security keys. Open NeoCloud in a browser over HTTPS to add one.'**
  String get passkeyUnsupported;

  /// No description provided for @navSharedWithMe.
  ///
  /// In en, this message translates to:
  /// **'Shared with me'**
  String get navSharedWithMe;

  /// No description provided for @sharedWithMeSubtitle.
  ///
  /// In en, this message translates to:
  /// **'Files and folders other people on this server shared with you.'**
  String get sharedWithMeSubtitle;

  /// No description provided for @sharedWithMeEmpty.
  ///
  /// In en, this message translates to:
  /// **'Nothing shared with you yet'**
  String get sharedWithMeEmpty;

  /// No description provided for @sharedWithMeEmptySubtitle.
  ///
  /// In en, this message translates to:
  /// **'When someone shares a file or folder with you, it shows up here.'**
  String get sharedWithMeEmptySubtitle;

  /// No description provided for @sharedBy.
  ///
  /// In en, this message translates to:
  /// **'Shared by {owner}'**
  String sharedBy(String owner);

  /// No description provided for @sharedByCanEdit.
  ///
  /// In en, this message translates to:
  /// **'Shared by {owner} · you can edit'**
  String sharedByCanEdit(String owner);

  /// No description provided for @sharedByReadOnly.
  ///
  /// In en, this message translates to:
  /// **'Shared by {owner} · view only'**
  String sharedByReadOnly(String owner);

  /// No description provided for @sharedWithPeople.
  ///
  /// In en, this message translates to:
  /// **'Shared with people'**
  String get sharedWithPeople;

  /// No description provided for @sharedLeave.
  ///
  /// In en, this message translates to:
  /// **'Leave share'**
  String get sharedLeave;

  /// No description provided for @sharedLeaveConfirm.
  ///
  /// In en, this message translates to:
  /// **'You will no longer see “{name}”. Only {owner} can share it with you again.'**
  String sharedLeaveConfirm(String name, String owner);

  /// No description provided for @sharedSaveToMyFiles.
  ///
  /// In en, this message translates to:
  /// **'Save to my files'**
  String get sharedSaveToMyFiles;

  /// No description provided for @sharedSavedToMyFiles.
  ///
  /// In en, this message translates to:
  /// **'“{name}” copied to your files.'**
  String sharedSavedToMyFiles(String name);

  /// No description provided for @sharedReadOnlyError.
  ///
  /// In en, this message translates to:
  /// **'This folder is shared with you to view only.'**
  String get sharedReadOnlyError;

  /// No description provided for @sharedBoundaryError.
  ///
  /// In en, this message translates to:
  /// **'Items can’t be moved into or out of a shared folder. Copy them instead.'**
  String get sharedBoundaryError;

  /// No description provided for @sharedRootError.
  ///
  /// In en, this message translates to:
  /// **'The shared folder itself belongs to its owner.'**
  String get sharedRootError;

  /// No description provided for @sharedZipMixedError.
  ///
  /// In en, this message translates to:
  /// **'Download your own files and shared files separately.'**
  String get sharedZipMixedError;

  /// No description provided for @sharePeopleTitle.
  ///
  /// In en, this message translates to:
  /// **'Share with people'**
  String get sharePeopleTitle;

  /// No description provided for @sharePeopleHelp.
  ///
  /// In en, this message translates to:
  /// **'Enter the exact email address or username of someone with an account on this server.'**
  String get sharePeopleHelp;

  /// No description provided for @sharePublicLinkTitle.
  ///
  /// In en, this message translates to:
  /// **'Public link'**
  String get sharePublicLinkTitle;

  /// No description provided for @memberRecipientHint.
  ///
  /// In en, this message translates to:
  /// **'Email address or username'**
  String get memberRecipientHint;

  /// No description provided for @memberAdd.
  ///
  /// In en, this message translates to:
  /// **'Add'**
  String get memberAdd;

  /// No description provided for @memberCanView.
  ///
  /// In en, this message translates to:
  /// **'Can view'**
  String get memberCanView;

  /// No description provided for @memberCanEdit.
  ///
  /// In en, this message translates to:
  /// **'Can edit'**
  String get memberCanEdit;

  /// No description provided for @memberChange.
  ///
  /// In en, this message translates to:
  /// **'Change access'**
  String get memberChange;

  /// No description provided for @memberRemove.
  ///
  /// In en, this message translates to:
  /// **'Remove access'**
  String get memberRemove;

  /// No description provided for @memberExpired.
  ///
  /// In en, this message translates to:
  /// **'Expired'**
  String get memberExpired;

  /// No description provided for @membersEmpty.
  ///
  /// In en, this message translates to:
  /// **'Not shared with anyone yet.'**
  String get membersEmpty;

  /// No description provided for @memberNotFound.
  ///
  /// In en, this message translates to:
  /// **'No account found that this can be shared with. Check the exact email address or username.'**
  String get memberNotFound;

  /// No description provided for @memberAlreadyAdded.
  ///
  /// In en, this message translates to:
  /// **'Already shared with this person.'**
  String get memberAlreadyAdded;
}

class _AppL10nDelegate extends LocalizationsDelegate<AppL10n> {
  const _AppL10nDelegate();

  @override
  Future<AppL10n> load(Locale locale) {
    return SynchronousFuture<AppL10n>(lookupAppL10n(locale));
  }

  @override
  bool isSupported(Locale locale) =>
      <String>['de', 'en'].contains(locale.languageCode);

  @override
  bool shouldReload(_AppL10nDelegate old) => false;
}

AppL10n lookupAppL10n(Locale locale) {
  // Lookup logic when only language code is specified.
  switch (locale.languageCode) {
    case 'de':
      return AppL10nDe();
    case 'en':
      return AppL10nEn();
  }

  throw FlutterError(
    'AppL10n.delegate failed to load unsupported locale "$locale". This is likely '
    'an issue with the localizations generation tool. Please file an issue '
    'on GitHub with a reproducible sample app and the gen-l10n configuration '
    'that was used.',
  );
}
