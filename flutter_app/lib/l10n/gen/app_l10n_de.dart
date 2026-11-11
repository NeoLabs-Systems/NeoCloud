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
  String get connectionError => 'Der NeoCloud-Server ist nicht erreichbar.';

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
      'Dieses Konto verwaltet NeoCloud auf diesem Server.';

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
      'NeoCloud verschickt noch keine E-Mails. Bitte einen Admin dieses Servers, dir ein neues Passwort zu setzen (Admin › Benutzer).';

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
  String get filesSubtitle =>
      'Alles hier liegt verschlüsselt auf deinem Server.';

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
  String get settingsScopeAccount => 'KONTO';

  @override
  String get settingsScopeApp => 'DIESES GERÄT';

  @override
  String get settingsAccountHint => 'Wird in deinem Konto gespeichert';

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
      'Füge diesen Schlüssel in deiner Authenticator-App hinzu und gib den angezeigten Code ein.';

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
  String get adminConfirmPassword => 'Mit deinem Passwort bestätigen';

  @override
  String get officePreviewUnavailable =>
      'Für diese Datei gibt es keine Vorschau. Lade sie herunter, um sie zu öffnen.';

  @override
  String get play => 'Abspielen';

  @override
  String get pause => 'Pause';

  @override
  String get mediaUnavailable =>
      'Diese Datei lässt sich im Browser nicht abspielen.';

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
  String get backupDeleteMessage =>
      'Das Ziel wird aus NeoCloud entfernt. Vorhandene Backups bleiben dort erhalten.';

  @override
  String get backupHistory => 'Letzte Durchläufe';

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
      'Gib die Adresse deines NeoCloud-Servers ein. Anschließend bestätigst du dieses Gerät im Browser.';

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
      'Ende-zu-Ende-verschlüsselt. Namen und Inhalte werden auf diesem Gerät verschlüsselt; der Server kann sie nicht lesen.';

  @override
  String get vaultIntro =>
      'Ein Tresor ist ein Ordner, dessen Namen und Inhalte auf deinem Gerät mit einer Passphrase verschlüsselt werden, die nur du kennst.';

  @override
  String get vaultPassphrase => 'Passphrase';

  @override
  String get vaultPassphraseRepeat => 'Passphrase wiederholen';

  @override
  String get vaultNoRecovery =>
      'Die Passphrase lässt sich nicht wiederherstellen oder zurücksetzen, auch nicht durch einen Admin. Vergisst du sie, sind die Dateien in diesem Tresor verloren.';

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
    return 'Dieses Gerät verliert den Zugang zu $server und der Kamera-Upload wird ausgeschaltet. Zum erneuten Verbinden bestätigst du es noch einmal im Browser.';
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
      'NeoCloud liest neue Fotos und Videos aus deiner Mediathek, um sie auf deinen Server hochzuladen. Sonst verlässt nichts das Telefon. Im nächsten Schritt kannst du „Alle Fotos“ oder eine Auswahl erlauben.';

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
      'Nur du hast den Schlüssel. Wenn du Passphrase und Wiederherstellungsschlüssel verlierst, kann niemand – auch kein Admin – diesen Tresor wieder öffnen.';

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
      'Die Festplatte des Servers ist voll. Bitte den Admin, Platz zu schaffen; nichts wurde gespeichert.';

  @override
  String get securityServerStopping =>
      'Der Server startet neu. Versuch es gleich noch einmal.';

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
  String get cameraStatusUnreachable => 'Server nicht erreichbar';

  @override
  String get cameraStatusSignedOut => 'Abgemeldet';

  @override
  String get cameraStatusServerFull => 'Server voll';

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
  String get mobileNavFiles => 'Dateien';

  @override
  String get mobileNavCamera => 'Kamera';

  @override
  String get mobileMore => 'Mehr';

  @override
  String get mobileRecentServers => 'Zuletzt verwendet';

  @override
  String get mobileForgetServer => 'Vergessen';

  @override
  String get filesQuickAccess => 'Schnellzugriff';

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
      'Dieser Link ist für dein Smartphone gedacht. Öffne ihn dort oder scanne den QR-Code unter Einstellungen › Geräte mit der NeoCloud-App.';

  @override
  String get pairLandingInvalid =>
      'Dieser Kopplungslink ist unvollständig. Lass dir unter Einstellungen › Geräte einen neuen Code anzeigen.';

  @override
  String get connectOr => 'oder';

  @override
  String get connectScanQr => 'QR-Code scannen';

  @override
  String get connectScanQrHint =>
      'Öffne in der Web-App Einstellungen › Geräte › Smartphone koppeln.';

  @override
  String get scanTitle => 'QR-Code scannen';

  @override
  String get scanPrimeTitle => 'Mit der Kamera koppeln';

  @override
  String get scanPrimeBody =>
      'NeoCloud braucht die Kamera nur, um den Kopplungscode aus der Web-App zu lesen. Es wird nichts aufgenommen oder hochgeladen.';

  @override
  String get scanPrimeAllow => 'Kamera erlauben';

  @override
  String get scanHint =>
      'Richte die Kamera auf den QR-Code unter Einstellungen › Geräte';

  @override
  String get scanWrongCode => 'Das ist kein NeoCloud-Kopplungscode';

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
      'Der Server indexiert den Text deiner Dokumente, damit die Suche Wörter darin findet. Schalte das aus, um deine Dateien aus dem Index herauszuhalten.';

  @override
  String get searchSettingsContentsOff =>
      'Die Volltextsuche ist auf diesem Server aus. Schaltet ein Admin sie ein, entscheidet diese Option, ob deine Dateien indexiert werden.';

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
      'Nur ein Admin kann von einem Server in deinem lokalen Netzwerk importieren.';

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
  String get navSharedWithMe => 'Für mich freigegeben';

  @override
  String get sharedWithMeSubtitle =>
      'Dateien und Ordner, die andere auf diesem Server für dich freigegeben haben.';

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
      'Gib die genaue E-Mail-Adresse oder den Benutzernamen einer Person mit Konto auf diesem Server ein.';

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
}
