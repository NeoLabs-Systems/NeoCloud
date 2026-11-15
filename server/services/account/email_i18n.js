'use strict';

// The words in NeoCloud's service mails and mail pages, English and German
// (the app's own two languages). `t(lang, key, params)` falls back to English
// for an unknown language or key, and `{name}` placeholders are filled from
// `params`. Everything returned is plain text: the templates escape it.

const STRINGS = {
  en: {
    'mail.eyebrow': 'Account',
    'mail.fallback': 'If the button does not work, open this link:',
    'mail.footer': 'This mailbox is used only for {brand} service notifications.',
    'mail.footerSupport': 'This mailbox is used only for {brand} service notifications. Support: {url}',
    'mail.label.account': 'Account',
    'mail.label.expires': 'Expires',
    'mail.label.newEmail': 'New email',
    'mail.label.requestedEmail': 'Requested email',
    'mail.label.time': 'Time',
    'mail.label.ip': 'IP address',
    'mail.label.device': 'Device',
    'mail.label.message': 'Message',
    'mail.hours.one': '{n} hour',
    'mail.hours.other': '{n} hours',

    'signup.subject': 'Confirm your {brand} email',
    'signup.title': 'Confirm your email',
    'signup.intro': 'Confirm this email address to finish creating the {brand} account {username}.',
    'signup.button': 'Confirm email',

    'emailChange.subject': 'Confirm your new {brand} email',
    'emailChange.title': 'Confirm your new email',
    'emailChange.intro': 'Confirm this address before {brand} changes the account email for {username}.',
    'emailChange.button': 'Confirm new email',

    'verify.subject': 'Confirm your {brand} email',
    'verify.title': 'Confirm your email',
    'verify.intro': 'Confirm that this address belongs to the {brand} account {username}.',
    'verify.button': 'Confirm email',

    'reset.subject': 'Reset your {brand} password',
    'reset.title': 'Reset your password',
    'reset.intro': 'Use this link to set a new password for {username}. Ignore this email if you did not ask for it; your password stays as it is.',
    'reset.button': 'Reset password',

    'passwordChanged.subject': '{brand} password changed',
    'passwordChanged.title': 'Your password was changed',
    'passwordChanged.intro': 'The password for {username} was changed. If this was not you, reset your password right away and sign out other devices.',

    'emailChangeRequested.subject': '{brand} email change requested',
    'emailChangeRequested.title': 'Email change requested',
    'emailChangeRequested.intro': 'Someone asked to change the email for {username}. Nothing changes until the new address is confirmed. If this was not you, change your password.',

    'emailChangedOld.subject': '{brand} email changed',
    'emailChangedOld.title': 'Your account email was changed',
    'emailChangedOld.intro': 'The email address for {username} was changed. If this was not you, contact the person who runs {brand}.',
    'emailChangedNew.subject': '{brand} email changed',
    'emailChangedNew.title': 'This email is now linked',
    'emailChangedNew.intro': 'This address is now linked to the {brand} account {username}.',

    'signIn.subject': 'New {brand} sign-in',
    'signIn.title': 'New sign-in detected',
    'signIn.intro': 'Your {brand} account was signed in from a device or network we have not seen recently. If this was you, nothing to do. If not, change your password and sign out other devices.',

    'test.subject': '{brand} test email',
    'test.title': 'Email works',
    'test.intro': 'This is a test message from {brand}. Your SMTP settings are working.',

    'page.openApp': 'Open {brand}',
    'page.confirm.title': 'Confirm your email',
    'page.confirm.intro': 'Press the button to confirm this email address for your {brand} account.',
    'page.confirm.button': 'Confirm email',
    'page.confirmed.title': 'Email confirmed',
    'page.confirmed.intro': 'Your {brand} email is confirmed. You can return to the app and sign in.',
    'page.confirmedChange.intro': 'Your new {brand} email is confirmed and now belongs to your account.',
    'page.confirmFailed.title': 'This link does not work',
    'page.confirmFailed.intro': 'The confirmation link is invalid, already used or expired. Request a new one in the app: sign in, or use "Resend confirmation".',
    'page.confirmMissing.title': 'Link incomplete',
    'page.confirmMissing.intro': 'Open the full link from your {brand} email.',
    'page.emailTaken.intro': 'This email address is already used by another account, so it could not be changed.',
    'page.reset.title': 'Choose a new password',
    'page.reset.intro': 'Enter a new password for your {brand} account. You will be signed out everywhere else.',
    'page.reset.password': 'New password',
    'page.reset.confirm': 'Repeat new password',
    'page.reset.submit': 'Change password',
    'page.reset.mismatch': 'The two passwords do not match.',
    'page.reset.hintMin': 'At least {min} characters. Do not use your username or email.',
    'page.reset.show': 'Show',
    'page.reset.hide': 'Hide',
    'page.resetDone.title': 'Password changed',
    'page.resetDone.intro': 'Your {brand} password has been changed and all other sessions were signed out. You can sign in with the new password.',
    'page.resetFailed.title': 'This link does not work',
    'page.resetFailed.intro': 'The reset link is invalid, already used or expired. Request a new one with "Forgot password?" on the sign-in page.',
    'page.resetTooMany.intro': 'Too many tries. Please wait a few minutes and request a new link.',
    'page.resetMissing.intro': 'Open the full link from your {brand} email.',

    'msg.registerPending': 'Check your email to confirm your {brand} account.',
    'msg.resendUniform': 'If that account is waiting for confirmation, {brand} sends a new confirmation link.',
    'msg.forgotUniform': 'If that account has a confirmed email, {brand} will send a password reset link.',
  },
  de: {
    'mail.eyebrow': 'Konto',
    'mail.fallback': 'Falls die Schaltfläche nicht funktioniert, öffne diesen Link:',
    'mail.footer': 'Dieses Postfach wird nur für Servicemitteilungen von {brand} verwendet.',
    'mail.footerSupport': 'Dieses Postfach wird nur für Servicemitteilungen von {brand} verwendet. Hilfe: {url}',
    'mail.label.account': 'Konto',
    'mail.label.expires': 'Gültig bis',
    'mail.label.newEmail': 'Neue E-Mail',
    'mail.label.requestedEmail': 'Gewünschte E-Mail',
    'mail.label.time': 'Zeit',
    'mail.label.ip': 'IP-Adresse',
    'mail.label.device': 'Gerät',
    'mail.label.message': 'Nachricht',
    'mail.hours.one': '{n} Stunde',
    'mail.hours.other': '{n} Stunden',

    'signup.subject': 'Bestätige deine {brand}-E-Mail',
    'signup.title': 'E-Mail bestätigen',
    'signup.intro': 'Bestätige diese E-Mail-Adresse, um das {brand}-Konto {username} fertig anzulegen.',
    'signup.button': 'E-Mail bestätigen',

    'emailChange.subject': 'Bestätige deine neue {brand}-E-Mail',
    'emailChange.title': 'Neue E-Mail bestätigen',
    'emailChange.intro': 'Bestätige diese Adresse, bevor {brand} die E-Mail-Adresse von {username} ändert.',
    'emailChange.button': 'Neue E-Mail bestätigen',

    'verify.subject': 'Bestätige deine {brand}-E-Mail',
    'verify.title': 'E-Mail bestätigen',
    'verify.intro': 'Bestätige, dass diese Adresse zum {brand}-Konto {username} gehört.',
    'verify.button': 'E-Mail bestätigen',

    'reset.subject': 'Setze dein {brand}-Passwort zurück',
    'reset.title': 'Passwort zurücksetzen',
    'reset.intro': 'Über diesen Link legst du ein neues Passwort für {username} fest. Hast du das nicht angefordert, ignoriere diese Mail; dein Passwort bleibt, wie es ist.',
    'reset.button': 'Passwort zurücksetzen',

    'passwordChanged.subject': '{brand}-Passwort geändert',
    'passwordChanged.title': 'Dein Passwort wurde geändert',
    'passwordChanged.intro': 'Das Passwort für {username} wurde geändert. Warst du das nicht, setze dein Passwort sofort zurück und melde andere Geräte ab.',

    'emailChangeRequested.subject': '{brand}: Änderung der E-Mail angefragt',
    'emailChangeRequested.title': 'Änderung der E-Mail angefragt',
    'emailChangeRequested.intro': 'Jemand möchte die E-Mail-Adresse von {username} ändern. Erst wenn die neue Adresse bestätigt ist, ändert sich etwas. Warst du das nicht, ändere dein Passwort.',

    'emailChangedOld.subject': '{brand}-E-Mail geändert',
    'emailChangedOld.title': 'Die E-Mail deines Kontos wurde geändert',
    'emailChangedOld.intro': 'Die E-Mail-Adresse von {username} wurde geändert. Warst du das nicht, wende dich an die Person, die {brand} betreibt.',
    'emailChangedNew.subject': '{brand}-E-Mail geändert',
    'emailChangedNew.title': 'Diese E-Mail ist jetzt verknüpft',
    'emailChangedNew.intro': 'Diese Adresse gehört jetzt zum {brand}-Konto {username}.',

    'signIn.subject': 'Neue Anmeldung bei {brand}',
    'signIn.title': 'Neue Anmeldung erkannt',
    'signIn.intro': 'Dein {brand}-Konto wurde von einem Gerät oder Netzwerk angemeldet, das wir länger nicht gesehen haben. Warst du das, ist nichts zu tun. Wenn nicht, ändere dein Passwort und melde andere Geräte ab.',

    'test.subject': '{brand}: Test-E-Mail',
    'test.title': 'E-Mail funktioniert',
    'test.intro': 'Das ist eine Testnachricht von {brand}. Deine SMTP-Einstellungen funktionieren.',

    'page.openApp': '{brand} öffnen',
    'page.confirm.title': 'E-Mail bestätigen',
    'page.confirm.intro': 'Tippe auf die Schaltfläche, um diese E-Mail-Adresse für dein {brand}-Konto zu bestätigen.',
    'page.confirm.button': 'E-Mail bestätigen',
    'page.confirmed.title': 'E-Mail bestätigt',
    'page.confirmed.intro': 'Deine {brand}-E-Mail ist bestätigt. Du kannst zur App zurückkehren und dich anmelden.',
    'page.confirmedChange.intro': 'Deine neue {brand}-E-Mail ist bestätigt und gehört jetzt zu deinem Konto.',
    'page.confirmFailed.title': 'Dieser Link funktioniert nicht',
    'page.confirmFailed.intro': 'Der Bestätigungslink ist ungültig, schon benutzt oder abgelaufen. Fordere in der App einen neuen an: melde dich an oder nutze „Bestätigung erneut senden“.',
    'page.confirmMissing.title': 'Link unvollständig',
    'page.confirmMissing.intro': 'Öffne den vollständigen Link aus deiner {brand}-Mail.',
    'page.emailTaken.intro': 'Diese E-Mail-Adresse gehört schon zu einem anderen Konto und konnte deshalb nicht übernommen werden.',
    'page.reset.title': 'Neues Passwort festlegen',
    'page.reset.intro': 'Gib ein neues Passwort für dein {brand}-Konto ein. Überall sonst wirst du abgemeldet.',
    'page.reset.password': 'Neues Passwort',
    'page.reset.confirm': 'Neues Passwort wiederholen',
    'page.reset.submit': 'Passwort ändern',
    'page.reset.mismatch': 'Die beiden Passwörter stimmen nicht überein.',
    'page.reset.hintMin': 'Mindestens {min} Zeichen. Verwende nicht deinen Benutzernamen oder deine E-Mail.',
    'page.reset.show': 'Zeigen',
    'page.reset.hide': 'Verbergen',
    'page.resetDone.title': 'Passwort geändert',
    'page.resetDone.intro': 'Dein {brand}-Passwort wurde geändert, alle anderen Sitzungen sind abgemeldet. Du kannst dich jetzt mit dem neuen Passwort anmelden.',
    'page.resetFailed.title': 'Dieser Link funktioniert nicht',
    'page.resetFailed.intro': 'Der Link zum Zurücksetzen ist ungültig, schon benutzt oder abgelaufen. Fordere auf der Anmeldeseite mit „Passwort vergessen?“ einen neuen an.',
    'page.resetTooMany.intro': 'Zu viele Versuche. Warte ein paar Minuten und fordere einen neuen Link an.',
    'page.resetMissing.intro': 'Öffne den vollständigen Link aus deiner {brand}-Mail.',

    'msg.registerPending': 'Prüfe dein Postfach, um dein {brand}-Konto zu bestätigen.',
    'msg.resendUniform': 'Wartet dieses Konto auf die Bestätigung, schickt {brand} einen neuen Bestätigungslink.',
    'msg.forgotUniform': 'Hat dieses Konto eine bestätigte E-Mail, schickt {brand} einen Link zum Zurücksetzen des Passworts.',
  },
};

const DEFAULT_LANGUAGE = 'en';

/** 'de' when the first language the caller prefers is German, else English. */
function pickLanguage(acceptLanguage) {
  const first = String(acceptLanguage || '').split(',')[0].trim().toLowerCase();
  return first.startsWith('de') ? 'de' : DEFAULT_LANGUAGE;
}

function t(lang, key, params = {}) {
  const table = STRINGS[lang] || STRINGS[DEFAULT_LANGUAGE];
  const template = table[key] ?? STRINGS[DEFAULT_LANGUAGE][key] ?? key;
  return template.replace(/\{(\w+)\}/g, (match, name) => (name in params ? String(params[name]) : match));
}

function formatHours(lang, hours) {
  return t(lang, hours === 1 ? 'mail.hours.one' : 'mail.hours.other', { n: hours });
}

module.exports = { formatHours, pickLanguage, t };
