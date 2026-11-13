// ignore: unused_import
import 'package:intl/intl.dart' as intl;
import 'app_l10n.dart';

// ignore_for_file: type=lint

/// The translations for German (`de`).
class AppL10nDe extends AppL10n {
  AppL10nDe([String locale = 'de']) : super(locale);

  @override
  String get appName => 'NeoCloud';

  @override
  String get appEyebrow => 'PRIVATE CLOUD';

  @override
  String get cancel => 'Abbrechen';

  @override
  String get save => 'Speichern';

  @override
  String get saving => 'Speichert…';

  @override
  String get saved => 'Gespeichert';

  @override
  String get close => 'Schließen';

  @override
  String get dismiss => 'Ausblenden';

  @override
  String get retry => 'Erneut versuchen';

  @override
  String get refresh => 'Aktualisieren';

  @override
  String get create => 'Erstellen';

  @override
  String get delete => 'Löschen';

  @override
  String get clear => 'Leeren';

  @override
  String get copy => 'Kopieren';

  @override
  String get copied => 'Kopiert';

  @override
  String get continueLabel => 'Weiter';

  @override
  String get genericError =>
      'Etwas ist schiefgelaufen. Bitte versuche es erneut.';

  @override
  String get connectionError =>
      'NeoCloud ist nicht erreichbar. Prüfe deine Verbindung und versuch es noch einmal.';

  @override
  String get signIn => 'Anmelden';

  @override
  String get authSignInTitle => 'Willkommen zurück';

  @override
  String get authSignInSubtitle => 'Melde dich an, um fortzufahren.';

  @override
  String get authCreateAccount => 'Konto erstellen';

  @override
  String get createTheFirstAccount => 'Erstes Konto anlegen';

  @override
  String get firstAccountSubtitle =>
      'Du bist als Erstes hier. Mit diesem Konto richtest du NeoCloud ein und lädst andere ein.';

  @override
  String get createAnotherAccount => 'Lege ein weiteres NeoCloud-Konto an.';

  @override
  String get usernameOrEmail => 'Benutzername oder E-Mail';

  @override
  String get username => 'Benutzername';

  @override
  String get email => 'E-Mail';

  @override
  String get password => 'Passwort';

  @override
  String get confirmPassword => 'Passwort bestätigen';

  @override
  String get forgotPasswordAskAdmin =>
      'NeoCloud verschickt noch keine E-Mails. Bitte die Person, die deine NeoCloud betreibt, dein Passwort zurückzusetzen.';

  @override
  String get alreadyHaveAnAccount => 'Schon ein Konto? Anmelden';

  @override
  String get needANewAccount => 'Noch kein Konto? Registrieren';

  @override
  String get verification => 'Bestätigung';

  @override
  String get enter2faCode => '2FA-Code eingeben';

  @override
  String get openYourAuthenticator =>
      'Öffne deine Authenticator-App und gib den 6-stelligen Code ein – oder einen Wiederherstellungscode.';

  @override
  String get twoFactorOrRecoveryCode => '2FA- oder Wiederherstellungscode';

  @override
  String get verify => 'Bestätigen';

  @override
  String get backToSignIn => 'Zurück zur Anmeldung';

  @override
  String get enterAUsername => 'Gib einen Benutzernamen ein.';

  @override
  String get usernameTooShort => 'Mindestens 3 Zeichen.';

  @override
  String get enterAValidEmail => 'Gib eine gültige E-Mail-Adresse ein.';

  @override
  String get enterAPassword => 'Gib ein Passwort ein.';

  @override
  String get passwordsDoNotMatch => 'Die Passwörter stimmen nicht überein.';

  @override
  String get enterYourUsernameOrEmail =>
      'Gib deinen Benutzernamen oder deine E-Mail ein.';

  @override
  String get enterYour2faCode =>
      'Gib deinen 2FA- oder Wiederherstellungscode ein.';

  @override
  String get showPassword => 'Passwort anzeigen';

  @override
  String get hidePassword => 'Passwort verbergen';

  @override
  String passwordStrength(String label) {
    return 'Passwortstärke: $label';
  }

  @override
  String get strengthEmpty => 'leer';

  @override
  String get strengthRejected => 'zu schwach, wird abgelehnt';

  @override
  String get strengthGood => 'gut';

  @override
  String get strengthStrong => 'stark';

  @override
  String get passwordHintEmpty =>
      'Verwende mindestens 8 Zeichen. Längere Passphrasen eignen sich gut.';

  @override
  String get passwordHintLength => 'Verwende mindestens 8 Zeichen.';

  @override
  String get passwordHintUserInfo =>
      'Verwende nicht deinen Benutzernamen oder deine E-Mail.';

  @override
  String get passwordHintPattern =>
      'Vermeide wiederholte Zeichen und offensichtliche Folgen.';

  @override
  String get passwordHintMore =>
      'Füge mehr Länge oder einen weiteren Zeichentyp hinzu.';

  @override
  String get passwordHintStrong => 'Starkes Passwort.';

  @override
  String get passwordHintGood =>
      'Gutes Passwort. Etwas mehr Länge macht es stärker.';

  @override
  String get navFiles => 'Dateien';

  @override
  String get navAllFiles => 'Alle Dateien';

  @override
  String get navRecent => 'Zuletzt';

  @override
  String get navTrash => 'Papierkorb';

  @override
  String get navAdmin => 'Admin';

  @override
  String get navSettings => 'Einstellungen';

  @override
  String storageUsedOf(String used, String total) {
    return '$used von $total';
  }

  @override
  String storageUsedUnlimited(String used) {
    return '$used belegt';
  }

  @override
  String get storageLabel => 'Speicher';

  @override
  String get unlimited => 'Unbegrenzt';

  @override
  String get filesTitle => 'Dateien';

  @override
  String get filesSubtitle => 'Alles hier liegt verschlüsselt.';

  @override
  String get upload => 'Hochladen';

  @override
  String get newFolder => 'Neuer Ordner';

  @override
  String get folderName => 'Ordnername';

  @override
  String get rename => 'Umbenennen';

  @override
  String get newName => 'Neuer Name';

  @override
  String get moveTo => 'Verschieben nach…';

  @override
  String get moveHere => 'Hierher verschieben';

  @override
  String get makeCopy => 'Kopie erstellen';

  @override
  String get download => 'Herunterladen';

  @override
  String get downloadZip => 'Als ZIP herunterladen';

  @override
  String get versions => 'Versionen';

  @override
  String get open => 'Öffnen';

  @override
  String get moveToTrash => 'In den Papierkorb';

  @override
  String get home => 'Start';

  @override
  String get columnName => 'Name';

  @override
  String get columnSize => 'Größe';

  @override
  String get columnModified => 'Geändert';

  @override
  String itemsCount(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count Einträge',
      one: '1 Eintrag',
      zero: 'Keine Einträge',
    );
    return '$_temp0';
  }

  @override
  String selectedCount(int count) {
    return '$count ausgewählt';
  }

  @override
  String get emptyFolderTitle => 'Dieser Ordner ist leer';

  @override
  String get emptyFolderSubtitle =>
      'Zieh Dateien auf diese Seite oder nutze „Hochladen“.';

  @override
  String dropToUpload(String folder) {
    return 'Loslassen, um nach „$folder“ hochzuladen';
  }

  @override
  String get searchFiles => 'Dateien und Ordner durchsuchen';

  @override
  String noSearchResults(String query) {
    return 'Nichts passt zu „$query“.';
  }

  @override
  String get trashConfirmTitle => 'In den Papierkorb verschieben?';

  @override
  String trashConfirmMessage(int count, String name) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other:
          '$count Einträge wandern in den Papierkorb. Dort kannst du sie wiederherstellen.',
      one:
          '„$name“ wandert in den Papierkorb. Dort kannst du es wiederherstellen.',
    );
    return '$_temp0';
  }

  @override
  String nameExistsTitle(String name) {
    return '„$name“ gibt es schon';
  }

  @override
  String get nameExistsMessage =>
      'In diesem Ordner gibt es schon eine Datei mit diesem Namen.';

  @override
  String get replace => 'Ersetzen';

  @override
  String get keepBoth => 'Beide behalten';

  @override
  String get skip => 'Überspringen';

  @override
  String get applyToAll => 'Für alle weiteren Dateien übernehmen';

  @override
  String get uploadsTitle => 'Uploads';

  @override
  String uploadsActive(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count Dateien werden hochgeladen',
      one: '1 Datei wird hochgeladen',
    );
    return '$_temp0';
  }

  @override
  String get uploadsDone => 'Uploads abgeschlossen';

  @override
  String get uploadWaiting => 'Wartet';

  @override
  String get uploadDone => 'Fertig';

  @override
  String get uploadSkipped => 'Übersprungen';

  @override
  String get uploadFailed => 'Fehlgeschlagen';

  @override
  String get uploadCancel => 'Upload abbrechen';

  @override
  String get uploadClearFinished => 'Erledigte entfernen';

  @override
  String get recentTitle => 'Zuletzt';

  @override
  String get recentSubtitle => 'Zuletzt geänderte oder hinzugefügte Dateien.';

  @override
  String get recentEmpty => 'Noch keine Dateien';

  @override
  String get recentEmptySubtitle => 'Lade etwas hoch, dann erscheint es hier.';

  @override
  String get trashTitle => 'Papierkorb';

  @override
  String get trashSubtitle =>
      'Gelöschte Einträge warten hier, bevor sie endgültig entfernt werden.';

  @override
  String trashRetention(int days) {
    return 'Einträge werden nach $days Tagen automatisch entfernt.';
  }

  @override
  String get restore => 'Wiederherstellen';

  @override
  String get deleteForever => 'Endgültig löschen';

  @override
  String get emptyTrash => 'Papierkorb leeren';

  @override
  String get emptyTrashConfirm =>
      'Alles im Papierkorb wird endgültig gelöscht. Das lässt sich nicht rückgängig machen.';

  @override
  String deleteForeverConfirm(String name) {
    return '„$name“ wird endgültig gelöscht. Das lässt sich nicht rückgängig machen.';
  }

  @override
  String get trashIsEmpty => 'Der Papierkorb ist leer';

  @override
  String get trashIsEmptySubtitle =>
      'Gelöschte Dateien und Ordner erscheinen hier.';

  @override
  String deletedOn(String date, String path) {
    return 'Gelöscht $date · aus $path';
  }

  @override
  String versionsTitle(String name) {
    return 'Versionen von „$name“';
  }

  @override
  String get versionCurrent => 'Aktuelle Version';

  @override
  String versionNumber(int number) {
    return 'Version $number';
  }

  @override
  String get noVersions =>
      'Noch keine älteren Versionen. Beim Ersetzen bleibt die alte hier erhalten.';

  @override
  String get noPreview => 'Für diesen Dateityp gibt es noch keine Vorschau.';

  @override
  String get previewTooLarge => 'Diese Datei ist zu groß für die Vorschau.';

  @override
  String get details => 'Details';

  @override
  String get settingsTitle => 'Einstellungen';

  @override
  String get settingsScopeAccount => 'Du';

  @override
  String get settingsScopeApp => 'Diese App';

  @override
  String get settingsAccountHint => 'Wird in deinem Konto gespeichert';

  @override
  String get settingsDeviceHint => 'Gilt für dieses Gerät';

  @override
  String get settingsSearchHint => 'Einstellungen suchen';

  @override
  String get settingsTwoFactorOn => '2FA an';

  @override
  String get settingsTwoFactorOff => '2FA aus';

  @override
  String get settingsSaveHint => 'Änderungen werden automatisch gespeichert';

  @override
  String get settingsSaveFailed =>
      'Die letzte Änderung wurde nicht gespeichert';

  @override
  String get pageProfile => 'Profil';

  @override
  String get pageProfileDescription => 'Dein Name und deine Anmeldedaten.';

  @override
  String get pageSecurity => 'Anmeldung & Sicherheit';

  @override
  String get pageSecurityDescription =>
      'Passwort, Zwei-Faktor-Authentifizierung und aktive Sitzungen.';

  @override
  String get pageDevices => 'Geräte';

  @override
  String get pageDevicesDescription =>
      'Desktop- und Mobil-Apps, die mit deinem Konto verbunden sind.';

  @override
  String get pageStorage => 'Speicher';

  @override
  String get pageStorageDescription => 'Wie viel Platz du belegst und wofür.';

  @override
  String get pageGeneral => 'Allgemein';

  @override
  String get pageGeneralDescription => 'Darstellung, Sprache und Dateiansicht.';

  @override
  String get displayName => 'Anzeigename';

  @override
  String get displayNameHint => 'Wird statt deines Benutzernamens angezeigt.';

  @override
  String get accountUsername => 'Benutzername';

  @override
  String get accountEmail => 'E-Mail';

  @override
  String get accountRole => 'Rolle';

  @override
  String get roleAdmin => 'Administrator';

  @override
  String get roleUser => 'Benutzer';

  @override
  String get signOut => 'Abmelden';

  @override
  String get signOutDescription => 'Diese Sitzung auf diesem Gerät beenden.';

  @override
  String get disconnectDescription =>
      'Dieses Handy von NeoCloud abmelden; es verliert seinen Zugang und der Kamera-Upload wird ausgeschaltet.';

  @override
  String get changePassword => 'Passwort ändern';

  @override
  String get currentPassword => 'Aktuelles Passwort';

  @override
  String get newPassword => 'Neues Passwort';

  @override
  String get passwordChanged => 'Passwort geändert.';

  @override
  String get twoFactorTitle => 'Zwei-Faktor-Authentifizierung';

  @override
  String twoFactorOn(int count) {
    return 'An · noch $count Wiederherstellungscodes';
  }

  @override
  String get twoFactorOff =>
      'Aus. Verlange bei jeder Anmeldung einen Code aus einer Authenticator-App.';

  @override
  String get twoFactorEnable => 'Einrichten';

  @override
  String get twoFactorDisable => 'Ausschalten';

  @override
  String get twoFactorSetupStep =>
      'Scanne diesen QR-Code mit deiner Authenticator-App oder gib den Schlüssel manuell ein. Gib anschließend den angezeigten Code ein.';

  @override
  String get twoFactorCode => '6-stelliger Code';

  @override
  String get recoveryCodesTitle => 'Wiederherstellungscodes sichern';

  @override
  String get recoveryCodesMessage =>
      'Jeder Code meldet dich einmal an, falls du dein Handy verlierst. Sie werden nicht noch einmal angezeigt.';

  @override
  String get sessionsTitle => 'Aktive Sitzungen';

  @override
  String get sessionCurrent => 'Diese Sitzung';

  @override
  String get deviceCurrent => 'Dieses Gerät';

  @override
  String sessionLastSeen(String date) {
    return 'Zuletzt aktiv $date';
  }

  @override
  String get sessionRevoke => 'Abmelden';

  @override
  String get devicesEmpty => 'Noch keine Geräte verbunden.';

  @override
  String get deviceConnect => 'Gerät verbinden';

  @override
  String get deviceName => 'Gerätename';

  @override
  String get deviceTokenTitle => 'Geräte-Token';

  @override
  String get deviceTokenMessage =>
      'Füge diesen Token in die NeoCloud-App auf dem Gerät ein. Er wird nur einmal angezeigt.';

  @override
  String get deviceRevoke => 'Trennen';

  @override
  String deviceLastSeen(String date) {
    return 'Zuletzt gesehen $date';
  }

  @override
  String get deviceNeverSeen => 'Noch nicht verwendet';

  @override
  String get storageUsed => 'Belegt';

  @override
  String get storageQuota => 'Kontingent';

  @override
  String get storageCurrentFiles => 'Aktuelle Dateien';

  @override
  String get storageVersions => 'Ältere Versionen';

  @override
  String get storageTrash => 'Papierkorb';

  @override
  String get theme => 'Design';

  @override
  String get themeSystem => 'System';

  @override
  String get themeLight => 'Hell';

  @override
  String get themeDark => 'Dunkel';

  @override
  String get language => 'Sprache';

  @override
  String get fileView => 'Dateiansicht';

  @override
  String get fileViewList => 'Liste';

  @override
  String get fileViewGrid => 'Raster';

  @override
  String get uploadConflict => 'Wenn eine Datei schon existiert';

  @override
  String get uploadConflictAsk => 'Fragen';

  @override
  String get adminTitle => 'Admin';

  @override
  String get adminSubtitle => 'Konten, Speichergrenzen und Serverbetrieb.';

  @override
  String get adminTabUsers => 'Benutzer';

  @override
  String get adminTabStorage => 'Speicher';

  @override
  String get adminTabAccess => 'Zugang';

  @override
  String get adminTabServer => 'Server';

  @override
  String get adminUsersTitle => 'Konten';

  @override
  String adminUsersCount(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count Konten',
      one: '1 Konto',
    );
    return '$_temp0';
  }

  @override
  String adminUserYou(String name) {
    return '$name (du)';
  }

  @override
  String get adminSearchHint => 'Admin-Einstellungen durchsuchen';

  @override
  String get adminSearchUsers => 'Nach Name oder E-Mail suchen';

  @override
  String adminQuotaDefault(String size) {
    return 'Standard ($size)';
  }

  @override
  String get adminSetQuota => 'Speichergrenze';

  @override
  String get adminQuotaModeDefault => 'Serverstandard';

  @override
  String get adminQuotaModeCustom => 'Eigener Wert';

  @override
  String get adminQuotaModeUnlimited => 'Unbegrenzt';

  @override
  String get adminQuotaCustomHint => 'z. B. 50GB oder 500MB';

  @override
  String get adminMakeAdmin => 'Zum Admin machen';

  @override
  String get adminRemoveAdmin => 'Admin entfernen';

  @override
  String get adminResetPassword => 'Passwort zurücksetzen';

  @override
  String adminResetPasswordMessage(String name) {
    return 'Setzt ein neues Passwort für $name und meldet das Konto überall ab.';
  }

  @override
  String get adminDeleteUser => 'Konto löschen';

  @override
  String adminDeleteUserMessage(String name) {
    return '$name und alle Dateien werden endgültig gelöscht.';
  }

  @override
  String get adminUserDisabled => 'Deaktiviert';

  @override
  String get adminDisableUser => 'Deaktivieren';

  @override
  String get adminEnableUser => 'Aktivieren';

  @override
  String get adminSignOutEverywhere => 'Überall abmelden';

  @override
  String adminSignOutEverywhereTitle(String name) {
    return '$name überall abmelden?';
  }

  @override
  String get adminSignOutEverywhereMessage =>
      'Jede aktive Sitzung für dieses Konto wird beendet, seine Geräte und WebDAV-App-Passwörter funktionieren nicht mehr. Die Anmeldung ist sofort wieder möglich.';

  @override
  String adminSignedOutEverywhere(String name) {
    return '$name wurde aus allen Sitzungen abgemeldet.';
  }

  @override
  String get accountDataTitle => 'Deine Daten';

  @override
  String get accountExportTitle => 'Kontodaten exportieren';

  @override
  String get accountExportHint =>
      'Profil, Sitzungen, Geräte, Links, Freigaben und die Liste deiner Dateien und Ordner als eine Datei. Dateiinhalte sind nicht enthalten: Lade deine Dateien oder einen Ordner als ZIP herunter, um sie zu behalten. Passwörter und Schlüssel sind nicht enthalten.';

  @override
  String get accountExport => 'Exportieren';

  @override
  String get accountExported => 'Heruntergeladen.';

  @override
  String get accountDeleteTitle => 'Konto löschen';

  @override
  String get accountDeleteHint =>
      'Löscht dein Konto mit allen Dateien, Versionen, Links, Freigaben und Geräten. Wer Zugriff auf deine Freigaben hatte, verliert ihn. Das lässt sich nicht rückgängig machen.';

  @override
  String get accountDelete => 'Konto löschen';

  @override
  String get accountDeleteConfirm =>
      'Alles in diesem Konto wird endgültig gelöscht. Exportiere vorher deine Daten, wenn du sie behalten willst.';

  @override
  String accountDeleteTypeUsername(String name) {
    return 'Gib zur Bestätigung deinen Benutzernamen $name ein.';
  }

  @override
  String get accountDeleted => 'Dein Konto wurde gelöscht.';

  @override
  String get accountConfirmPassword =>
      'Gib dein Passwort ein, um fortzufahren.';

  @override
  String get accountConfirmPasswordCode =>
      'Gib dein Passwort und einen Code aus deiner Authenticator-App ein.';

  @override
  String get accountWrongPassword => 'Das Passwort stimmt nicht.';

  @override
  String get accountWrongCode => 'Der Code ist ungültig.';

  @override
  String get accountLastAdmin =>
      'Du bist der einzige Admin, daher lässt sich dein Konto noch nicht löschen. Mach zuerst jemand anderen zum Admin.';

  @override
  String get accountConfirmMismatch => 'Der Benutzername stimmt nicht.';

  @override
  String adminFilesCount(int count) {
    return '$count Dateien';
  }

  @override
  String get adminStorageTitle => 'Speicherrichtlinie';

  @override
  String get adminDefaultQuota => 'Standardkontingent pro Konto';

  @override
  String get adminDefaultQuotaDescription =>
      'Eine Größe wie 10GB oder „unlimited“.';

  @override
  String get adminMaxUpload => 'Größte einzelne Datei';

  @override
  String get adminMaxUploadDescription =>
      '0 heißt: keine Grenze außer dem Kontingent.';

  @override
  String get adminTrashDays => 'Tage im Papierkorb';

  @override
  String get adminTrashDaysDescription =>
      '0 behält Einträge, bis der Papierkorb geleert wird.';

  @override
  String get adminVersions => 'Versionen pro Datei';

  @override
  String get adminRecalculate => 'Belegung neu berechnen';

  @override
  String get adminAccessTitle => 'Registrierung';

  @override
  String get adminAllowSignup => 'Offene Registrierung';

  @override
  String get adminAllowSignupDescription =>
      'Aus: Niemand kann sich registrieren. Das erste Konto auf einem neuen Server geht immer.';

  @override
  String get adminServerTitle => 'Server';

  @override
  String get adminVersion => 'Version';

  @override
  String get adminUptime => 'Laufzeit';

  @override
  String get adminDatabase => 'Datenbank';

  @override
  String get adminAccounts => 'Konten';

  @override
  String get adminFiles => 'Dateien';

  @override
  String get adminStored => 'Gespeichert';

  @override
  String get adminDiskFree => 'Frei auf dem Speicherlaufwerk';

  @override
  String get adminLogs => 'Letzte Logs';

  @override
  String get adminMasterKey => 'Master-Key';

  @override
  String get adminMasterKeyDescription =>
      'Jede Datei ist unter diesem Schlüssel verschlüsselt. Bewahre eine Kopie offline auf – ohne ihn lassen sich Dateien und Backups nicht entschlüsseln.';

  @override
  String get adminShowMasterKey => 'Master-Key anzeigen';

  @override
  String get officePreviewUnavailable =>
      'Für diese Datei gibt es keine Vorschau. Lade sie herunter, um sie zu öffnen.';

  @override
  String get play => 'Abspielen';

  @override
  String get pause => 'Pause';

  @override
  String get mediaUnavailable => 'Diese Datei lässt sich hier nicht abspielen.';

  @override
  String get adminTabBackups => 'Backups';

  @override
  String get backupTargets => 'Backup-Ziele';

  @override
  String get backupTargetsDescription =>
      'Kopien aller Dateien und der Datenbank, verschlüsselt, auf einem anderen Laufwerk oder Server. Hochgeladen werden jeweils nur neue Dateien.';

  @override
  String get backupAddTarget => 'Ziel hinzufügen';

  @override
  String get backupEditTarget => 'Ziel bearbeiten';

  @override
  String get backupNoTargets => 'Noch keine Backup-Ziele.';

  @override
  String get backupName => 'Name';

  @override
  String get backupType => 'Typ';

  @override
  String get backupTypeLocal => 'Ordner / NAS';

  @override
  String get backupTypeSftp => 'SFTP';

  @override
  String get backupTypeS3 => 'S3-kompatibel';

  @override
  String get backupTypeWebdav => 'WebDAV';

  @override
  String get backupPath => 'Ordnerpfad';

  @override
  String get backupPathHint => 'z. B. /mnt/nas/neocloud';

  @override
  String get backupHost => 'Host';

  @override
  String get backupPort => 'Port';

  @override
  String get backupUsername => 'Benutzername';

  @override
  String get backupPassword => 'Passwort';

  @override
  String get backupPrivateKey => 'Privater Schlüssel (optional)';

  @override
  String get backupSecretKept =>
      'Leer lassen, um den gespeicherten zu behalten';

  @override
  String get backupEndpoint => 'Endpoint (leer für AWS)';

  @override
  String get backupRegion => 'Region';

  @override
  String get backupBucket => 'Bucket';

  @override
  String get backupAccessKey => 'Access Key';

  @override
  String get backupSecretKey => 'Secret Key';

  @override
  String get backupPrefix => 'Präfix (optional)';

  @override
  String get backupPathStyle => 'Path-Style-URLs (MinIO)';

  @override
  String get backupUrl => 'WebDAV-Ordner-URL';

  @override
  String get backupSchedule => 'Zeitplan';

  @override
  String get backupScheduleManual => 'Manuell';

  @override
  String get backupScheduleDaily => 'Täglich';

  @override
  String get backupScheduleWeekly => 'Wöchentlich';

  @override
  String get backupHour => 'Um Stunde (Serverzeit)';

  @override
  String get backupWeekday => 'Wochentag';

  @override
  String get backupRetention => 'Aufzubewahrende Stände';

  @override
  String get backupRunNow => 'Jetzt sichern';

  @override
  String get backupTest => 'Verbindung testen';

  @override
  String get backupTestOk => 'Verbindung funktioniert.';

  @override
  String get backupStarted => 'Backup gestartet.';

  @override
  String get backupNeverRun => 'Noch nie gesichert';

  @override
  String backupLastOk(String date, int count) {
    return 'Letztes Backup $date · $count neue Dateien';
  }

  @override
  String backupLastFailed(String error) {
    return 'Letztes Backup fehlgeschlagen: $error';
  }

  @override
  String get backupRunning => 'Sichert…';

  @override
  String backupRunningProgress(int percent, String done, String total) {
    return 'Sichert… $percent % · $done von $total';
  }

  @override
  String get backupDeleteMessage =>
      'Das Ziel wird aus NeoCloud entfernt. Vorhandene Backups bleiben dort erhalten.';

  @override
  String get backupHistory => 'Letzte Durchläufe';

  @override
  String get adminBackupRunOk => 'Fertig';

  @override
  String get adminBackupRunFailed => 'Fehlgeschlagen';

  @override
  String get backupRestoreHint =>
      'Wiederherstellen per CLI bei gestoppter NeoCloud: neocloud backup restore <id> <stand>. Dafür brauchst du den Master-Key.';

  @override
  String get adminPreviews => 'Vorschauen';

  @override
  String adminPreviewsValue(String office) {
    return 'Bilder: ja · Office: $office';
  }

  @override
  String get yes => 'ja';

  @override
  String get noLibreOffice => 'nein (LibreOffice installieren)';

  @override
  String get forgotPassword => 'Passwort vergessen?';

  @override
  String get forgotPasswordTitle => 'Passwort zurücksetzen';

  @override
  String get newMenu => 'Neu';

  @override
  String get uploadFiles => 'Dateien hochladen';

  @override
  String get sortBy => 'Sortieren';

  @override
  String get sortName => 'Name';

  @override
  String get sortSize => 'Größe';

  @override
  String get sortModified => 'Zuletzt geändert';

  @override
  String get sortType => 'Typ';

  @override
  String get sortRelevance => 'Relevanz';

  @override
  String get sortAscending => 'Aufsteigend';

  @override
  String get sortDescending => 'Absteigend';

  @override
  String movedTo(String folder) {
    return 'Nach „$folder“ verschoben.';
  }

  @override
  String dragItems(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count Elemente',
      one: '1 Element',
    );
    return '$_temp0';
  }

  @override
  String get selectAll => 'Alle auswählen';

  @override
  String get openFolder => 'Ordner öffnen';

  @override
  String get showInFolder => 'Im Ordner zeigen';

  @override
  String get copied2 => 'Kopie erstellt.';

  @override
  String get deviceApprovalTitle => 'Gerät verbinden';

  @override
  String get deviceApprovalSubtitle =>
      'Ein Gerät möchte auf deine Dateien zugreifen.';

  @override
  String get deviceApprovalEnterCode =>
      'Gib den Code ein, den das Gerät anzeigt.';

  @override
  String get deviceCodeLabel => 'Gerätecode';

  @override
  String get deviceCodeUnknown =>
      'Dieser Code ist unbekannt oder abgelaufen. Starte die Anmeldung auf dem Gerät erneut.';

  @override
  String deviceApproveHint(String account) {
    return 'Bestätige nur, wenn du die Anmeldung selbst gestartet hast. Das Gerät erhält Zugriff auf die Dateien von $account.';
  }

  @override
  String get deviceApprove => 'Zulassen';

  @override
  String get deviceDeny => 'Ablehnen';

  @override
  String deviceApproved(String name) {
    return '$name ist verbunden. Du kannst zum Gerät zurückkehren.';
  }

  @override
  String deviceDenied(String name) {
    return '$name wurde der Zugriff verweigert.';
  }

  @override
  String get openNeoCloud => 'NeoCloud öffnen';

  @override
  String get connectTitle => 'Mit NeoCloud verbinden';

  @override
  String get connectSubtitle =>
      'Gib die Adresse deines NeoCloud-Servers ein und melde dich dann an.';

  @override
  String get connectServerLabel => 'Serveradresse';

  @override
  String get connectButton => 'Verbinden';

  @override
  String get connectInvalidServer =>
      'Gib eine Serveradresse wie https://cloud.example.com ein.';

  @override
  String get connectApproveTitle => 'Im Browser bestätigen';

  @override
  String get connectApproveSubtitle =>
      'Melde dich im geöffneten Browserfenster bei NeoCloud an und prüfe, dass dort dieser Code steht.';

  @override
  String get connectWaiting => 'Warte auf Bestätigung …';

  @override
  String get connectOpenBrowser => 'Browser erneut öffnen';

  @override
  String get connectDenied => 'Die Anmeldung wurde im Browser abgelehnt.';

  @override
  String get connectExpired =>
      'Der Code ist abgelaufen, bevor er bestätigt wurde. Verbinde dich erneut.';

  @override
  String get deviceRevokedNotice =>
      'Dieses Gerät wurde abgemeldet. Verbinde es erneut, um fortzufahren.';

  @override
  String get vaultBoundaryError =>
      'Elemente können nicht in einen Tresor oder aus ihm heraus verschoben werden. Lade sie stattdessen herunter und wieder hoch.';

  @override
  String shareTargetTitle(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count Dateien in NeoCloud speichern',
      one: '1 Datei in NeoCloud speichern',
    );
    return '$_temp0';
  }

  @override
  String get uploadHere => 'Hierher hochladen';

  @override
  String shareRejected(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count geteilte Elemente konnten nicht übernommen werden.',
      one: '1 geteiltes Element konnte nicht übernommen werden.',
    );
    return '$_temp0';
  }

  @override
  String get shareTooLarge =>
      'Was du geteilt hast, ist größer als 2 GB und wurde nicht übernommen. Große Dateien lädst du unter „Dateien“ hoch.';

  @override
  String get edit => 'Bearbeiten';

  @override
  String get editorDone => 'Fertig';

  @override
  String get editorSaved => 'Als neue Version gespeichert';

  @override
  String get editorUnsaved => 'Ungespeicherte Änderungen';

  @override
  String get editorDiscardTitle => 'Änderungen verwerfen?';

  @override
  String get editorDiscardMessage =>
      'Was du seit dem letzten Speichern geändert hast, geht verloren.';

  @override
  String get editorKeepEditing => 'Weiter bearbeiten';

  @override
  String get editorDiscard => 'Verwerfen';

  @override
  String get editorConflictTitle => 'Anderswo geändert';

  @override
  String editorConflictMessage(String name) {
    return '$name wurde geändert, seit du die Datei geöffnet hast. Trotzdem speichern? Die andere Fassung bleibt im Versionsverlauf.';
  }

  @override
  String get editorOverwrite => 'Trotzdem speichern';

  @override
  String get vaultNoMediaPreview =>
      'Videos und Audio aus einem Tresor lassen sich nicht direkt abspielen. Lade die Datei herunter, um sie abzuspielen.';

  @override
  String get shareLink => 'Teilen';

  @override
  String get newVault => 'Neuer Tresor';

  @override
  String get vaultLock => 'Sperren';

  @override
  String get vaultSubtitle =>
      'Ende-zu-Ende-verschlüsselt. Namen und Inhalte werden auf diesem Gerät verschlüsselt; NeoCloud kann sie nicht lesen.';

  @override
  String get vaultIntro =>
      'Ein Tresor ist ein Ordner, dessen Namen und Inhalte auf deinem Gerät mit einer Passphrase verschlüsselt werden, die nur du kennst.';

  @override
  String get vaultPassphrase => 'Passphrase';

  @override
  String get vaultPassphraseRepeat => 'Passphrase wiederholen';

  @override
  String get vaultNoRecovery =>
      'Die Passphrase lässt sich nicht wiederherstellen oder zurücksetzen, auch nicht durch NeoCloud. Vergisst du sie, sind die Dateien in diesem Tresor verloren.';

  @override
  String get vaultCreate => 'Tresor erstellen';

  @override
  String get vaultNameRequired => 'Gib dem Tresor einen Namen.';

  @override
  String vaultPassphraseTooShort(int count) {
    return 'Verwende mindestens $count Zeichen.';
  }

  @override
  String get vaultWrongPassphrase =>
      'Mit dieser Passphrase lässt sich der Tresor nicht öffnen.';

  @override
  String vaultLockedTitle(String name) {
    return '$name ist gesperrt';
  }

  @override
  String get vaultLockedSubtitle =>
      'Gib die Passphrase ein, um Dateien zu sehen und hinzuzufügen. Der Tresor bleibt entsperrt, bis du ihn sperrst oder NeoCloud schließt.';

  @override
  String get vaultUnlock => 'Entsperren';

  @override
  String shareLinkTitle(String name) {
    return '„$name“ teilen';
  }

  @override
  String get shareExpiry => 'Link läuft ab';

  @override
  String get shareExpiryNever => 'Nie';

  @override
  String get shareExpiryDay => '1 Tag';

  @override
  String get shareExpiryWeek => '7 Tage';

  @override
  String get shareExpiryMonth => '30 Tage';

  @override
  String get sharePasswordToggle => 'Mit Passwort schützen';

  @override
  String get sharePasswordHint =>
      'Besucher brauchen es, um den Link zu öffnen.';

  @override
  String get sharePassword => 'Link-Passwort';

  @override
  String get sharePasswordRequired =>
      'Gib ein Passwort ein oder schalte den Schutz aus.';

  @override
  String get shareAllowUpload => 'Hochladen erlauben';

  @override
  String get shareAllowUploadHint =>
      'Jeder mit dem Link kann Dateien in diesen Ordner legen.';

  @override
  String get shareCreate => 'Link erstellen';

  @override
  String get shareExpired => 'Abgelaufen';

  @override
  String get shareNeverExpires => 'Läuft nie ab';

  @override
  String shareExpiresOn(String date) {
    return 'Läuft ab am $date';
  }

  @override
  String get sharePasswordTag => 'Passwort';

  @override
  String get shareUploadTag => 'Hochladen erlaubt';

  @override
  String get shareFolderTag => 'Ordner';

  @override
  String get shareRevoke => 'Widerrufen';

  @override
  String shareRevokeConfirm(String name) {
    return 'Der Link zu $name funktioniert sofort für niemanden mehr.';
  }

  @override
  String get shareRevoked => 'Link widerrufen.';

  @override
  String get shareCopied => 'Link kopiert.';

  @override
  String get pageSharedLinks => 'Geteilte Links';

  @override
  String get pageSharedLinksDescription =>
      'Öffentliche Links zu deinen Dateien und Ordnern.';

  @override
  String get sharedLinksHint =>
      'Links erstellst du über die Aktionen einer Datei. Ein Widerruf gilt sofort.';

  @override
  String get sharedLinksEmpty => 'Noch keine geteilten Links.';

  @override
  String get pageCameraUpload => 'Kamera-Upload';

  @override
  String get pageCameraUploadDescription =>
      'Neue Fotos und Videos von diesem Telefon automatisch hochladen.';

  @override
  String get disconnect => 'Trennen';

  @override
  String get disconnectTitle => 'Dieses Gerät trennen?';

  @override
  String disconnectMessage(String server) {
    return 'Dieses Gerät verliert den Zugang zu $server und der Kamera-Upload wird ausgeschaltet. Zum erneuten Verbinden meldest du dich noch einmal an.';
  }

  @override
  String get syncChooseThisFolder => 'Diesen Ordner wählen';

  @override
  String get syncChoose => 'Wählen';

  @override
  String get syncNothingChosen => 'Noch nicht gewählt';

  @override
  String get cameraAccessTitle => 'Zugriff auf deine Fotos erlauben';

  @override
  String get cameraAccessMessage =>
      'NeoCloud liest neue Fotos und Videos aus deiner Mediathek, um sie in deine NeoCloud hochzuladen. Sonst verlässt nichts das Telefon. Im nächsten Schritt kannst du „Alle Fotos“ oder eine Auswahl erlauben.';

  @override
  String get cameraAccessDenied =>
      'NeoCloud hat keinen Zugriff auf deine Fotos. Erlaube ihn in den Systemeinstellungen, um den Kamera-Upload zu nutzen.';

  @override
  String get cameraUploadOn =>
      'Kamera-Upload ist aktiv. Neue Fotos werden ab jetzt hochgeladen.';

  @override
  String get cameraReconfirmTitle => 'Kamera-Upload wieder einschalten?';

  @override
  String cameraReconfirmMessage(String server) {
    return 'Der Kamera-Upload wurde ausgeschaltet, weil sich dieses Gerät abgemeldet oder mit einem anderen Server oder Konto verbunden hat. Fotos gehen erst an $server, wenn du ihn neu einrichtest.';
  }

  @override
  String get cameraReconfirmLater => 'Jetzt nicht';

  @override
  String get cameraReconfirmSetUp => 'Einrichten';

  @override
  String get cameraUploadFolder => 'Hochladen nach';

  @override
  String get cameraUploadEnable => 'Neue Fotos hochladen';

  @override
  String get cameraUploadEnableDescription =>
      'Fotos, die nach dem Einschalten aufgenommen werden; ältere bleiben nur auf dem Telefon.';

  @override
  String get cameraUploadWifiOnly => 'Nur über WLAN';

  @override
  String get cameraUploadVideos => 'Videos einbeziehen';

  @override
  String get cameraUploadStatus => 'Status';

  @override
  String get cameraUploadBackgroundHint =>
      'Im Hintergrund entscheidet das System, wann NeoCloud laufen darf: unter Android höchstens etwa stündlich, unter iOS einige Male am Tag. Beim Öffnen der App wird sofort hochgeladen.';

  @override
  String get cameraUploadOffState => 'Aus';

  @override
  String get cameraUploadFailedState => 'Letzter Lauf fehlgeschlagen';

  @override
  String get cameraUploadFailedHint =>
      'Prüfe das Netzwerk und ob NeoCloud deine Fotos noch lesen darf.';

  @override
  String cameraUploadCount(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count Dateien hochgeladen',
      one: '1 Datei hochgeladen',
      zero: 'Noch nichts hochgeladen',
    );
    return '$_temp0';
  }

  @override
  String cameraUploadSince(String date) {
    return 'seit $date';
  }

  @override
  String cameraUploadLastRun(String date) {
    return 'zuletzt $date';
  }

  @override
  String get cameraUploadRunNow => 'Jetzt hochladen';

  @override
  String get cameraAccessSettings => 'Fotozugriff';

  @override
  String get cameraOpenSettings => 'Einstellungen öffnen';

  @override
  String get shareSummaryView =>
      'Alle mit diesem Link können ansehen und herunterladen';

  @override
  String get shareSummaryViewUpload =>
      'Alle mit diesem Link können ansehen, herunterladen und Dateien hinzufügen';

  @override
  String get shareSummaryUploadOnly =>
      'Alle mit diesem Link können Dateien hinzufügen, sehen aber nicht, was im Ordner liegt';

  @override
  String get shareSummaryPassword => 'Passwort nötig';

  @override
  String shareSummaryExpires(String date) {
    return 'läuft am $date ab';
  }

  @override
  String get shareSummaryNoExpiry => 'läuft nicht ab';

  @override
  String get shareSummaryExpired => 'abgelaufen';

  @override
  String get shareSummaryOneTime => 'Einmal-Link';

  @override
  String shareSummaryDownloadsLeft(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: 'noch $count Downloads',
      one: 'noch 1 Download',
    );
    return '$_temp0';
  }

  @override
  String get shareSummaryUsedUp => 'Download-Limit erreicht';

  @override
  String shareDownloadedTimes(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count× heruntergeladen',
      zero: 'Noch nicht heruntergeladen',
    );
    return '$_temp0';
  }

  @override
  String shareExpiresInDays(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: 'läuft in $count Tagen ab',
      one: 'läuft morgen ab',
    );
    return '$_temp0';
  }

  @override
  String shareExpiresInHours(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: 'läuft in $count Stunden ab',
      one: 'läuft in 1 Stunde ab',
      zero: 'läuft in der nächsten Stunde ab',
    );
    return '$_temp0';
  }

  @override
  String shareExpiredOn(String date) {
    return 'abgelaufen am $date';
  }

  @override
  String get shareModeTitle => 'Besucher können';

  @override
  String get shareModeView => 'Ansehen';

  @override
  String get shareModeViewUpload => 'Ansehen + hochladen';

  @override
  String get shareModeUploadOnly => 'Dateianfrage';

  @override
  String get shareModeViewHint =>
      'Sehen, ansehen und herunterladen, was du teilst.';

  @override
  String get shareModeViewUploadHint =>
      'Zusätzlich Dateien in diesen Ordner legen. Nichts wird überschrieben.';

  @override
  String get shareModeUploadOnlyHint =>
      'Nur Dateien hinzufügen. Besucher sehen den Ordnerinhalt nicht, auch nicht, was andere hochgeladen haben.';

  @override
  String get shareExpiryCustom => 'Datum…';

  @override
  String get shareExpiryPick => 'Läuft ab am';

  @override
  String get sharePasswordGenerate => 'Erzeugen';

  @override
  String get sharePasswordCopy => 'Passwort kopieren';

  @override
  String get sharePasswordCopied => 'Passwort kopiert.';

  @override
  String get sharePasswordSendSeparately =>
      'Schicke das Passwort auf einem anderen Weg als den Link, z. B. telefonisch oder per anderem Messenger.';

  @override
  String sharePasswordTooShort(int count) {
    return 'Mindestens $count Zeichen.';
  }

  @override
  String get sharePasswordKeep =>
      'Leer lassen, um das bisherige Passwort zu behalten.';

  @override
  String get shareLimitTitle => 'Download-Limit';

  @override
  String get shareLimitNone => 'Kein Limit';

  @override
  String get shareLimitOnce => 'Einmalig';

  @override
  String get shareLimitCustom => 'Eigenes';

  @override
  String get shareLimitCount => 'Erlaubte Downloads';

  @override
  String get shareLimitHint =>
      'Jeder Datei- oder ZIP-Download zählt. Danach funktioniert der Link nicht mehr.';

  @override
  String get shareLimitInvalid => 'Gib eine Zahl von 1 bis 1.000.000 ein.';

  @override
  String get shareNote => 'Hinweis für Besucher (optional)';

  @override
  String get shareNoteHint => 'Wird auf der Freigabeseite angezeigt.';

  @override
  String get shareSave => 'Änderungen speichern';

  @override
  String get shareSaved => 'Link aktualisiert.';

  @override
  String get shareEdit => 'Link bearbeiten';

  @override
  String get shareNewLink => 'Neuer Link';

  @override
  String shareMoreLinks(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other:
          '$count weitere aktive Links dafür unter Einstellungen › Geteilte Links.',
      one:
          '1 weiterer aktiver Link dafür unter Einstellungen › Geteilte Links.',
    );
    return '$_temp0';
  }

  @override
  String get shareGroupActive => 'Aktiv';

  @override
  String get shareGroupInactive => 'Abgelaufen oder aufgebraucht';

  @override
  String get shareNoteTag => 'Hinweis';

  @override
  String get shareModeFileRequestTag => 'Dateianfrage';

  @override
  String get shareExpiryInPast => 'Wähle ein Ablaufdatum in der Zukunft.';

  @override
  String get vaultShowPassphrase => 'Anzeigen oder verbergen';

  @override
  String get vaultGeneratePassphrase => 'Passphrase erzeugen';

  @override
  String get vaultGeneratedHint =>
      'Sechs zufällige Wörter. Schreib sie auf – der Tresor öffnet sich nur damit oder mit dem Wiederherstellungsschlüssel.';

  @override
  String get vaultCopyPassphrase => 'Passphrase kopieren';

  @override
  String get vaultStrengthTooWeak => 'Zu schwach';

  @override
  String get vaultStrengthWeak => 'Schwach';

  @override
  String get vaultStrengthGood => 'Gut';

  @override
  String get vaultStrengthStrong => 'Stark';

  @override
  String vaultStrengthLabel(String label) {
    return 'Stärke: $label';
  }

  @override
  String vaultHintTooShort(int count) {
    return 'Mindestens $count Zeichen, oder erzeuge eine Passphrase.';
  }

  @override
  String get vaultHintTooWeak =>
      'Leicht zu erraten. Füge Wörter oder Zeichen hinzu, vermeide Namen, Jahreszahlen und gängige Passwörter.';

  @override
  String get vaultHintGood =>
      'Gut. Länger ist stärker: vier oder mehr zusammenhanglose Wörter funktionieren gut.';

  @override
  String get vaultHintStrong => 'Starke Passphrase.';

  @override
  String get vaultWarningCallout =>
      'Nur du hast den Schlüssel. Wenn du Passphrase und Wiederherstellungsschlüssel verlierst, kann niemand – auch nicht NeoCloud – diesen Tresor wieder öffnen.';

  @override
  String get vaultNext => 'Weiter';

  @override
  String get vaultBack => 'Zurück';

  @override
  String get vaultStepDetails => 'Schritt 1 von 3 · Name und Passphrase';

  @override
  String get vaultStepRecovery =>
      'Schritt 2 von 3 · Wiederherstellungsschlüssel';

  @override
  String get vaultStepConfirm =>
      'Schritt 3 von 3 · Wiederherstellungsschlüssel prüfen';

  @override
  String get vaultRecoveryTitle => 'Wiederherstellungsschlüssel';

  @override
  String get vaultRecoveryIntro =>
      'Diese 24 Wörter öffnen den Tresor, falls du die Passphrase vergisst. Schreib sie auf oder drucke das Blatt aus und bewahre es sicher und offline auf.';

  @override
  String get vaultRecoveryDontStore =>
      'Speichere den Wiederherstellungsschlüssel nicht in NeoCloud. Wer ihn hat, kann den Tresor öffnen.';

  @override
  String get vaultRecoveryCopy => 'Wörter kopieren';

  @override
  String get vaultRecoveryDownload => 'Wiederherstellungsblatt herunterladen';

  @override
  String get vaultRecoverySaved =>
      'Ich habe den Wiederherstellungsschlüssel sicher aufbewahrt';

  @override
  String get vaultRecoveryCheckIntro =>
      'Gib diese Wörter aus deinem Wiederherstellungsschlüssel ein, um sicherzugehen, dass du ihn hast.';

  @override
  String vaultRecoveryWordNumber(String number) {
    return 'Wort $number';
  }

  @override
  String get vaultRecoveryCheckWrong =>
      'Das passt nicht zum Wiederherstellungsschlüssel. Prüfe, was du aufbewahrt hast.';

  @override
  String get vaultSheetTitle => 'NeoCloud-Tresor – Wiederherstellungsschlüssel';

  @override
  String vaultSheetVault(String name) {
    return 'Tresor: $name';
  }

  @override
  String vaultSheetServer(String server) {
    return 'Server: $server';
  }

  @override
  String vaultSheetCreated(String date) {
    return 'Erstellt: $date';
  }

  @override
  String get vaultSheetUse =>
      'Falls du die Passphrase vergisst: Öffne den Tresor in NeoCloud, wähle „Passphrase vergessen?“ und gib diese 24 Wörter in der Reihenfolge ein.';

  @override
  String get vaultSheetKeep =>
      'Bewahre dieses Blatt offline auf, z. B. ausgedruckt bei deinen wichtigen Unterlagen. Speichere es nicht in NeoCloud. Wer diese Wörter hat, kann den Tresor öffnen.';

  @override
  String get vaultRecoveryCopied =>
      'Wiederherstellungswörter kopiert. Füge sie offline ein und leere die Zwischenablage.';

  @override
  String get vaultForgot => 'Passphrase vergessen?';

  @override
  String get vaultRecoverTitle => 'Passphrase zurücksetzen';

  @override
  String get vaultRecoverIntro =>
      'Gib die 24 Wörter des Wiederherstellungsschlüssels ein, den du beim Erstellen des Tresors gesichert hast, und wähle eine neue Passphrase.';

  @override
  String get vaultRecoverWords => 'Wiederherstellungsschlüssel (24 Wörter)';

  @override
  String vaultRecoverWordCount(int count) {
    return '$count von 24 Wörtern';
  }

  @override
  String vaultRecoverUnknownWord(String number) {
    return 'Wort $number ist kein Wiederherstellungswort. Prüfe die Schreibweise.';
  }

  @override
  String get vaultRecoverChecksum =>
      'Diese Wörter ergeben keinen gültigen Wiederherstellungsschlüssel. Prüfe Reihenfolge und Schreibweise.';

  @override
  String get vaultRecoverWordsOk =>
      'Wiederherstellungsschlüssel sieht richtig aus.';

  @override
  String get vaultRecoverMismatch =>
      'Dieser Wiederherstellungsschlüssel gehört zu einem anderen Tresor.';

  @override
  String get vaultNewPassphrase => 'Neue Passphrase';

  @override
  String get vaultRecoverSubmit => 'Zurücksetzen und entsperren';

  @override
  String get vaultRecovered =>
      'Neue Passphrase gesetzt. Der Tresor ist entsperrt; dein Wiederherstellungsschlüssel bleibt gleich.';

  @override
  String get vaultShowRecovery => 'Wiederherstellungsschlüssel anzeigen';

  @override
  String get vaultShowRecoveryIntro =>
      'Gib die Passphrase des Tresors ein, um den Wiederherstellungsschlüssel zu sehen.';

  @override
  String get vaultBanner =>
      'Keine Vorschau, Suche oder Freigaben in Tresoren – mit Absicht. Namen und Inhalte werden vor dem Hochladen auf diesem Gerät verschlüsselt.';

  @override
  String get vaultUnlocking => 'Wird entsperrt…';

  @override
  String get vaultConvertAction => 'In Tresor umwandeln…';

  @override
  String get vaultConvertResumeAction => 'Umwandlung in Tresor fortsetzen…';

  @override
  String vaultConvertTitle(String name) {
    return '„$name“ in einen Tresor umwandeln';
  }

  @override
  String get vaultConvertIntro =>
      'Alles in diesem Ordner wird auf diesem Gerät verschlüsselt und in einen neuen Tresor kopiert, den nur du öffnen kannst.';

  @override
  String get vaultConvertStepCopy =>
      'Jede Datei wird heruntergeladen, hier verschlüsselt und wieder hochgeladen. Große Ordner brauchen eine Weile; du kannst anhalten und später weitermachen.';

  @override
  String get vaultConvertStepPassphrase =>
      'Du wählst eine Passphrase und bekommst einen Wiederherstellungsschlüssel. Ohne einen von beiden kann niemand diese Dateien je wieder öffnen, auch NeoCloud nicht.';

  @override
  String get vaultConvertStepTrash =>
      'Erst wenn jede Datei sicher im Tresor ist, kommt der ursprüngliche Ordner in den Papierkorb. Lösche ihn dort für vollen Schutz.';

  @override
  String vaultConvertSummary(int files, String size) {
    String _temp0 = intl.Intl.pluralLogic(
      files,
      locale: localeName,
      other: '$files Dateien',
      one: '1 Datei',
      zero: 'Keine Dateien',
    );
    return '$_temp0 · $size';
  }

  @override
  String get vaultConvertTrashSpace =>
      'Bis du den Papierkorb leerst, zählen die Originale weiter zu deinem Speicher.';

  @override
  String vaultConvertNoSpace(String needed, String free) {
    return 'Für die verschlüsselte Kopie ist nicht genug Platz: Sie braucht $needed, frei sind $free. Schaffe Platz oder bitte um mehr Speicher.';
  }

  @override
  String get vaultConvertStart => 'Passphrase wählen';

  @override
  String get vaultConvertRefusedTitle =>
      'Dieser Ordner kann kein Tresor werden';

  @override
  String get vaultConvertProblemShared =>
      'Er ist mit anderen geteilt, die den Zugriff verlieren würden. Beende zuerst die Freigabe.';

  @override
  String get vaultConvertProblemLink =>
      'Er oder etwas darin hat einen öffentlichen Link. Entferne zuerst den Link.';

  @override
  String get vaultConvertProblemContainsVault =>
      'Er enthält schon einen Tresor, und Tresore können nicht in anderen Tresoren liegen.';

  @override
  String get vaultConvertProblemInVault => 'Er liegt schon in einem Tresor.';

  @override
  String get vaultConvertProblemOther =>
      'Er kann gerade nicht umgewandelt werden.';

  @override
  String vaultConvertProgressTitle(String name) {
    return '„$name“ wird umgewandelt';
  }

  @override
  String vaultConvertProgressFiles(int done, int total) {
    return '$done von $total Dateien';
  }

  @override
  String vaultConvertProgressBytes(String done, String total) {
    return '$done von $total';
  }

  @override
  String get vaultConvertFinishing => 'Wird geprüft und abgeschlossen…';

  @override
  String get vaultConvertKeepOpen =>
      'Lass das hier offen, bis es fertig ist. Schließt sich die App, wähle beim Ordner wieder „In Tresor umwandeln…“, um weiterzumachen.';

  @override
  String get vaultConvertStop => 'Anhalten';

  @override
  String vaultConvertDoneTitle(String name) {
    return '„$name“ ist jetzt ein Tresor';
  }

  @override
  String get vaultConvertDoneBody =>
      'Die ursprünglichen Dateien liegen im Papierkorb. Bis du sie dort löschst, lassen sie sich ohne Passphrase öffnen.';

  @override
  String vaultConvertDoneSkipped(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count Dateien, die nicht kopiert werden konnten, liegen',
      one: '1 Datei, die nicht kopiert werden konnte, liegt',
    );
    return '$_temp0 mit den Originalen im Papierkorb.';
  }

  @override
  String get vaultConvertOpenVault => 'Tresor öffnen';

  @override
  String get vaultConvertStoppedTitle => 'Umwandlung angehalten';

  @override
  String get vaultConvertStoppedBody =>
      'Dein ursprünglicher Ordner ist unverändert. Der teilweise gefüllte Tresor bleibt daneben; wähle beim Ordner „In Tresor umwandeln…“, um weiterzumachen, oder lösche den Tresor.';

  @override
  String get vaultConvertOffline =>
      'Die Verbindung ist weg. Dein ursprünglicher Ordner ist unverändert; mach weiter, sobald du wieder online bist.';

  @override
  String get vaultConvertQuota =>
      'Dein Speicher ist voll. Dein ursprünglicher Ordner ist unverändert; schaffe Platz und mach dann weiter.';

  @override
  String get vaultConvertChanging =>
      'Dateien im Ordner ändern sich laufend. Warte, bis sie fertig sind, und mach dann weiter.';

  @override
  String get vaultConvertVerifyFailed =>
      'Eine Datei ließ sich aus dem Tresor nicht korrekt zurücklesen, deshalb bleiben die Originale, wo sie sind. Versuch es noch einmal; passiert es wieder, lösche den Tresor.';

  @override
  String vaultConvertError(String error) {
    return 'Etwas ist schiefgegangen: $error. Dein ursprünglicher Ordner ist unverändert.';
  }

  @override
  String get vaultConvertFailedTitle =>
      'Einige Dateien konnten nicht kopiert werden';

  @override
  String vaultConvertFailedBody(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count Dateien konnten nicht kopiert werden',
      one: '1 Datei konnte nicht kopiert werden',
    );
    return '$_temp0; alles andere ist im Tresor. Versuch es noch einmal oder schließe ohne sie ab: Sie kommen mit den anderen Originalen in den Papierkorb.';
  }

  @override
  String get vaultConvertRetry => 'Noch einmal versuchen';

  @override
  String get vaultConvertFinishAnyway => 'Ohne sie abschließen';

  @override
  String get vaultConvertContinue => 'Weiter';

  @override
  String get vaultConvertLater => 'Später';

  @override
  String get vaultConvertResumeIntro =>
      'Die Umwandlung dieses Ordners wurde schon begonnen. Gib die Passphrase des Tresors ein, um weiterzumachen.';

  @override
  String get vaultConvertForget => 'Neu beginnen';

  @override
  String get vaultConvertForgetNote =>
      'Beim Neubeginn bleibt der teilweise gefüllte Tresor bestehen; lösche ihn selbst, wenn du ihn nicht brauchst.';

  @override
  String get securityChecklistTitle => 'Server wiederherstellbar machen';

  @override
  String get securityChecklistSubtitle =>
      'Drei Dinge, die entscheiden, ob deine Dateien eine verlorene Platte oder einen verlorenen Server überstehen.';

  @override
  String get securityMasterKeyItem => 'Master-Key exportiert';

  @override
  String get securityMasterKeyItemHint =>
      'Alle Dateien und Backups sind damit verschlüsselt. Bewahre eine Kopie offline auf, außerhalb dieses Servers.';

  @override
  String get securityBackupTargetItem => 'Backup-Ziel eingerichtet';

  @override
  String get securityBackupTargetItemHint =>
      'Ein zweiter Ort – andere Platte, SFTP, S3 oder WebDAV –, der eine verschlüsselte Kopie bekommt.';

  @override
  String get securityBackupVerifiedItem => 'Backup geprüft';

  @override
  String get securityBackupVerifiedItemHint =>
      'Eine Probe-Wiederherstellung des letzten Snapshots hat geklappt.';

  @override
  String get securityBackupRanItem => 'Ein Backup ist erfolgreich gelaufen';

  @override
  String get securityExportKey => 'Master-Key exportieren';

  @override
  String get securitySetUpBackup => 'Backups einrichten';

  @override
  String get securityVerifyNow => 'Jetzt prüfen';

  @override
  String get securityVerifyOk => 'Backup geprüft.';

  @override
  String securityVerifyFailed(String error) {
    return 'Prüfung fehlgeschlagen: $error';
  }

  @override
  String get securityDone => 'Erledigt';

  @override
  String get securityOpen => 'Offen';

  @override
  String get securityBanner =>
      'Exportiere den Master-Key und bewahre ihn offline auf. Ohne ihn lassen sich Dateien und Backups nicht entschlüsseln, wenn dieser Server verloren geht.';

  @override
  String get securityKeyStored =>
      'Ich habe den Master-Key sicher außerhalb dieses Servers aufbewahrt';

  @override
  String get securityKeyDownload => 'Als Datei herunterladen';

  @override
  String get securityDatabaseOk => 'OK';

  @override
  String get securityDatabasePending => 'Wird geprüft…';

  @override
  String get securityDatabaseDamaged => 'Beschädigt';

  @override
  String securityDatabaseCheckedAt(String date) {
    return 'geprüft $date';
  }

  @override
  String securityDamagedBlobs(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count beschädigte Dateien bei der Hintergrundprüfung gefunden',
      one: '1 beschädigte Datei bei der Hintergrundprüfung gefunden',
    );
    return '$_temp0';
  }

  @override
  String get securityDiskFull =>
      'NeoCloud hat gerade keinen Platz mehr, daher wurde nichts gespeichert. Versuch es später noch einmal oder wende dich an die Person, die sie betreibt.';

  @override
  String get securityServerStopping =>
      'NeoCloud startet neu. Versuch es gleich noch einmal.';

  @override
  String get filesFavoritesEmpty => 'Noch keine Favoriten';

  @override
  String get filesFavoritesEmptySubtitle =>
      'Markiere oft genutzte Dateien und Ordner mit einem Stern – sie erscheinen dann hier.';

  @override
  String get filesFavoritesSubtitle =>
      'Dateien und Ordner, die du mit einem Stern markiert hast.';

  @override
  String get filesNavFavorites => 'Favoriten';

  @override
  String filesTrashExpiresIn(int days) {
    String _temp0 = intl.Intl.pluralLogic(
      days,
      locale: localeName,
      other: 'wird in $days Tagen endgültig gelöscht',
      one: 'wird in 1 Tag endgültig gelöscht',
      zero: 'wird heute endgültig gelöscht',
    );
    return '$_temp0';
  }

  @override
  String filesAndMore(String names, int count) {
    return '$names und $count weitere';
  }

  @override
  String filesCut(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other:
          '$count Elemente ausgeschnitten · in einem anderen Ordner einfügen',
      one: '1 Element ausgeschnitten · in einem anderen Ordner einfügen',
    );
    return '$_temp0';
  }

  @override
  String get filesCutAction => 'Ausschneiden';

  @override
  String get filesFavorite => 'Favorit';

  @override
  String filesFavoriteFailed(String names) {
    return 'Stern für $names konnte nicht geändert werden.';
  }

  @override
  String get filesKindFolder => 'Ordner';

  @override
  String filesModifiedOn(String date) {
    return 'geändert am $date';
  }

  @override
  String get filesModifiedToday => 'heute geändert';

  @override
  String get filesModifiedYesterday => 'gestern geändert';

  @override
  String filesMoveFailed(String names, String folder) {
    return '$names konnte nicht nach „$folder“ verschoben werden.';
  }

  @override
  String filesPasteHere(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count Elemente hier einfügen',
      one: '1 Element hier einfügen',
    );
    return '$_temp0';
  }

  @override
  String filesRenameFailed(String name) {
    return '„$name“ konnte nicht umbenannt werden.';
  }

  @override
  String filesRestoreFailed(String names) {
    return '$names konnte nicht wiederhergestellt werden.';
  }

  @override
  String get filesShortcutsHint =>
      '? zeigt die Tastenkürzel · Strg/⌘+K öffnet die Befehle · Elemente auf einen Ordner ziehen, um sie zu verschieben';

  @override
  String get filesStar => 'Zu Favoriten';

  @override
  String filesStarred(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count Elemente zu Favoriten hinzugefügt',
      one: 'Zu Favoriten hinzugefügt',
    );
    return '$_temp0';
  }

  @override
  String filesTrashFailed(String names) {
    return '$names konnte nicht in den Papierkorb verschoben werden.';
  }

  @override
  String filesUndoFailed(String names) {
    return 'Rückgängig machen für $names fehlgeschlagen.';
  }

  @override
  String get filesUnstar => 'Aus Favoriten entfernen';

  @override
  String filesUnstarred(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count Elemente aus Favoriten entfernt',
      one: 'Aus Favoriten entfernt',
    );
    return '$_temp0';
  }

  @override
  String get filesWelcomeTitle => 'Willkommen in deiner Cloud';

  @override
  String get filesWelcomeUploadBody =>
      'Lade Dateien hoch oder zieh sie irgendwo auf diese Seite.';

  @override
  String get filesWelcomeUploadBodyPhone =>
      'Lade Dateien mit der Plus-Schaltfläche hoch oder schalte den Kamera-Upload ein.';

  @override
  String get filesWelcomeDesktopBody =>
      'Halte einen Ordner auf deinem Computer synchron.';

  @override
  String get filesWelcomeDesktopAction => 'App holen';

  @override
  String get filesWelcomeKeyBody =>
      'Ohne ihn kann ein wiederhergestellter Server keine Datei lesen. Bewahre ihn sicher auf.';

  @override
  String get filesWelcomeKeyAction => 'Verwaltung öffnen';

  @override
  String get paletteActions => 'Aktionen';

  @override
  String get paletteFolders => 'Gehe zu Ordner';

  @override
  String get paletteHint => 'Befehl, Ordner oder Seite eingeben';

  @override
  String get paletteNothing => 'Keine Treffer.';

  @override
  String get palettePlaces => 'Orte';

  @override
  String get paletteRecent => 'Zuletzt geöffnet';

  @override
  String get paletteSearchFiles => 'Dateien durchsuchen';

  @override
  String get paletteShortcuts => 'Tastenkürzel';

  @override
  String get paletteTitle => 'Befehlspalette';

  @override
  String get paletteShortcutClear => 'Auswahl oder Suche leeren';

  @override
  String get paletteShortcutCutPaste =>
      'Ausschneiden und in anderen Ordner einfügen';

  @override
  String get paletteShortcutExtend => 'Auswahl erweitern';

  @override
  String get paletteShortcutHomeEnd => 'Erstes oder letztes Element';

  @override
  String get paletteShortcutMove => 'Zwischen Elementen wechseln';

  @override
  String get paletteShortcutSelectAll => 'Alles auswählen';

  @override
  String get paletteShortcutToggle => 'Auswählen oder abwählen';

  @override
  String get paletteShortcutTypeAhead =>
      'Zu einem Namen springen, indem man ihn tippt';

  @override
  String get searchAny => 'Alle';

  @override
  String get searchClearFilters => 'Filter zurücksetzen';

  @override
  String get searchEmptyHint =>
      'Versuche weniger Wörter, andere Filter oder alle Ordner.';

  @override
  String get searchEverywhere => 'Alle Ordner durchsuchen';

  @override
  String get searchLookInTrash => 'Im Papierkorb nachsehen';

  @override
  String get searchModified => 'Geändert';

  @override
  String get searchModifiedDay => 'Letzte 24 Stunden';

  @override
  String get searchModifiedWeek => 'Letzte 7 Tage';

  @override
  String get searchModifiedMonth => 'Letzte 30 Tage';

  @override
  String get searchModifiedYear => 'Letztes Jahr';

  @override
  String get searchNothingFiltered => 'Nichts passt zu diesen Filtern.';

  @override
  String get searchOpenFolder => 'Enthaltenden Ordner öffnen';

  @override
  String searchResultsCount(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count Treffer',
      one: '1 Treffer',
      zero: 'Keine Treffer',
    );
    return '$_temp0';
  }

  @override
  String get searchSize => 'Größe';

  @override
  String get searchSize10m => 'Über 10 MB';

  @override
  String get searchSize100m => 'Über 100 MB';

  @override
  String get searchSize1g => 'Über 1 GB';

  @override
  String get searchThisFolder => 'Dieser Ordner';

  @override
  String get searchType => 'Typ';

  @override
  String get searchTypeArchives => 'Archive';

  @override
  String get searchTypeAudio => 'Audio';

  @override
  String get searchTypeDocuments => 'Dokumente';

  @override
  String get searchTypeFolders => 'Ordner';

  @override
  String get searchTypeImages => 'Bilder';

  @override
  String get searchTypePdf => 'PDFs';

  @override
  String get searchTypeVideos => 'Videos';

  @override
  String get storageAlmostFull => 'Fast voll';

  @override
  String get storageByType => 'Nach Typ';

  @override
  String get storageDetailsUnavailable =>
      'Eine genaue Aufschlüsselung ist gerade nicht verfügbar.';

  @override
  String storageDuplicateGroup(int count, String size) {
    return '$count identische Kopien · je $size';
  }

  @override
  String get storageDuplicates => 'Duplikate';

  @override
  String get storageKeepNewest => 'Neueste behalten';

  @override
  String get storageLargest => 'Größte Dateien';

  @override
  String get storageNewest => 'Neueste';

  @override
  String get storageNoDuplicates => 'Keine identischen Dateien gefunden.';

  @override
  String storageReclaimable(String size) {
    return '$size freigebbar';
  }

  @override
  String get storageTypeOther => 'Sonstiges';

  @override
  String get storageTypeVaults => 'Tresore';

  @override
  String get undoAction => 'Rückgängig';

  @override
  String undoMoved(int count, String folder) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count Elemente nach „$folder“ verschoben',
      one: '1 Element nach „$folder“ verschoben',
    );
    return '$_temp0';
  }

  @override
  String undoRenamed(String name) {
    return 'Umbenannt in „$name“';
  }

  @override
  String undoRestored(String name) {
    return '„$name“ wiederhergestellt';
  }

  @override
  String undoTrashed(int count, String name) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count Elemente in den Papierkorb verschoben',
      one: '„$name“ in den Papierkorb verschoben',
    );
    return '$_temp0';
  }

  @override
  String uploadAnnounceFailed(String name) {
    return 'Upload fehlgeschlagen: $name';
  }

  @override
  String uploadAnnounceFinishedWithFailures(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: 'Uploads fertig, $count fehlgeschlagen',
      one: 'Uploads fertig, 1 fehlgeschlagen',
    );
    return '$_temp0';
  }

  @override
  String uploadConflictApplyToAll(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: 'Für alle $count verbleibenden Uploads übernehmen',
      one: 'Für den 1 verbleibenden Upload übernehmen',
    );
    return '$_temp0';
  }

  @override
  String get uploadConflictExisting => 'SCHON VORHANDEN';

  @override
  String get uploadConflictIncoming => 'NEUER UPLOAD';

  @override
  String uploadConflictKeepBothAs(String name) {
    return 'Beide behalten speichert die neue Datei als „$name“.';
  }

  @override
  String get uploadDismiss => 'Verwerfen';

  @override
  String uploadEtaHours(int count) {
    return 'etwa $count h';
  }

  @override
  String uploadEtaMinutes(int count) {
    return 'etwa $count Min.';
  }

  @override
  String get uploadEtaUnderMinute => 'unter einer Minute';

  @override
  String get uploadFinalizing => 'Wird abgeschlossen…';

  @override
  String uploadInterrupted(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count Uploads wurden unterbrochen',
      one: '1 Upload wurde unterbrochen',
    );
    return '$_temp0';
  }

  @override
  String get uploadInterruptedHint =>
      'Wähle dieselben Dateien erneut, um dort weiterzumachen, wo sie aufgehört haben.';

  @override
  String get uploadPause => 'Pausieren';

  @override
  String get uploadPauseAll => 'Alle pausieren';

  @override
  String get uploadPaused => 'Pausiert';

  @override
  String uploadProgressCount(int done, int total) {
    return '$done von $total';
  }

  @override
  String get uploadResume => 'Fortsetzen';

  @override
  String get uploadResumeAll => 'Alle fortsetzen';

  @override
  String get uploadResumeReselect => 'Fortsetzen (Dateien neu wählen)';

  @override
  String get uploadWaitingForConnection => 'Warte auf Verbindung';

  @override
  String get cameraStatusOff => 'Aus';

  @override
  String get cameraStatusUpToDate => 'Aktuell';

  @override
  String cameraStatusUploading(int current, int total) {
    return 'Lädt $current von $total hoch';
  }

  @override
  String cameraStatusPending(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count Dateien warten',
      one: '1 Datei wartet',
    );
    return '$_temp0';
  }

  @override
  String get cameraStatusWaitingWifi => 'Wartet auf WLAN';

  @override
  String get cameraStatusWaitingCharger => 'Wartet aufs Laden';

  @override
  String get cameraStatusLimited => 'Eingeschränkter Fotozugriff';

  @override
  String get cameraStatusNoAccess => 'Kein Fotozugriff';

  @override
  String get cameraStatusNoFolder => 'Kein Ordner gewählt';

  @override
  String get cameraStatusUnreachable => 'NeoCloud nicht erreichbar';

  @override
  String get cameraStatusSignedOut => 'Abgemeldet';

  @override
  String get cameraStatusServerFull => 'NeoCloud ist voll';

  @override
  String cameraStatusFailed(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count fehlgeschlagen',
      one: '1 fehlgeschlagen',
    );
    return '$_temp0';
  }

  @override
  String get cameraCountOnPhone => 'Auf dem Telefon';

  @override
  String get cameraCountBackedUp => 'Gesichert';

  @override
  String get cameraCountPending => 'Ausstehend';

  @override
  String get cameraCountFailed => 'Fehlgeschlagen';

  @override
  String cameraWaitingCloud(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count warten auf iCloud',
      one: '1 wartet auf iCloud',
    );
    return '$_temp0';
  }

  @override
  String get cameraBackupTitle => 'Fotosicherung';

  @override
  String get cameraBackupEnableDescription =>
      'Sichert die Fotos auf diesem Telefon in NeoCloud. Auf dem Telefon wird nie etwas gelöscht.';

  @override
  String get cameraScopeTitle => 'Was soll gesichert werden?';

  @override
  String get cameraScopeNew => 'Nur neue Fotos';

  @override
  String get cameraScopeNewHint => 'Fotos und Videos ab jetzt.';

  @override
  String cameraScopeEverything(int count, String size) {
    return 'Alles ($count Elemente, etwa $size)';
  }

  @override
  String cameraScopeEverythingCount(int count) {
    return 'Alles ($count Elemente)';
  }

  @override
  String get cameraScopeEverythingHint =>
      'Deine ganze Mediathek, neueste zuerst. Der erste Durchlauf dauert eine Weile.';

  @override
  String get cameraScopeCounting => 'Mediathek wird gezählt…';

  @override
  String get cameraScopeSetting => 'Sichern';

  @override
  String get cameraScopeNewShort => 'Nur neue';

  @override
  String get cameraScopeEverythingShort => 'Alles';

  @override
  String get cameraChargingOnly => 'Nur beim Laden';

  @override
  String get cameraChargingHint =>
      'Im Hintergrund wartet Android aufs Ladekabel. iOS verrät das Apps nicht; dort wird gesichert, wenn du NeoCloud öffnest oder „Jetzt sichern“ tippst.';

  @override
  String get cameraVideosWifiOnly => 'Videos nur im WLAN';

  @override
  String get cameraVideosWifiOnlyHint =>
      'Fotos dürfen mobile Daten nutzen, Videos warten aufs WLAN.';

  @override
  String get cameraSkipSimilar =>
      'Fotos überspringen, die fast gleich sind wie welche, die du schon in NeoCloud hast';

  @override
  String get cameraLimitedMessage =>
      'NeoCloud sieht nur die ausgewählten Fotos. Die anderen werden nicht gesichert.';

  @override
  String get cameraAllowFullAccess => 'Vollen Zugriff erlauben';

  @override
  String get cameraIosBackgroundTip =>
      'iOS führt Hintergrundaufgaben selten aus. Öffne NeoCloud ab und zu, am besten im WLAN und beim Laden.';

  @override
  String get backgroundNotificationChannel => 'Fortschritt der Sicherung';

  @override
  String backgroundNotificationProgress(int current, int total) {
    return 'Sichere $current von $total';
  }

  @override
  String get backgroundNotificationPause => 'Pausieren';

  @override
  String get backgroundNotificationChecking => 'Suche nach neuen Fotos';

  @override
  String get backgroundCameraPaused => 'Pausiert';

  @override
  String get backgroundTitle => 'Sicherung am Laufen halten';

  @override
  String get backgroundIntroAndroid =>
      'Der Kamera-Upload läuft weiter, während NeoCloud geschlossen ist. Um Akku zu sparen, stoppen Android und der Hersteller deines Telefons Apps im Hintergrund; diese Einstellungen halten NeoCloud am Laufen.';

  @override
  String get backgroundIntroIos =>
      'iOS entscheidet, wann NeoCloud im Hintergrund laufen darf: ein paar Mal am Tag, meist nachts am Ladegerät. Diese Einstellungen geben ihm jede Gelegenheit dazu.';

  @override
  String get backgroundStatusGranted => 'Erteilt';

  @override
  String get backgroundStatusNeeded => 'Erforderlich';

  @override
  String get backgroundStatusRecommended => 'Empfohlen';

  @override
  String get backgroundStatusOff => 'Aus';

  @override
  String get backgroundPhotosTitle => 'Fotos und Videos';

  @override
  String get backgroundPhotosDetail =>
      'Zugriff auf die ganze Mediathek, nicht nur auf ausgewählte Fotos, damit jedes neue Foto gesichert wird.';

  @override
  String get backgroundBatteryTitle => 'Uneingeschränkter Akku';

  @override
  String get backgroundBatteryDetail =>
      'Lässt Android NeoCloud im Hintergrund laufen und eine Sicherung kurz nach jedem Foto starten.';

  @override
  String backgroundManufacturerTitle(String maker) {
    return 'Einstellungen von $maker';
  }

  @override
  String backgroundManufacturerDetail(String maker) {
    return '$maker stoppt Apps im Hintergrund zusätzlich zu Android. Ändere diese Einstellungen, damit NeoCloud weiterläuft:';
  }

  @override
  String get backgroundManufacturerGuide =>
      'Anleitung für dein Telefon auf dontkillmyapp.com';

  @override
  String get backgroundNotificationsTitle => 'Mitteilungen';

  @override
  String get backgroundNotificationsDetailAndroid =>
      'Zeigt den Fortschritt der Sicherung, mit einer Pause-Taste.';

  @override
  String get backgroundNotificationsDetailIos =>
      'Damit NeoCloud dir Bescheid geben kann, wenn die Sicherung dich braucht.';

  @override
  String get backgroundDataTitle => 'Hintergrunddaten';

  @override
  String get backgroundDataDetail =>
      'Der Datensparmodus blockiert Uploads, solange NeoCloud geschlossen ist, außer NeoCloud darf Daten uneingeschränkt nutzen.';

  @override
  String get backgroundRefreshTitle => 'Hintergrundaktualisierung';

  @override
  String get backgroundRefreshDetail =>
      'Ohne sie führt iOS NeoCloud nie im Hintergrund aus.';

  @override
  String get backgroundRefreshRestricted =>
      'Durch Bildschirmzeit oder ein Geräteprofil ausgeschaltet; nur dort lässt sie sich wieder einschalten.';

  @override
  String get backgroundLowPowerTitle => 'Stromsparmodus';

  @override
  String get backgroundLowPowerDetail =>
      'Solange er an ist, pausiert iOS Hintergrundaufgaben. Schalte ihn unter Einstellungen › Batterie aus, damit eine Sicherung laufen kann.';

  @override
  String get backgroundSwipeHint =>
      'Wische NeoCloud nicht im App-Umschalter weg: iOS stoppt dann seine Hintergrundaufgaben, bis du die App wieder öffnest.';

  @override
  String get backgroundAllow => 'Erlauben';

  @override
  String get backgroundAllowAll => 'Alle erlauben';

  @override
  String get backgroundOpenSettings => 'Einstellungen öffnen';

  @override
  String get backgroundNotNow => 'Jetzt nicht';

  @override
  String get backgroundBannerTitle =>
      'Sicherung läuft im Hintergrund womöglich nicht';

  @override
  String get backgroundBannerMissing =>
      'Etwas, das NeoCloud für den Hintergrund braucht, ist ausgeschaltet.';

  @override
  String backgroundBannerStale(String date) {
    return 'Keine vollständige Sicherung seit $date. Das System stoppt NeoCloud im Hintergrund vermutlich.';
  }

  @override
  String get backgroundBannerFix => 'Einstellungen prüfen';

  @override
  String get backgroundReliabilityTitle => 'Zuverlässigkeit im Hintergrund';

  @override
  String get backgroundReliabilityAllSet =>
      'Alles, was NeoCloud braucht, ist eingestellt.';

  @override
  String backgroundReliabilityOpen(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count Einstellungen zu prüfen',
      one: '1 Einstellung zu prüfen',
    );
    return '$_temp0';
  }

  @override
  String get backgroundReliabilityCheck => 'Prüfen';

  @override
  String get backgroundStepSamsungUnrestricted =>
      'Einstellungen › Apps › NeoCloud › Akku › Nicht eingeschränkt';

  @override
  String get backgroundStepSamsungNeverSleeping =>
      'Einstellungen › Akku › Einschränkungen der Hintergrundnutzung › Nie in Standby versetzte Apps › NeoCloud hinzufügen';

  @override
  String get backgroundStepXiaomiAutostart =>
      'Einstellungen › Apps › Apps verwalten › NeoCloud › Autostart einschalten';

  @override
  String get backgroundStepXiaomiNoRestrictions =>
      'Einstellungen › Apps › Apps verwalten › NeoCloud › Energiesparmodus › Keine Einschränkungen';

  @override
  String get backgroundStepHuaweiAppLaunch =>
      'Einstellungen › Akku › App-Start › NeoCloud › Manuell verwalten, mit Automatischem Start, Sekundärem Start und Im Hintergrund ausführen an';

  @override
  String get backgroundStepOppoBatteryUsage =>
      'Einstellungen › Apps › NeoCloud › Akkunutzung › Hintergrundaktivität und automatischen Start erlauben';

  @override
  String get backgroundStepOppoDontOptimise =>
      'Einstellungen › Akku › Akkuoptimierung › NeoCloud › Nicht optimieren';

  @override
  String get backgroundStepVivoBackgroundPower =>
      'Einstellungen › Akku › Verwaltung des Hintergrundverbrauchs › NeoCloud › Erlauben';

  @override
  String get backgroundStepVivoAutostart =>
      'i Manager › App-Manager › Autostart-Manager › NeoCloud einschalten';

  @override
  String get backgroundStepAsusAutostart =>
      'Mobile Manager › PowerMaster › Autostart-Manager › NeoCloud › Erlauben';

  @override
  String get backgroundStepLockInRecents =>
      'Öffne die zuletzt verwendeten Apps und sperre NeoCloud dort, damit es beim Aufräumen weiterläuft';

  @override
  String get cameraBackUpNow => 'Jetzt sichern';

  @override
  String get cameraRetryFailed => 'Fehlgeschlagene wiederholen';

  @override
  String get cameraExportLog => 'Upload-Protokoll exportieren';

  @override
  String get cameraLogTitle => 'Upload-Protokoll';

  @override
  String get cameraLogEmpty => 'Noch nichts protokolliert.';

  @override
  String get cameraListEmpty => 'Nichts vorhanden.';

  @override
  String get cameraReasonWifi => 'wartet auf WLAN';

  @override
  String get cameraReasonIcloud => 'wartet auf iCloud';

  @override
  String get cameraReasonNotAvailable =>
      'das System konnte die Datei nicht liefern';

  @override
  String get cameraReasonSimilar =>
      'ähnlich wie ein Foto, das schon in NeoCloud ist';

  @override
  String cameraAttempts(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count Versuche',
      one: '1 Versuch',
    );
    return '$_temp0';
  }

  @override
  String get cameraNeverRun => 'Noch nicht gelaufen';

  @override
  String get cameraUnnamed => 'Noch nicht gelesen';

  @override
  String get cameraOptions => 'Optionen';

  @override
  String mobilePermissionsSet(int granted, int total) {
    return '$granted von $total erledigt';
  }

  @override
  String mobilePermissionStep(int step, int total) {
    return 'Schritt $step von $total';
  }

  @override
  String mobileQrLoginExpiresAt(String time) {
    return 'Läuft ab $time';
  }

  @override
  String get mobileRecentServers => 'Zuletzt verwendet';

  @override
  String get mobileForgetServer => 'Vergessen';

  @override
  String get filesQuickAccess => 'Schnellzugriff';

  @override
  String get filesMoreActions => 'Weitere Aktionen';

  @override
  String filesMemberRemoveConfirm(String name, String item) {
    return '$name kann „$item“ danach nicht mehr öffnen.';
  }

  @override
  String get done => 'Fertig';

  @override
  String deviceRevokeTitle(String name) {
    return '$name trennen?';
  }

  @override
  String get deviceRevokeMessage =>
      'Die App auf diesem Gerät wird abgemeldet und muss neu verbunden werden.';

  @override
  String get qrPairing => 'QR-Pairing';

  @override
  String get pairPhoneDescription =>
      'Zeigt einen QR-Code an, mit dem sich die NeoCloud-App auf einem Telefon bei diesem Konto anmeldet, ohne Adresse oder Passwort einzugeben.';

  @override
  String get showQrCode => 'QR-Code anzeigen';

  @override
  String get pairPhone => 'Smartphone koppeln';

  @override
  String get pairTitle => 'Smartphone koppeln';

  @override
  String get pairInstructions =>
      'Öffne die NeoCloud-App auf deinem Smartphone, tippe auf „QR-Code scannen“ und richte die Kamera auf diesen Code.';

  @override
  String get pairQrSemantics => 'QR-Code zum Koppeln eines Smartphones';

  @override
  String get pairWaiting => 'Warte auf das Smartphone';

  @override
  String pairExpiresIn(String time) {
    return 'Läuft ab in $time';
  }

  @override
  String get pairExpired => 'Code abgelaufen';

  @override
  String get pairNewCode => 'Neuer Code';

  @override
  String pairPairedWith(String device) {
    return 'Gekoppelt mit $device ✓';
  }

  @override
  String get pairUnknownDevice => 'deinem Smartphone';

  @override
  String get pairCantScan =>
      'Scannen klappt nicht? Öffne diesen Link auf dem Smartphone:';

  @override
  String get pairLandingTitle => 'Mit der NeoCloud-App koppeln';

  @override
  String get pairLandingPhoneBody =>
      'Dieser Link meldet die NeoCloud-App auf diesem Smartphone an. Installiere die App, falls noch nicht geschehen, und öffne den Link darin. Der Code gilt fünf Minuten.';

  @override
  String get pairLandingOpenApp => 'In der NeoCloud-App öffnen';

  @override
  String get pairLandingAppStore => 'Im App Store laden';

  @override
  String get pairLandingPlayStore => 'Bei Google Play laden';

  @override
  String get pairLandingComputerBody =>
      'Dieser Link ist für dein Smartphone gedacht. Öffne ihn dort oder scanne den QR-Code unter Einstellungen › Anmeldung & Sicherheit mit der NeoCloud-App.';

  @override
  String get pairLandingInvalid =>
      'Dieser Kopplungslink ist unvollständig. Lass dir unter Einstellungen › Anmeldung & Sicherheit einen neuen Code anzeigen.';

  @override
  String get connectOr => 'oder';

  @override
  String get connectScanQr => 'QR-Code scannen';

  @override
  String get connectScanQrHint =>
      'Öffne in der Web-App Einstellungen › Anmeldung & Sicherheit › Smartphone koppeln.';

  @override
  String get scanTitle => 'QR-Code scannen';

  @override
  String get scanPrimeTitle => 'Mit der Kamera scannen';

  @override
  String get scanPrimeBody =>
      'NeoCloud braucht die Kamera nur, um NeoCloud-QR-Codes zu lesen. Es wird nichts aufgenommen oder hochgeladen.';

  @override
  String get scanPrimeAllow => 'Kamera erlauben';

  @override
  String get scanHint =>
      'Richte die Kamera auf den QR-Code unter Einstellungen › Anmeldung & Sicherheit';

  @override
  String get scanWrongCode => 'Das ist kein NeoCloud-QR-Code';

  @override
  String get scanTorchOn => 'Licht einschalten';

  @override
  String get scanTorchOff => 'Licht ausschalten';

  @override
  String get scanDeniedTitle => 'Kamerazugriff ist aus';

  @override
  String get scanDeniedBodyIos =>
      'Erlaube NeoCloud in den Einstellungen den Zugriff auf die Kamera oder verbinde dich stattdessen mit der Serveradresse.';

  @override
  String get scanDeniedBodyAndroid =>
      'Erlaube die Kamera bei der Nachfrage oder schalte sie unter Einstellungen › Apps › NeoCloud › Berechtigungen ein. Du kannst dich auch mit der Serveradresse verbinden.';

  @override
  String get scanOpenSettings => 'Einstellungen öffnen';

  @override
  String get scanFailedTitle => 'Die Kamera ist nicht gestartet';

  @override
  String get scanFailedBody =>
      'Schließe andere Apps, die die Kamera nutzen, und versuche es erneut.';

  @override
  String get webDavTitle => 'Apps verbinden (WebDAV)';

  @override
  String get webDavDescription =>
      'Finder, Windows Explorer, rclone und viele andere Apps öffnen NeoCloud als Netzlaufwerk. Sie melden sich mit einem App-Passwort an, nicht mit deinem Kontopasswort.';

  @override
  String get webDavAddress => 'WebDAV-Adresse';

  @override
  String get webDavPassword => 'App-Passwort';

  @override
  String get webDavPasswordHint =>
      'Eines pro App, damit du jede einzeln trennen kannst. App-Passwörter stehen oben mit dem Etikett WebDAV.';

  @override
  String get webDavCreatePassword => 'App-Passwort erstellen';

  @override
  String get webDavPasswordName => 'Für welche App?';

  @override
  String get webDavPasswordDefaultName => 'WebDAV';

  @override
  String get webDavPasswordTitle => 'Dein App-Passwort';

  @override
  String get webDavPasswordMessage =>
      'Gib es zusammen mit deinem Benutzernamen in der App ein. Es wird nur einmal angezeigt.';

  @override
  String get webDavRevokeMessage =>
      'Apps mit diesem App-Passwort erreichen deine Dateien nicht mehr.';

  @override
  String get webDavTag => 'WebDAV';

  @override
  String get webDavHowTo => 'So verbindest du gängige Apps';

  @override
  String get webDavHintFinderTitle => 'Finder (macOS)';

  @override
  String get webDavHintFinder =>
      'Gehe zu › Mit Server verbinden (⌘K), füge die Adresse ein und melde dich mit Benutzername und App-Passwort an.';

  @override
  String get webDavHintWindowsTitle => 'Windows';

  @override
  String get webDavHintWindows =>
      'Im Explorer: Rechtsklick auf Dieser PC › Netzlaufwerk verbinden, Adresse einfügen und „Verbindung mit anderen Anmeldeinformationen herstellen“ wählen. Erfordert HTTPS.';

  @override
  String get webDavHintRcloneTitle => 'rclone';

  @override
  String get webDavHintRclone =>
      'rclone config › New remote › WebDAV, Anbieter „owncloud“, dann Adresse, Benutzername und App-Passwort.';

  @override
  String get webDavHintObsidianTitle => 'Obsidian';

  @override
  String get webDavHintObsidian =>
      'Installiere das Plugin Remotely Save, wähle WebDAV und gib Adresse, Benutzername und App-Passwort ein.';

  @override
  String get webDavHintKeepassTitle => 'KeePassXC und KeePass-Apps';

  @override
  String get webDavHintKeepass =>
      'Öffne die Datenbank in KeePassXC vom verbundenen Laufwerk; auf Smartphones öffnen KeePassDX und Strongbox WebDAV-Adressen direkt.';

  @override
  String get pageImport => 'Import';

  @override
  String get pageImportDescription =>
      'Hol deine Dateien von Nextcloud oder einem anderen WebDAV-Server herüber.';

  @override
  String get importNextcloudTitle => 'Von Nextcloud umziehen';

  @override
  String get importNextcloudDescription =>
      'NeoCloud kopiert deine Dateien samt Ordnern und Datumsangaben. Auf dem anderen Server wird nichts verändert.';

  @override
  String get importServerUrl => 'Serveradresse';

  @override
  String get importAppPassword => 'App-Passwort';

  @override
  String get importAppPasswordHelpLink => 'Wie erstelle ich ein App-Passwort?';

  @override
  String get importAppPasswordHelpTitle => 'App-Passwort in Nextcloud';

  @override
  String get importAppPasswordHelp =>
      'Öffne in Nextcloud dein Profilbild › Persönliche Einstellungen › Sicherheit. Gib unter „Geräte & Sitzungen“ einen Namen wie NeoCloud ein und klicke auf „Neues App-Passwort erstellen“. Kopiere das angezeigte Passwort und füge es hier ein. NeoCloud speichert es verschlüsselt und löscht es, wenn der Import endet.';

  @override
  String get importAppPasswordDocs => 'Nextcloud-Hilfe';

  @override
  String get importSource => 'Quellordner';

  @override
  String get importSourceHint =>
      'Der Ordner auf dem anderen Server; / übernimmt alles.';

  @override
  String get importTarget => 'Importieren nach';

  @override
  String get importChooseFolder => 'Ordner wählen';

  @override
  String get importTest => 'Verbindung testen';

  @override
  String importTestFound(String files, String size) {
    return 'Verbunden. $files Einträge in diesem Ordner, insgesamt $size.';
  }

  @override
  String get importTestFailed =>
      'Der Server konnte nicht gelesen werden. Prüfe Adresse, Benutzername und App-Passwort.';

  @override
  String importQuotaWarning(String size) {
    return 'Das passt nicht: Es fehlen $size Speicherplatz. Der Import hält an, sobald der Speicher voll ist.';
  }

  @override
  String get importStart => 'Import starten';

  @override
  String get importStarted =>
      'Import gestartet. Du kannst die Seite verlassen; er läuft weiter.';

  @override
  String get importJobsTitle => 'Importe';

  @override
  String get importJobsEmpty => 'Noch keine Importe.';

  @override
  String importInto(String folder) {
    return 'Nach $folder';
  }

  @override
  String importFilesOf(String done, String total) {
    return '$done von $total Dateien';
  }

  @override
  String importFilesDone(String done) {
    return '$done Dateien';
  }

  @override
  String importBytesOf(String done, String total) {
    return '$done von $total';
  }

  @override
  String importSkipped(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count übersprungen (schon vorhanden)',
      one: '1 übersprungen (schon vorhanden)',
    );
    return '$_temp0';
  }

  @override
  String importFailed(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count fehlgeschlagen',
      one: '1 fehlgeschlagen',
    );
    return '$_temp0';
  }

  @override
  String importShowErrors(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count Fehler anzeigen',
      one: '1 Fehler anzeigen',
    );
    return '$_temp0';
  }

  @override
  String get importHideErrors => 'Fehler ausblenden';

  @override
  String get importPause => 'Pausieren';

  @override
  String get importResume => 'Fortsetzen';

  @override
  String get importCancel => 'Import abbrechen';

  @override
  String get importCancelTitle => 'Diesen Import abbrechen?';

  @override
  String get importCancelMessage =>
      'Bereits kopierte Dateien bleiben in NeoCloud. Du kannst später fortsetzen; vorhandene Dateien werden übersprungen.';

  @override
  String get importOpenFolder => 'Ordner öffnen';

  @override
  String get importRemove => 'Aus der Liste entfernen';

  @override
  String get importStatusQueued => 'In Warteschlange';

  @override
  String get importStatusScanning => 'Wird durchsucht';

  @override
  String get importStatusRunning => 'Wird importiert';

  @override
  String get importStatusPaused => 'Pausiert';

  @override
  String get importStatusDone => 'Fertig';

  @override
  String get importStatusFailed => 'Fehlgeschlagen';

  @override
  String get importStatusCancelled => 'Abgebrochen';

  @override
  String get filesWelcomeImportBody =>
      'Importiere deine Dateien samt Ordnern und Datumsangaben.';

  @override
  String get filesWelcomeImportAction => 'Dateien importieren';

  @override
  String get searchFileContents => 'Dateiinhalte';

  @override
  String get searchFileContentsHint =>
      'Dateien auch nach den Wörtern darin finden';

  @override
  String get searchMatchName => 'Name';

  @override
  String get searchMatchContent => 'Inhalt';

  @override
  String get searchMatchBoth => 'Name + Inhalt';

  @override
  String get searchSettingsTitle => 'Suche';

  @override
  String get searchSettingsContents => 'In meinen Dateien suchen';

  @override
  String get searchSettingsContentsHint =>
      'NeoCloud indexiert den Text in deinen Dokumenten, damit die Suche Wörter darin findet. Schalte das aus, um deine Dateien aus dem Index herauszuhalten.';

  @override
  String get searchSettingsContentsOff =>
      'Die Suche in Dokumenten ist in deiner NeoCloud noch nicht eingeschaltet. Sobald sie es ist, entscheidet diese Option, ob deine Dateien dabei sind.';

  @override
  String get adminSearchTitle => 'Volltextsuche';

  @override
  String get adminSearchDescription =>
      'Damit finden Nutzer Dateien über die Wörter darin: Text-, PDF- und Office-Dokumente. Dateien in Tresoren werden nie indexiert.';

  @override
  String get adminSearchToggle => 'Volltextsuche';

  @override
  String get adminSearchPrivacy =>
      'Abwägung: Der Index speichert, welche Wörter in jeder Datei vorkommen. Der Text selbst ist verschlüsselt, aber wer die Datenbank hat, kann erkennen, ob eine Datei ein bestimmtes Wort enthält. Jede Person kann sich in ihren Einstellungen abmelden.';

  @override
  String get adminSearchState => 'Index';

  @override
  String get adminSearchOff => 'Aus';

  @override
  String get adminSearchIndexing => 'Wird indexiert';

  @override
  String get adminSearchUpToDate => 'Aktuell';

  @override
  String get adminSearchIndexed => 'Indexierte Dateien';

  @override
  String get adminSearchPending => 'Wartend';

  @override
  String get adminSearchFailed => 'Fehlgeschlagen';

  @override
  String get adminSearchFailedHint =>
      'Beschädigte oder passwortgeschützte Dateien können nicht gelesen werden.';

  @override
  String get adminSearchSize => 'Indexgröße';

  @override
  String get adminSearchPdf => 'PDF-Text';

  @override
  String get adminSearchPdfFallback =>
      'Installiere poppler-utils für besseren PDF-Text; bis dahin wird der eingebaute Leser verwendet.';

  @override
  String get adminSearchOffice => 'Office-Dokumente';

  @override
  String get adminSearchToolPdftotext => 'pdftotext';

  @override
  String get adminSearchToolAvailable => 'Verfügbar';

  @override
  String get adminSearchToolMissing => 'Nicht installiert';

  @override
  String get adminSearchRebuild => 'Index neu aufbauen';

  @override
  String get adminSearchRebuildHint =>
      'Liest alle Dateien im Hintergrund neu ein.';

  @override
  String get adminSearchRebuildStarted =>
      'Der Index wird im Hintergrund neu aufgebaut.';

  @override
  String get importTargetHint =>
      'Ohne Auswahl landen die Dateien in einem neuen Ordner „Nextcloud“ unter Start.';

  @override
  String get importHostBlocked =>
      'NeoCloud kann nicht von einer Adresse in deinem lokalen Netzwerk importieren.';

  @override
  String get filesWelcomeDontShowAgain => 'Nicht mehr anzeigen';

  @override
  String get filesQuickAccessShow => 'Schnellzugriff anzeigen';

  @override
  String get filesQuickAccessHide => 'Schnellzugriff ausblenden';

  @override
  String get adminSafetyAllDone => 'Alles erledigt';

  @override
  String adminSafetyProgress(int done, int total) {
    return '$done von $total erledigt';
  }

  @override
  String get adminSafetyExpand => 'Checkliste anzeigen';

  @override
  String get adminSafetyCollapse => 'Checkliste ausblenden';

  @override
  String get authSignInWithSecurityKey => 'Mit Sicherheitsschlüssel anmelden';

  @override
  String get authOrContinueWith => 'oder fortfahren mit';

  @override
  String get passkeyWebOnly =>
      'Sicherheitsschlüssel sind nur in der NeoCloud-Web-App verfügbar.';

  @override
  String get passkeyNoneProvided =>
      'Es wurde kein Sicherheitsschlüssel angegeben.';

  @override
  String get passkeyPromptDismissed =>
      'Die Aufforderung zum Sicherheitsschlüssel wurde abgebrochen.';

  @override
  String get passkeyAlreadyRegistered =>
      'Dieser Sicherheitsschlüssel ist bereits für dieses Konto registriert.';

  @override
  String get passkeyCouldNotBeUsed =>
      'Der Sicherheitsschlüssel konnte nicht verwendet werden.';

  @override
  String get passkeySignInFailed =>
      'Anmeldung mit Sicherheitsschlüssel konnte nicht gestartet werden.';

  @override
  String get passkeyRegistrationFailed =>
      'Die Sicherheitsschlüssel-Registrierung konnte nicht gestartet werden.';

  @override
  String get passkeysTitle => 'Sicherheitsschlüssel';

  @override
  String get passkeysDescription =>
      'Melde dich mit einem Hardware-Schlüssel oder Passkey statt mit deinem Passwort an. Ein Schlüssel, der eine PIN oder einen Fingerabdruck verlangt, ersetzt auch deinen Zwei-Faktor-Code.';

  @override
  String get passkeysNone => 'Keine';

  @override
  String passkeysRegistered(int count) {
    return '$count registriert';
  }

  @override
  String get passkeyAdd => 'Sicherheitsschlüssel hinzufügen';

  @override
  String get passkeyNameOptional => 'Name (optional)';

  @override
  String get passkeyNameHint => 'YubiKey, MacBook Touch ID, …';

  @override
  String passkeyDefaultName(int number) {
    return 'Sicherheitsschlüssel $number';
  }

  @override
  String get passkeyRenameTitle => 'Sicherheitsschlüssel umbenennen';

  @override
  String get passkeyName => 'Name';

  @override
  String get passkeyRemove => 'Entfernen';

  @override
  String settingsPasskeyRemoveTitle(String name) {
    return '„$name“ entfernen?';
  }

  @override
  String get settingsPasskeyRemoveMessage =>
      'Mit diesem Schlüssel kannst du dich nicht mehr anmelden. Behalte mindestens einen anderen Weg ins Konto.';

  @override
  String passkeyAddedLastUsed(String added, String lastUsed) {
    return 'Hinzugefügt $added · Zuletzt verwendet $lastUsed';
  }

  @override
  String get passkeyNotRecorded => 'Nicht erfasst';

  @override
  String get passkeyNeverUsed => 'Noch nie verwendet';

  @override
  String get passkeyUnsupported =>
      'Dieses Gerät kann keine Sicherheitsschlüssel registrieren. Öffne NeoCloud in einem Browser über HTTPS, um einen hinzuzufügen.';

  @override
  String get passkeyAddInBrowser =>
      'Sicherheitsschlüssel und Passkeys fügst du in der NeoCloud-Web-App im Browser hinzu. Dort hinzugefügte Schlüssel stehen hier, und du kannst sie umbenennen oder entfernen.';

  @override
  String get navSharedWithMe => 'Für mich freigegeben';

  @override
  String get sharedWithMeSubtitle =>
      'Dateien und Ordner, die andere auf NeoCloud für dich freigegeben haben.';

  @override
  String get sharedWithMeEmpty => 'Noch nichts für dich freigegeben';

  @override
  String get sharedWithMeEmptySubtitle =>
      'Wenn jemand eine Datei oder einen Ordner für dich freigibt, erscheint sie hier.';

  @override
  String sharedBy(String owner) {
    return 'Freigegeben von $owner';
  }

  @override
  String sharedByCanEdit(String owner) {
    return 'Freigegeben von $owner · du kannst bearbeiten';
  }

  @override
  String sharedByReadOnly(String owner) {
    return 'Freigegeben von $owner · nur ansehen';
  }

  @override
  String get sharedWithPeople => 'Mit Personen geteilt';

  @override
  String get sharedLeave => 'Freigabe verlassen';

  @override
  String sharedLeaveConfirm(String name, String owner) {
    return 'Du siehst „$name“ dann nicht mehr. Nur $owner kann es wieder für dich freigeben.';
  }

  @override
  String get sharedSaveToMyFiles => 'In meine Dateien kopieren';

  @override
  String sharedSavedToMyFiles(String name) {
    return '„$name“ in deine Dateien kopiert.';
  }

  @override
  String get sharedReadOnlyError =>
      'Dieser Ordner ist für dich nur zum Ansehen freigegeben.';

  @override
  String get sharedBoundaryError =>
      'Elemente können nicht in einen freigegebenen Ordner hinein oder aus ihm heraus verschoben werden. Kopiere sie stattdessen.';

  @override
  String get sharedRootError =>
      'Der freigegebene Ordner selbst gehört seinem Besitzer.';

  @override
  String get sharedZipMixedError =>
      'Lade eigene und freigegebene Dateien getrennt herunter.';

  @override
  String get sharePeopleTitle => 'Mit Personen teilen';

  @override
  String get sharePeopleHelp =>
      'Gib die genaue E-Mail-Adresse oder den Benutzernamen einer Person ein, die diese NeoCloud nutzt.';

  @override
  String get sharePublicLinkTitle => 'Öffentlicher Link';

  @override
  String get memberRecipientHint => 'E-Mail-Adresse oder Benutzername';

  @override
  String get memberAdd => 'Hinzufügen';

  @override
  String get memberCanView => 'Kann ansehen';

  @override
  String get memberCanEdit => 'Kann bearbeiten';

  @override
  String get memberChange => 'Zugriff ändern';

  @override
  String get memberRemove => 'Zugriff entfernen';

  @override
  String get memberExpired => 'Abgelaufen';

  @override
  String get membersEmpty => 'Noch mit niemandem geteilt.';

  @override
  String get memberNotFound =>
      'Kein Konto gefunden, mit dem das geteilt werden kann. Prüfe die genaue E-Mail-Adresse oder den Benutzernamen.';

  @override
  String get memberAlreadyAdded => 'Bereits mit dieser Person geteilt.';

  @override
  String get authOrUsePassword => 'oder mit Passwort';

  @override
  String get qrLoginScanTitle => 'Mit NeoCloud auf dem Smartphone scannen';

  @override
  String get qrLoginScanBody =>
      'Öffne die NeoCloud-App auf einem angemeldeten Smartphone und tippe auf QR-Code scannen.';

  @override
  String qrLoginRefreshesIn(int seconds) {
    return 'Neuer Code in $seconds s';
  }

  @override
  String get qrLoginPreparing => 'Code wird vorbereitet …';

  @override
  String get qrLoginNewCode => 'Neuer Code';

  @override
  String get qrLoginShowCode => 'Mit dem Smartphone anmelden';

  @override
  String get qrLoginWaitingTitle => 'Schau auf dein Smartphone';

  @override
  String get qrLoginWaitingBody =>
      'Bestätige die Anmeldung auf deinem Smartphone, um hier weiterzumachen.';

  @override
  String get qrLoginSigningInOn => 'Anmeldung auf';

  @override
  String get qrLoginFrom => 'Von';

  @override
  String get qrLoginWaitingForApproval => 'Warte auf Bestätigung';

  @override
  String get qrLoginCancelAndUsePassword => 'Abbrechen und Passwort verwenden';

  @override
  String get qrLoginCouldNotStart =>
      'Der QR-Code konnte nicht vorbereitet werden.';

  @override
  String get qrLoginRateLimited =>
      'Zu viele QR-Codes in kurzer Zeit. Warte einen Moment und versuche es dann erneut.';

  @override
  String get qrLoginDeclinedOnPhone =>
      'Die Anmeldung wurde auf dem Smartphone abgelehnt.';

  @override
  String get qrLoginExpired => 'Der Code ist abgelaufen. Scanne den neuen.';

  @override
  String get scanQrSubtitle =>
      'Einen Browser anmelden oder ein Gerät bestätigen';

  @override
  String get scanAnyHint =>
      'Richte die Kamera auf den QR-Code auf dem anderen Gerät';

  @override
  String get qrLoginApproveTitle => 'Anmeldung bestätigen?';

  @override
  String qrLoginApproveHint(String account) {
    return 'Bestätige nur, wenn du diese Anmeldung selbst gestartet hast. Sie wird als $account angemeldet.';
  }

  @override
  String get qrLoginApprove => 'Bestätigen';

  @override
  String get qrLoginDecline => 'Ablehnen';

  @override
  String qrLoginRequestedAt(String time) {
    return 'Angefragt $time';
  }

  @override
  String get qrLoginPreviewExpired =>
      'Diese Anfrage ist abgelaufen. Zeige auf dem anderen Gerät einen neuen Code an.';

  @override
  String get qrLoginPreviewUsed => 'Diese Anfrage wurde bereits verwendet.';

  @override
  String qrLoginApproved(String device) {
    return '$device ist angemeldet.';
  }

  @override
  String get qrLoginDeclined => 'Anmeldung abgelehnt.';

  @override
  String get qrLoginCouldNotApprove =>
      'Die Anmeldung konnte nicht bestätigt werden.';

  @override
  String scanOtherServer(String server) {
    return 'Dieser Code gehört zu einem anderen Server ($server).';
  }

  @override
  String get cameraOfferTitle => 'Fotos sichern?';

  @override
  String get cameraOfferBody =>
      'NeoCloud kann neue Fotos und Videos von diesem Smartphone automatisch hochladen, verschlüsselt wie jede andere Datei.';

  @override
  String get cameraOfferSetUp => 'Einrichten';

  @override
  String get notNow => 'Nicht jetzt';

  @override
  String get connectWithBrowser => 'Im Browser anmelden';

  @override
  String get connectChangeServer => 'Server wechseln';

  @override
  String get signInInvalidCredentials =>
      'Benutzername, E-Mail oder Passwort ist falsch.';

  @override
  String get signInInvalidCode =>
      'Der Code stimmt nicht. Prüfe deine Authenticator-App und versuche es erneut.';

  @override
  String signInLockedMinutes(int minutes) {
    return 'Zu viele fehlgeschlagene Anmeldeversuche für dieses Konto. Versuche es in $minutes Min. erneut.';
  }

  @override
  String signInLockedSeconds(int seconds) {
    return 'Zu viele fehlgeschlagene Anmeldeversuche für dieses Konto. Versuche es in $seconds s erneut.';
  }

  @override
  String get signInRateLimited =>
      'Zu viele Versuche. Warte ein paar Minuten und versuche es dann erneut.';

  @override
  String confirmLockedMinutes(int minutes) {
    return 'Zu viele falsche Passwörter oder Codes für dieses Konto. Versuche es in $minutes Min. erneut.';
  }

  @override
  String confirmLockedSeconds(int seconds) {
    return 'Zu viele falsche Passwörter oder Codes für dieses Konto. Versuche es in $seconds s erneut.';
  }

  @override
  String get passkeyConfirmMessage =>
      'Ein Sicherheitsschlüssel meldet dich allein an und bleibt auch nach einer Passwortänderung bestehen. Bestätige deshalb zuerst, dass du es bist.';

  @override
  String get approvalNeedsServerUpdate =>
      'Anmeldungen in der App zu bestätigen ist noch nicht verfügbar. Bestätige stattdessen im Browser, in dem du angemeldet bist.';

  @override
  String get serverUpdateNeeded => 'Diese Funktion ist noch nicht verfügbar.';

  @override
  String get approvalNeedsPhone =>
      'Bestätige Anmeldungen in der NeoCloud-App auf dem Handy oder in einem Browser, in dem du angemeldet bist.';

  @override
  String get connectWelcome => 'WILLKOMMEN BEI NEOCLOUD';

  @override
  String get connectReadyToSignIn => 'Bereit zur Anmeldung';

  @override
  String get connectEnterAddress => 'Adresse manuell eingeben';

  @override
  String get connectEnterAddressHint =>
      'Die Adresse deines Servers, z. B. cloud.example.com';

  @override
  String get connectServerAddress => 'NeoCloud-Serveradresse';

  @override
  String get connectConnecting => 'Verbindung wird hergestellt…';

  @override
  String get connectToThisServer => 'Mit diesem Server verbinden';

  @override
  String get connectForgetServerBody =>
      'Diesen Server aus der Liste entfernen? Du kannst seine Adresse weiterhin eingeben.';

  @override
  String authSigningInTo(String host) {
    return 'Anmeldung bei $host';
  }

  @override
  String get authShowPassword => 'Passwort anzeigen';

  @override
  String get authHidePassword => 'Passwort verbergen';

  @override
  String get authForgotPasswordAskAdmin =>
      'Passwort vergessen? Bitte die Person, die deine NeoCloud betreibt, es zurückzusetzen.';

  @override
  String get authPhoneSignInTitle => 'Bei NeoCloud anmelden';

  @override
  String get authPhoneSignInSubtitle =>
      'Scanne den Code auf einem angemeldeten Computer oder nutze dein Passwort.';

  @override
  String get aiPage => 'KI';

  @override
  String get aiPageDescription =>
      'Lass dir von KI Namen, Ordner und Schlagwörter für deine Dateien vorschlagen.';

  @override
  String get aiOptInTitle => 'KI-Funktionen';

  @override
  String get aiOptIn => 'KI-Funktionen nutzen';

  @override
  String get aiPrivacyNote =>
      'Wenn die KI eine Datei ansieht, gehen Name, Text und Vorschau an den KI-Dienst. Dateien im Tresor nie, und nichts ändert sich, bevor du einen Vorschlag annimmst.';

  @override
  String get aiModelsTitle => 'Modelle';

  @override
  String get aiModelsDescription =>
      'Welches KI-Modell welche Aufgabe übernimmt. Auto wählt das beste verfügbare.';

  @override
  String get aiRoleText => 'Alltägliche Aufgaben';

  @override
  String get aiRoleTextDescription => 'Namen, Schlagwörter und Sortieren.';

  @override
  String get aiRoleSmart => 'Zusammenfassungen';

  @override
  String get aiRoleSmartDescription =>
      'Zusammenfassungen und Fragen zu einer Datei.';

  @override
  String get aiRoleVision => 'Bilder';

  @override
  String get aiRoleVisionDescription =>
      'Liest Fotos, Screenshots und Scans. Nur Modelle, die Bilder sehen können.';

  @override
  String get aiRoleEmbedding => 'Ähnliche Dateien';

  @override
  String get aiRoleEmbeddingDescription =>
      'Findet ähnliche Dateien und Ordner fürs Sortieren.';

  @override
  String get aiRoleNotReady => 'Dafür ist noch kein Modell verfügbar.';

  @override
  String get aiAuto => 'Auto';

  @override
  String get aiAutoSubtitle => 'Das beste verfügbare Modell';

  @override
  String aiSelectModel(String name) {
    return '$name auswählen';
  }

  @override
  String get aiSearchModels => 'Suchen…';

  @override
  String aiNoResults(String query) {
    return 'Keine Ergebnisse für \"$query\"';
  }

  @override
  String get aiNoModelsForRole => 'Noch kann kein Modell das.';

  @override
  String get aiAll => 'Alle';

  @override
  String get aiOther => 'Andere';

  @override
  String get aiProviderCustom => 'Eigene OpenAI-kompatible';

  @override
  String get aiGranted => 'Freigegeben';

  @override
  String get aiGrantedHint =>
      'Dieses Modell wurde nur für dich freigeschaltet. Für alle anderen ist es ausgeschaltet.';

  @override
  String get aiByokTag => 'Dein Schlüssel';

  @override
  String get aiPriceFree => 'Kostenlos';

  @override
  String get aiPriceUnknown => 'Preis unbekannt';

  @override
  String aiPricePerMillion(String price) {
    return '\$$price / 1M Eingabe';
  }

  @override
  String get aiNoModelTitle => 'KI ist noch nicht eingerichtet';

  @override
  String get aiNoModelBody =>
      'Frag die Person, die deine NeoCloud betreibt, oder nutze unter Erweitert dein eigenes NeoRouter-Konto oder einen eigenen API-Schlüssel.';

  @override
  String get aiNoModelAdminHint =>
      'Richte KI im Admin-Bereich für alle ein oder nutze unter Erweitert dein eigenes NeoRouter-Konto oder einen eigenen API-Schlüssel.';

  @override
  String get aiOpenAdmin => 'Admin öffnen';

  @override
  String get aiDisabledTitle => 'KI ist ausgeschaltet';

  @override
  String get aiDisabledBody =>
      'KI-Funktionen sind nicht eingeschaltet. Frag die Person, die deine NeoCloud betreibt, wenn du sie nutzen möchtest.';

  @override
  String get aiDisabledAdminHint =>
      'KI-Funktionen sind nicht eingeschaltet. Du kannst sie im Admin-Bereich für alle einschalten.';

  @override
  String get aiNotAvailableTitle => 'KI ist noch nicht verfügbar';

  @override
  String get aiNotAvailableBody => 'Diese Funktion ist noch nicht verfügbar.';

  @override
  String get aiAdminNotAvailableTitle =>
      'KI ist auf diesem Server nicht verfügbar';

  @override
  String get aiAdminNotAvailableBody =>
      'Aktualisiere NeoCloud auf dem Server, um KI-Funktionen zu nutzen.';

  @override
  String get aiFeaturesTitle => 'Vorschläge';

  @override
  String get aiFeaturesDescription =>
      'Was die KI vorschlagen darf. Du prüfst jeden Vorschlag, bevor sich etwas ändert.';

  @override
  String get aiFeatureNames => 'Namen vorschlagen';

  @override
  String get aiFeatureNamesDescription =>
      'Für Dateien mit nichtssagenden Namen wie IMG_1234 oder scan.pdf.';

  @override
  String get aiFeatureFolders => 'Ordner vorschlagen';

  @override
  String get aiFeatureFoldersDescription =>
      'Wohin eine Datei unter deinen bestehenden Ordnern gehört.';

  @override
  String get aiFeatureSummaries => 'Zusammenfassungen';

  @override
  String get aiFeatureSummariesDescription =>
      'Eine kurze Zusammenfassung in den Details einer Datei.';

  @override
  String get aiFeatureOnUpload => 'Beim Hochladen';

  @override
  String get aiFeatureOnUploadDescription =>
      'Für neue Dateien Vorschläge machen, sobald sie ankommen.';

  @override
  String get aiNamingTemplate => 'Namensvorlage';

  @override
  String get aiNamingTemplateDescription =>
      'Wie vorgeschlagene Namen aus Datum, Absender und Betreff gebaut werden, die die KI findet.';

  @override
  String get aiUsageTitle => 'KI-Nutzung diesen Monat';

  @override
  String aiUsageOf(String used, String limit) {
    return '$used von $limit Tokens';
  }

  @override
  String aiUsageUnlimited(String used) {
    return '$used Tokens · kein Limit';
  }

  @override
  String aiUsagePercent(int percent) {
    return '$percent % genutzt · beginnt am 1. neu';
  }

  @override
  String get aiUsageNoLimit => 'Kein monatliches Limit';

  @override
  String get aiAdvanced => 'Erweitert';

  @override
  String get aiAdvancedDescription =>
      'Modelle, dein eigenes NeoRouter-Konto und eigene API-Schlüssel.';

  @override
  String get aiByokTitle => 'Eigener KI-Dienst';

  @override
  String get aiByokDescription =>
      'Nutze dein eigenes NeoRouter-Konto oder eigene API-Schlüssel für deine KI-Funktionen. Schlüssel werden verschlüsselt gespeichert und nie wieder angezeigt.';

  @override
  String get aiByokNoProviders => 'Hier lässt sich noch nichts verbinden.';

  @override
  String get aiByokUsingYourKey => 'Dein Schlüssel';

  @override
  String get aiNotSetUp => 'Nicht eingerichtet';

  @override
  String get aiUpdate => 'Ändern';

  @override
  String get aiConnectEndpoint => 'Endpunkt verbinden';

  @override
  String get aiAddKey => 'Schlüssel hinzufügen';

  @override
  String get aiRemove => 'Entfernen';

  @override
  String aiRemoveKeyTitle(String provider) {
    return '$provider-Schlüssel entfernen?';
  }

  @override
  String aiRemoveKeyBody(String provider) {
    return 'NeoCloud verwendet deinen $provider-Schlüssel dann nicht mehr. Das lässt sich nicht rückgängig machen.';
  }

  @override
  String aiKeyRemoved(String provider) {
    return '$provider-Schlüssel entfernt.';
  }

  @override
  String get aiEnterKeyFirst => 'Gib zuerst einen API-Schlüssel ein.';

  @override
  String get aiConnectionGood => 'Verbindung sieht gut aus.';

  @override
  String get aiCouldNotConnect => 'Verbindung fehlgeschlagen.';

  @override
  String get aiKeyRequired => 'Ein API-Schlüssel ist erforderlich.';

  @override
  String get aiBaseUrlRequired =>
      'Für diesen Endpunkt ist eine Basis-URL erforderlich.';

  @override
  String get aiCustomEndpointTitle => 'Eigenen Endpunkt verbinden';

  @override
  String get aiCustomEndpointBody =>
      'Richte NeoCloud auf eine beliebige OpenAI-kompatible Chat-Completions-API, etwa ein selbst gehostetes Modell oder einen anderen gehosteten Dienst.';

  @override
  String aiYourKeyTitle(String provider) {
    return 'Dein $provider-Schlüssel';
  }

  @override
  String aiYourKeyBody(String provider) {
    return 'Füge deinen $provider-API-Schlüssel unten ein. Er wird verschlüsselt gespeichert und nur für deine eigenen KI-Funktionen verwendet.';
  }

  @override
  String get aiNameOptional => 'Name (optional)';

  @override
  String get aiNameHint => 'z. B. Mein eigenes Modell';

  @override
  String get aiBaseUrl => 'Basis-URL';

  @override
  String get aiBaseUrlOptional => 'Basis-URL (optional)';

  @override
  String get aiBaseUrlHelper =>
      'Muss über das öffentliche Internet erreichbar sein. Lokale und private Netzwerkadressen sind nicht erlaubt.';

  @override
  String get aiApiKey => 'API-Schlüssel';

  @override
  String get aiTestConnection => 'Verbindung testen';

  @override
  String get aiByokLimitsNote =>
      'Das monatliche KI-Limit gilt nicht für Modelle mit deinem eigenen Schlüssel.';

  @override
  String get aiShow => 'Anzeigen';

  @override
  String get aiHide => 'Verbergen';

  @override
  String get aiNeoRouterConnected => 'NeoRouter ist verbunden.';

  @override
  String get aiNeoRouterDisconnectTitle => 'NeoRouter trennen?';

  @override
  String get aiNeoRouterDisconnectBody =>
      'NeoCloud nutzt diesen NeoRouter nicht mehr. Um auch den Key zu entziehen, widerrufe ihn in NeoRouter unter API-Keys.';

  @override
  String aiNeoRouterWaiting(String code) {
    return 'Bestätige NeoCloud in NeoRouter. Dort steht der Code $code.';
  }

  @override
  String get aiNeoRouterOpenAgain => 'Erneut öffnen';

  @override
  String get aiNeoRouterServerInUse =>
      'NeoCloud nutzt bereits einen NeoRouter für alle. Verbinde dein eigenes NeoRouter-Konto für eigene Modelle und ein eigenes Limit.';

  @override
  String get aiNeoRouterUserDescription =>
      'Nutze dein eigenes NeoRouter-Konto für die Aufgaben, die du unter Verwalten auswählst.';

  @override
  String get aiNeoRouterServerDescription =>
      'Verbinde deinen NeoRouter einmal, und jedes Konto hier nutzt ihn für die unter Verwalten eingeschalteten Aufgaben.';

  @override
  String get aiNeoRouterUnreachable => 'NeoRouter ist gerade nicht erreichbar.';

  @override
  String get aiNeoRouterConnect => 'NeoRouter verbinden';

  @override
  String get aiNeoRouterAddress => 'NeoRouter-Adresse';

  @override
  String get aiNeoRouterRoleChat => 'Text und Bilderkennung';

  @override
  String get aiNeoRouterRoleEmbeddings => 'Ähnlichkeitssuche';

  @override
  String get aiNeoRouterRoleTranscription => 'Sprache zu Text';

  @override
  String get aiNeoRouterRoleImages => 'Bilderzeugung';

  @override
  String aiNeoRouterModelCount(int count) {
    return '$count in deinem NeoRouter verfügbar';
  }

  @override
  String aiNeoRouterNotAllowed(int count) {
    return '$count in deinem NeoRouter, aber der Key von NeoCloud darf sie noch nicht nutzen (z. B. Bezahl-Modelle). Erlaube sie für NeoCloud in NeoRouter unter API-Keys.';
  }

  @override
  String get aiNeoRouterNoModels =>
      'In deinem NeoRouter ist noch nichts verfügbar.';

  @override
  String get aiNeoRouterCancelled => 'Die Anmeldung wurde abgebrochen.';

  @override
  String get aiNeoRouterExpired =>
      'Diese Anmeldung ist abgelaufen. Starte sie neu.';

  @override
  String get aiNeoRouterFailed =>
      'NeoRouter hat die Anmeldung nicht bestätigt.';

  @override
  String get aiNeoRouterCouldNotOpen =>
      'NeoRouter ließ sich nicht öffnen. Nutze „Erneut öffnen“.';

  @override
  String get aiConnect => 'Verbinden';

  @override
  String get aiConnected => 'Verbunden';

  @override
  String get aiManage => 'Verwalten';

  @override
  String get aiAdminTitle => 'KI-Funktionen';

  @override
  String get aiAdminDescription =>
      'Vorschläge für Namen und Ordner, für Konten, die sie einschalten. Tresordateien werden nie verarbeitet.';

  @override
  String get aiAdminEnable => 'KI-Funktionen einschalten';

  @override
  String get aiAdminEnableDescription =>
      'Jedes Konto schaltet sie trotzdem selbst unter Einstellungen → KI ein.';

  @override
  String get aiAdminBudget => 'Monatliches Token-Budget';

  @override
  String get aiAdminBudgetDescription =>
      'Pro Konto, außer ein Konto hat unter Benutzer ein eigenes. Leer heißt kein Limit.';

  @override
  String get aiBudgetInvalid => 'Gib eine ganze Zahl von Tokens ein.';

  @override
  String get aiNoLimit => 'Kein Limit';

  @override
  String get aiProvidersTitle => 'Server-Anbieter-Zugangsdaten';

  @override
  String get aiProvidersDescription =>
      'API-Schlüssel und Endpunkte für KI-Anbieter. Sie werden von jedem Konto auf diesem Server genutzt; Konten können weiterhin eigene Schlüssel unter Einstellungen → KI hinzufügen.';

  @override
  String aiProvidersSetCount(int count, int total) {
    return '$count von $total festgelegt';
  }

  @override
  String get aiProvidersEmpty =>
      'Dieser Server meldet keine Anbietereinstellungen.';

  @override
  String get aiSet => 'Festgelegt';

  @override
  String get aiNotSet => 'Nicht festgelegt';

  @override
  String get aiReplace => 'Ersetzen';

  @override
  String get aiChange => 'Ändern';

  @override
  String get aiSetUrl => 'URL festlegen';

  @override
  String get aiEndpointUrl => 'Endpunkt-URL';

  @override
  String get aiUrl => 'URL';

  @override
  String aiClearTitle(String name) {
    return '$name löschen?';
  }

  @override
  String get aiClearUrlBody =>
      'Dadurch wird die Adresse für jedes Konto auf diesem Server entfernt.';

  @override
  String get aiClearKeyBody =>
      'Jedes Konto auf diesem Server verliert diesen gemeinsamen Schlüssel. Konten, die einen eigenen Schlüssel hinzugefügt haben, behalten ihn.';

  @override
  String aiCleared(String name) {
    return '$name gelöscht.';
  }

  @override
  String aiSavedName(String name) {
    return '$name gespeichert.';
  }

  @override
  String get aiPasteAKey => 'Füge einen Schlüssel ein.';

  @override
  String get aiEnterAddress => 'Gib eine Adresse ein.';

  @override
  String get aiUrlInvalid =>
      'Die Adresse muss eine http://- oder https://-Adresse sein.';

  @override
  String aiPasteNewKey(String hint) {
    return 'Neuen Schlüssel einfügen, um $hint zu ersetzen';
  }

  @override
  String get aiPasteTheKey => 'Schlüssel einfügen';

  @override
  String aiProviderUrlBody(String name) {
    return 'Jedes Konto auf diesem Server verwendet diese Adresse für $name.';
  }

  @override
  String get aiProviderKeyBody =>
      'Wird von jedem Konto auf diesem Server genutzt, das keinen eigenen Schlüssel hat. Nach dem Speichern wird er nicht mehr angezeigt.';

  @override
  String get aiModelAvailability => 'Modellverfügbarkeit';

  @override
  String get aiModelAvailabilityDescription =>
      'Wähle, welche Modelle jedes Konto auf diesem Server auswählen und nutzen darf. Solange ein Modell ausgeschaltet ist, starten später hinzugefügte Modelle ebenfalls ausgeschaltet.';

  @override
  String get aiAutoEnable => 'Neue Modelle automatisch aktivieren';

  @override
  String get aiAutoEnableHint =>
      'Wenn aus, bleiben Modelle, die ein Anbieter später hinzufügt, deaktiviert, bis du sie aktivierst.';

  @override
  String get aiSearchModelsOrProviders => 'Modelle oder Anbieter suchen';

  @override
  String get aiFilterEnabled => 'Aktiviert';

  @override
  String get aiFilterDisabled => 'Deaktiviert';

  @override
  String aiShownCount(int count) {
    return '$count angezeigt';
  }

  @override
  String get aiEnableAll => 'Alle aktivieren';

  @override
  String get aiEnableShown => 'Angezeigte aktivieren';

  @override
  String get aiDisableAll => 'Alle deaktivieren';

  @override
  String get aiDisableShown => 'Angezeigte deaktivieren';

  @override
  String get aiCollapseAll => 'Alle einklappen';

  @override
  String get aiExpandAll => 'Alle ausklappen';

  @override
  String get aiNoModelsYet =>
      'Füge zuerst Zugangsdaten eines Anbieters hinzu. Seine Modelle erscheinen hier, sobald der Anbieter antwortet.';

  @override
  String aiNoModelMatches(String query) {
    return 'Kein Modell passt zu \"$query\".';
  }

  @override
  String aiUnsavedChanges(int count) {
    return '$count ungespeicherte Änderungen';
  }

  @override
  String get aiDiscard => 'Verwerfen';

  @override
  String get aiSaveChanges => 'Änderungen speichern';

  @override
  String get aiModelsSaved => 'Modellverfügbarkeit gespeichert.';

  @override
  String aiEnabledCount(int count, int total) {
    return '$count von $total aktiviert';
  }

  @override
  String get aiKindVision => 'Bilder';

  @override
  String get aiKindEmbedding => 'Embedding';

  @override
  String get aiKindTranscription => 'Sprache';

  @override
  String get aiAdminUsageDescription =>
      'Nur Metadaten: wer welches Modell mit wie vielen Tokens genutzt hat, nie die Inhalte.';

  @override
  String get aiUsageRequests => 'Anfragen';

  @override
  String get aiUsageTokens => 'Tokens';

  @override
  String get aiUsageErrors => 'Fehler';

  @override
  String get aiUsageAccounts => 'Aktivste Konten';

  @override
  String get aiUsageModels => 'Meistgenutzte Modelle';

  @override
  String get aiUsageNone => 'Diesen Monat noch keine KI-Nutzung.';

  @override
  String aiTokenCount(String count) {
    return '$count Tokens';
  }

  @override
  String get aiAdminUserMenu => 'KI-Modelle und Budget';

  @override
  String aiAdminUserTitle(String name) {
    return 'KI · $name';
  }

  @override
  String aiAdminUserSaved(String name) {
    return 'KI-Einstellungen für $name gespeichert';
  }

  @override
  String get aiUserBudgetDefault => 'Serverstandard';

  @override
  String get aiUserBudgetCustom => 'Eigener Wert';

  @override
  String aiUserBudgetDefaultValue(String value) {
    return 'Serverstandard: $value';
  }

  @override
  String get aiTokensPerMonth => 'Tokens pro Monat';

  @override
  String get aiExtraModels => 'Zusätzliche Modelle';

  @override
  String aiExtraModelsIntro(String name) {
    return 'Diese Modelle sind für alle ausgeschaltet. Hake die an, die $name trotzdem nutzen darf. Ihre Nutzung zählt weiter zum Budget dieses Kontos.';
  }

  @override
  String get aiExtraModelsEmpty =>
      'Kein Modell ist für alle ausgeschaltet, es gibt also nichts zusätzlich freizugeben.';

  @override
  String get aiAdminTabProviders => 'Anbieter';

  @override
  String get aiAdminTabModels => 'Modelle';

  @override
  String get aiRoleRequestedUnavailable =>
      'Deine Wahl ist gerade nicht verfügbar, daher wird ein anderes Modell genutzt.';

  @override
  String get aiFeatureTags => 'Schlagwörter vorschlagen';

  @override
  String get aiFeatureTagsDescription =>
      'Schlagwörter aus dem Inhalt einer Datei, etwa Rechnung oder Vertrag.';

  @override
  String get aiFeatureAutoApply => 'Sichere Vorschläge automatisch anwenden';

  @override
  String get aiFeatureAutoApplyDescription =>
      'Sonst wartet jeder Vorschlag auf dich. Legt nie Ordner an. Jede Änderung lässt sich rückgängig machen.';

  @override
  String get aiUsageFeatures => 'Nach Funktion';

  @override
  String get aiErrorDisabled => 'KI-Funktionen sind nicht eingeschaltet.';

  @override
  String get aiErrorNotOptedIn =>
      'Schalte zuerst die KI-Funktionen unter Einstellungen → KI ein.';

  @override
  String get aiErrorBudget =>
      'Das KI-Budget dieses Monats ist aufgebraucht. Es beginnt am 1. neu.';

  @override
  String get aiErrorNoModel => 'Gerade ist kein KI-Modell verfügbar.';

  @override
  String get aiErrorProvider =>
      'Der KI-Dienst hat nicht richtig geantwortet. Versuche es später noch einmal.';

  @override
  String get aiErrorVault => 'Dateien in einem Tresor gehen nie an die KI.';

  @override
  String get aiErrorExcluded => 'Dieser Ordner ist von KI ausgeschlossen.';

  @override
  String get aiErrorUrlNotAllowed =>
      'Diese Adresse ist nicht erlaubt. Nutze eine im öffentlichen Internet.';

  @override
  String get aiFolderExclude => 'Von KI ausschließen';

  @override
  String get aiFolderInclude => 'Für KI freigeben';

  @override
  String aiFolderExcluded(String name) {
    return '$name ist von KI ausgeschlossen.';
  }

  @override
  String aiFolderIncluded(String name) {
    return '$name ist wieder für KI freigegeben.';
  }

  @override
  String get aiFolderExcludedTag => 'von KI ausgeschlossen';

  @override
  String get aiReviewNav => 'Prüfen';

  @override
  String get aiReviewTitle => 'Prüfen';

  @override
  String get aiReviewSubtitle =>
      'KI-Vorschläge warten hier auf deine Entscheidung. Nichts ändert sich, bevor du annimmst, und alles Angenommene lässt sich rückgängig machen.';

  @override
  String get aiReviewTabNames => 'Namen';

  @override
  String get aiReviewTabFolders => 'Ordner';

  @override
  String get aiReviewTabTags => 'Tags';

  @override
  String aiAcceptHigh(int count) {
    return 'Alle sicheren annehmen ($count)';
  }

  @override
  String aiAcceptHighTitle(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count sichere Vorschläge annehmen?',
      one: '1 sicheren Vorschlag annehmen?',
    );
    return '$_temp0';
  }

  @override
  String get aiAcceptHighBody =>
      'Die sicheren Vorschläge in diesem Reiter werden auf einmal übernommen. Verschieben in neue Ordner wartet immer auf dich. Du kannst sie gleich danach rückgängig machen.';

  @override
  String aiAcceptHighFailed(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other:
          '$count Vorschläge ließen sich nicht übernehmen und bleiben in der Liste.',
      one: '1 Vorschlag ließ sich nicht übernehmen und bleibt in der Liste.',
    );
    return '$_temp0';
  }

  @override
  String aiAcceptedMany(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count Vorschläge angenommen',
      one: '1 Vorschlag angenommen',
    );
    return '$_temp0';
  }

  @override
  String aiAcceptedRename(String name) {
    return 'Umbenannt in „$name“';
  }

  @override
  String aiAcceptedMove(String folder) {
    return 'Verschoben nach $folder';
  }

  @override
  String aiAcceptedTags(String name) {
    return 'Tags zu „$name“ hinzugefügt';
  }

  @override
  String get aiAccept => 'Annehmen';

  @override
  String get aiReject => 'Ablehnen';

  @override
  String get aiEditName => 'Anderen Namen verwenden';

  @override
  String get aiEditFolder => 'Woandershin verschieben';

  @override
  String get aiEditTags => 'Tags bearbeiten';

  @override
  String get aiTagsLabel => 'Tags, durch Kommas getrennt';

  @override
  String get aiTierHigh => 'Sicher';

  @override
  String get aiTierMedium => 'Mittel';

  @override
  String get aiTierLow => 'Unsicher';

  @override
  String aiConfidence(int percent) {
    return 'Zu $percent % sicher';
  }

  @override
  String get aiSourceAi => 'KI';

  @override
  String get aiSourceRule => 'Regel';

  @override
  String aiSourceModel(String model) {
    return 'Vorgeschlagen von $model';
  }

  @override
  String get aiSourceRuleHint =>
      'Eine deiner Sortierregeln hat gepasst; kein Modell wurde gefragt.';

  @override
  String get aiNewFolder => 'neuer Ordner';

  @override
  String aiCleanupBanner(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count leere Ordner, von der KI angelegt',
      one: '1 leerer Ordner, von der KI angelegt',
    );
    return '$_temp0';
  }

  @override
  String aiCleanupRecentBanner(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count leere Ordner aus den letzten 7 Tagen',
      one: '1 leerer Ordner aus den letzten 7 Tagen',
    );
    return '$_temp0';
  }

  @override
  String get aiCleanupRemove => 'Entfernen';

  @override
  String get aiCleanupRecentAction => 'Leere Ordner entfernen';

  @override
  String get aiCleanupTitle => 'Leere Ordner entfernen?';

  @override
  String get aiCleanupBodyAi =>
      'Die KI hat diese Ordner angelegt, und sie sind jetzt leer. Sie wandern in den Papierkorb.';

  @override
  String get aiCleanupBodyRecent =>
      'Diese leeren Ordner auf oberster Ebene sind in den letzten 7 Tagen entstanden. Wähle die aus, die weg sollen; sie wandern in den Papierkorb.';

  @override
  String aiCleanupDone(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count Ordner in den Papierkorb verschoben',
      one: '1 Ordner in den Papierkorb verschoben',
    );
    return '$_temp0';
  }

  @override
  String get aiFileTypesTitle => 'Dateitypen';

  @override
  String get aiFileTypesDescription =>
      'Dateien der Typen, die du ausschaltest, lässt die KI in Ruhe: keine Namen, Tags, Ordner oder Zusammenfassungen.';

  @override
  String get aiFileTypesChoose => 'Formate wählen';

  @override
  String aiFileTypesSome(int on, int total) {
    return '$on von $total Formaten';
  }

  @override
  String get aiFileTypeImages => 'Bilder';

  @override
  String get aiFileTypeVideos => 'Videos';

  @override
  String get aiFileTypeDocuments => 'Dokumente';

  @override
  String get aiFileTypeSpreadsheets => 'Tabellen';

  @override
  String get aiFileTypePresentations => 'Präsentationen';

  @override
  String get aiFileTypeArchives => 'Archive';

  @override
  String get aiFileTypeAudio => 'Audio';

  @override
  String get aiFileTypeCode => 'Code';

  @override
  String get aiFileTypeOther => 'Sonstige';

  @override
  String get aiFileTypeOtherDescription =>
      'Alle Formate, die oben nicht stehen';

  @override
  String aiShowLow(int count) {
    return 'Unsichere zeigen ($count)';
  }

  @override
  String get aiHideLow => 'Unsichere ausblenden';

  @override
  String get aiLoadMore => 'Mehr laden';

  @override
  String get aiReviewEmptyTitle => 'Nichts zu prüfen';

  @override
  String get aiReviewEmptyBody =>
      'Neue Vorschläge erscheinen hier. Frag im ⋯-Menü einer Datei danach oder mit „Diesen Ordner ordnen“.';

  @override
  String get aiReviewOffTitle => 'KI ist für dich aus';

  @override
  String get aiReviewOffBody =>
      'Schalte KI-Funktionen unter Einstellungen › KI ein, um Vorschläge für Namen, Ordner und Tags zu bekommen.';

  @override
  String get aiOpenSettings => 'Einstellungen öffnen';

  @override
  String get aiOpenReview => 'Prüfen öffnen';

  @override
  String get aiSuggestionStale =>
      'Die Datei hat sich seit dem Vorschlag geändert, darum wurde er entfernt.';

  @override
  String get aiSuggestionDecided =>
      'Über diesen Vorschlag wurde schon anderswo entschieden.';

  @override
  String aiUndoConflict(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other:
          '$count Änderungen lassen sich nicht rückgängig machen: Die Dateien haben sich seitdem geändert.',
      one: 'Rückgängig nicht möglich: Die Datei hat sich seitdem geändert.',
    );
    return '$_temp0';
  }

  @override
  String aiRuleProposalTitle(String type, String issuer, String folder) {
    return '$type von $issuer immer nach $folder verschieben?';
  }

  @override
  String aiRuleProposalBody(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other:
          'Du hast $count solche Verschiebungen angenommen. Eine Regel erledigt das, ohne die KI zu fragen.',
      one: 'Du hast 1 solche Verschiebung angenommen.',
    );
    return '$_temp0';
  }

  @override
  String get aiCreateRule => 'Regel anlegen';

  @override
  String get aiDismiss => 'Verwerfen';

  @override
  String get aiRuleCreated =>
      'Regel angelegt. Du findest sie unter Einstellungen › KI.';

  @override
  String get aiSuggestionChip => 'KI-Vorschlag';

  @override
  String get aiSuggestionChipHint => 'KI-Vorschlag zeigen';

  @override
  String get aiSuggestionTitle => 'KI-Vorschlag';

  @override
  String get aiMenu => 'KI';

  @override
  String get aiSuggestName => 'Namen vorschlagen';

  @override
  String get aiSuggestFolder => 'Ordner vorschlagen';

  @override
  String get aiSuggestNames => 'Namen vorschlagen';

  @override
  String get aiSuggestFolders => 'Ordner vorschlagen';

  @override
  String get aiSuggestTags => 'Tags vorschlagen';

  @override
  String get aiSummarize => 'Zusammenfassen';

  @override
  String aiRequested(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other:
          'KI zu $count Dateien gefragt. Vorschläge erscheinen unter Prüfen.',
      one: 'KI um einen Vorschlag gebeten. Er erscheint unter Prüfen.',
    );
    return '$_temp0';
  }

  @override
  String aiNothingSuggested(String name) {
    return 'Die KI hat keinen Vorschlag für „$name“.';
  }

  @override
  String get aiOrganizeFolder => 'Diesen Ordner ordnen …';

  @override
  String aiOrganizeTitle(String name) {
    return '„$name“ ordnen';
  }

  @override
  String get aiOrganizeIntro =>
      'Die KI sieht sich jede Datei in diesem Ordner und seinen Unterordnern an und schlägt vor, was du auswählst. Über jeden Vorschlag entscheidest du unter Prüfen.';

  @override
  String get aiOrganizeNames => 'Namen für Dateien mit nichtssagenden Namen';

  @override
  String get aiOrganizeFolders => 'Einen passenderen Ordner für jede Datei';

  @override
  String get aiOrganizeTags => 'Tags';

  @override
  String get aiOrganizeKindOff => 'Aus unter Einstellungen › KI';

  @override
  String get aiOrganizeNote =>
      'Tresore und von KI ausgeschlossene Ordner werden übersprungen. Bis zu 5.000 Dateien.';

  @override
  String get aiOrganizeStart => 'Starten';

  @override
  String get aiOrganizeRunning => 'Die KI sieht sich die Dateien an …';

  @override
  String get aiOrganizeDone => 'Fertig';

  @override
  String get aiOrganizeQueued => 'Wartet auf den Start …';

  @override
  String aiOrganizeProgress(int done, int total) {
    return '$done von $total Dateien';
  }

  @override
  String aiOrganizeSkipped(int skipped, int failed) {
    return '$skipped übersprungen, $failed fehlgeschlagen';
  }

  @override
  String get aiOrganizePaused => 'Die Arbeit ist vorerst angehalten.';

  @override
  String aiOrganizeWaiting(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count Vorschläge warten unter Prüfen.',
      one: '1 Vorschlag wartet unter Prüfen.',
    );
    return '$_temp0';
  }

  @override
  String get aiOrganizeNothing =>
      'Nichts vorzuschlagen: Diese Dateien sehen gut aus.';

  @override
  String get aiOrganizeBackground =>
      'Du kannst das schließen; die Arbeit läuft weiter und Vorschläge kommen unter Prüfen an.';

  @override
  String get aiSummaryTitle => 'Zusammenfassung';

  @override
  String get aiSummaryWorking => 'Wird zusammengefasst …';

  @override
  String get aiSummaryRefresh => 'Neu zusammenfassen';

  @override
  String aiSummaryBy(String model) {
    return 'Geschrieben von $model';
  }

  @override
  String get aiSummaryByAi => 'Von KI geschrieben';

  @override
  String get aiDetailsTitle => 'KI-Details';

  @override
  String get aiTagsTitle => 'Tags';

  @override
  String get aiFieldsTitle => 'Aus der Datei gelesen';

  @override
  String aiFieldsBy(String model) {
    return 'Gelesen von $model';
  }

  @override
  String get aiFieldType => 'Art';

  @override
  String get aiFieldIssuer => 'Von';

  @override
  String get aiFieldDate => 'Datum';

  @override
  String get aiFieldSubject => 'Betreff';

  @override
  String get aiFieldAmount => 'Betrag';

  @override
  String get aiFieldCaption => 'Beschreibung';

  @override
  String get aiDocTypeInvoice => 'Rechnung';

  @override
  String get aiDocTypeReceipt => 'Beleg';

  @override
  String get aiDocTypeContract => 'Vertrag';

  @override
  String get aiDocTypePayslip => 'Gehaltsabrechnung';

  @override
  String get aiDocTypeLetter => 'Brief';

  @override
  String get aiDocTypeStatement => 'Kontoauszug';

  @override
  String get aiDocTypeCertificate => 'Bescheinigung';

  @override
  String get aiDocTypeManual => 'Anleitung';

  @override
  String get aiDocTypeReport => 'Bericht';

  @override
  String get aiDocTypeNotes => 'Notizen';

  @override
  String get aiDocTypePresentation => 'Präsentation';

  @override
  String get aiDocTypeSpreadsheet => 'Tabelle';

  @override
  String get aiDocTypeScreenshot => 'Bildschirmfoto';

  @override
  String get aiDocTypePhoto => 'Foto';

  @override
  String get aiDocTypeScan => 'Scan';

  @override
  String get aiDocTypeOther => 'Sonstiges';

  @override
  String get aiFeatureNewTags => 'Neue Tags vorschlagen';

  @override
  String get aiFeatureNewTagsDescription =>
      'Sonst verwendet die KI nur Tags, die du schon hast.';

  @override
  String get aiFeatureNewFolders => 'Neue Ordner vorschlagen';

  @override
  String get aiFeatureNewFoldersDescription =>
      'Erlaubt der KI, einen Ordner vorzuschlagen, den es noch nicht gibt, wenn mindestens drei Dateien hineingehören. Er wird erst angelegt, wenn du annimmst.';

  @override
  String get aiReviewSettingsLabel => 'Vorschläge zum Prüfen';

  @override
  String aiReviewSettingsDescription(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count Vorschläge warten.',
      one: '1 Vorschlag wartet.',
      zero: 'Gerade wartet nichts.',
    );
    return '$_temp0';
  }

  @override
  String get aiRulesTitle => 'Sortierregeln';

  @override
  String get aiRulesDescription =>
      'Werden vor jedem Modell geprüft: Eine Datei, die alle Bedingungen einer Regel erfüllt, wird für deren Ordner vorgeschlagen.';

  @override
  String get aiRulesEmpty =>
      'Noch keine Regeln. Prüfen bietet eine an, wenn du ein paar ähnliche Verschiebungen angenommen hast.';

  @override
  String get aiRuleAdd => 'Regel hinzufügen';

  @override
  String get aiRuleEdit => 'Regel bearbeiten';

  @override
  String get aiRuleName => 'Name';

  @override
  String get aiRuleWhen => 'Wenn eine Datei';

  @override
  String get aiRuleAnyType => 'Jede Art';

  @override
  String get aiRuleExtension => 'Endung';

  @override
  String get aiRulePattern => 'Namensmuster (* und ?)';

  @override
  String get aiRuleTarget => 'Verschieben nach';

  @override
  String get aiRuleChooseFolder => 'Ordner wählen';

  @override
  String aiRuleSummary(String conditions, String folder) {
    return '$conditions → $folder';
  }

  @override
  String get aiRuleNeedsName => 'Gib der Regel einen Namen.';

  @override
  String get aiRuleNeedsCondition => 'Lege mindestens eine Bedingung fest.';

  @override
  String get aiRuleNeedsFolder => 'Wähle den Ordner, in den Dateien kommen.';

  @override
  String get aiRuleDeleteTitle => 'Regel löschen?';

  @override
  String aiRuleDeleteBody(String name) {
    return '„$name“ sortiert keine Dateien mehr. Vorschläge, die sie schon gemacht hat, bleiben.';
  }

  @override
  String get aiErrorFeatureOff =>
      'Diese KI-Funktion ist unter Einstellungen › KI ausgeschaltet.';

  @override
  String get aiErrorNoContent =>
      'In dieser Datei gibt es keinen Text, mit dem die KI arbeiten kann.';

  @override
  String get aiErrorTooManyFiles =>
      'Dieser Ordner hat zu viele Dateien auf einmal. Wähle einen kleineren Ordner.';

  @override
  String get aiErrorTargetNotAllowed =>
      'Dorthin kann die KI keine Dateien legen: Es ist ein Tresor oder von KI ausgeschlossen.';

  @override
  String get aiErrorShareBoundary =>
      'Diese Verschiebung würde ändern, wer die Datei sehen kann, darum macht die KI sie nicht.';

  @override
  String get aiErrorNotUndoable =>
      'Diese Änderung lässt sich nicht rückgängig machen.';

  @override
  String get photosNav => 'Fotos';

  @override
  String get photosEmptyTitle => 'Noch keine Fotos';

  @override
  String get photosEmptyBody =>
      'Fotos und Videos, die du in NeoCloud aufbewahrst, erscheinen hier, die neuesten zuerst.';

  @override
  String get photosEmptyBackup => 'Dieses Handy sichern';

  @override
  String get photosEmptyOpenFiles => 'Zu den Dateien';

  @override
  String get photosLoadFailed => 'Deine Fotos konnten nicht geladen werden.';

  @override
  String photosSorting(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count neue Fotos werden nach Datum einsortiert…',
      one: '1 neues Foto wird nach Datum einsortiert…',
    );
    return '$_temp0';
  }

  @override
  String get photosBackupOff => 'Sicherung ist aus';

  @override
  String get photosBackupTurnOn => 'Einschalten';

  @override
  String get photosToday => 'Heute';

  @override
  String get photosSortingSection => 'Neue Fotos werden einsortiert…';

  @override
  String get photosUnknownDate => 'Datum unbekannt';

  @override
  String get photosYesterday => 'Gestern';

  @override
  String get photosZoomIn => 'Größere Fotos';

  @override
  String get photosZoomOut => 'Mehr Fotos auf einmal';

  @override
  String get photosSelectGroup => 'Alle hier auswählen';

  @override
  String get photosShareOne =>
      'Wähle ein Foto aus, um einen Link dazu zu teilen.';

  @override
  String get photosPhotoUnavailable =>
      'Dieses Foto kann nicht angezeigt werden.';

  @override
  String get photosLocalOnPhone => 'Auf diesem Handy';

  @override
  String get photosLocalBackUp => 'In NeoCloud sichern';

  @override
  String get photosLocalBackedUp => 'Gesichert';

  @override
  String get photosLocalChecking => 'Wird geprüft…';

  @override
  String get photosLocalBackingUp => 'Wird gesichert…';

  @override
  String get photosLocalSignIn =>
      'Verbinde dich mit NeoCloud, um das zu sichern.';

  @override
  String get photosLocalBackupFailed =>
      'Das konnte nicht gesichert werden. Versuch es noch einmal.';

  @override
  String get aiMoreActions => 'Weitere Aktionen';

  @override
  String get aiAcceptAllTab => 'Alle in diesem Reiter annehmen';

  @override
  String get aiRejectAllTab => 'Alle in diesem Reiter ablehnen';

  @override
  String aiAcceptAllTitle(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count Vorschläge annehmen?',
      one: '1 Vorschlag annehmen?',
    );
    return '$_temp0';
  }

  @override
  String get aiAcceptAllBody =>
      'Alle Vorschläge in diesem Reiter werden auf einmal übernommen. Du kannst sie gleich danach rückgängig machen.';

  @override
  String aiRejectAllTitle(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count Vorschläge ablehnen?',
      one: '1 Vorschlag ablehnen?',
    );
    return '$_temp0';
  }

  @override
  String get aiRejectAllBody =>
      'Alle Vorschläge in diesem Reiter werden verworfen. Deine Dateien bleiben, wie sie sind.';

  @override
  String aiRejectedMany(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count Vorschläge abgelehnt',
      one: '1 Vorschlag abgelehnt',
    );
    return '$_temp0';
  }

  @override
  String get duplicatesTab => 'Duplikate';

  @override
  String get duplicatesSubtitle =>
      'Kopien derselben Datei und Fotos, die sich gleichen. Entferne die überzähligen, um Platz zu schaffen.';

  @override
  String get duplicatesExactTitle => 'Identische Kopien';

  @override
  String duplicatesSimilarTitle(int percent) {
    return 'Ähnliche Fotos · $percent % gleich';
  }

  @override
  String duplicatesGroupSummary(int count, String size) {
    return '$count Dateien · $size können frei werden';
  }

  @override
  String get duplicatesKeep => 'Behalten';

  @override
  String get duplicatesRemove => 'Entfernen';

  @override
  String get duplicatesToggleHint =>
      'Tippe, um zwischen Behalten und Entfernen zu wechseln';

  @override
  String duplicatesTrashGroup(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count in den Papierkorb',
      one: '1 in den Papierkorb',
    );
    return '$_temp0';
  }

  @override
  String get duplicatesNotDuplicates => 'Keine Duplikate';

  @override
  String duplicatesCleanUpAll(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: 'Alle aufräumen ($count Dateien)',
      one: 'Alle aufräumen (1 Datei)',
    );
    return '$_temp0';
  }

  @override
  String duplicatesCleanUpTitle(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count Dateien in den Papierkorb verschieben?',
      one: '1 Datei in den Papierkorb verschieben?',
    );
    return '$_temp0';
  }

  @override
  String get duplicatesCleanUpBody =>
      'In jeder Gruppe bleibt die mit „Behalten“ markierte Datei, die anderen kommen in den Papierkorb. Du kannst das gleich danach rückgängig machen oder sie später aus dem Papierkorb wiederherstellen.';

  @override
  String get duplicatesEmptyTitle => 'Keine Duplikate gefunden';

  @override
  String get duplicatesEmptyBody =>
      'Kopien derselben Datei und Fotos, die sich gleichen, erscheinen hier.';

  @override
  String duplicatesChecking(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: 'Deine Fotos werden geprüft … noch $count',
      one: 'Deine Fotos werden geprüft … noch 1',
    );
    return '$_temp0';
  }

  @override
  String duplicatesTrashFailed(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other:
          '$count Dateien konnten nicht in den Papierkorb verschoben werden.',
      one: '1 Datei konnte nicht in den Papierkorb verschoben werden.',
    );
    return '$_temp0';
  }
}
