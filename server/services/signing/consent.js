'use strict';

// The statement a signer agrees to before signing. The version is stored on
// the document when it is sent, so the wording a party saw can be named later.

const CONSENT_VERSION = 1;

const TEXTS = {
  en: 'I agree to review and sign this document electronically. I understand that my electronic signature '
    + 'is meant to have the same effect as a handwritten one, that I may decline to sign, and that I can ask '
    + 'the sender for a paper copy instead. My name, the time of each step, my IP address and my browser are '
    + 'recorded as evidence of the signing.',
  de: 'Ich bin damit einverstanden, dieses Dokument elektronisch zu prüfen und zu unterschreiben. Ich weiß, dass '
    + 'meine elektronische Unterschrift die gleiche Absicht ausdrückt wie eine handschriftliche, dass ich die '
    + 'Unterschrift ablehnen kann und dass ich beim Absender stattdessen eine Papierfassung verlangen kann. '
    + 'Mein Name, der Zeitpunkt jedes Schritts, meine IP-Adresse und mein Browser werden als Nachweis der '
    + 'Unterzeichnung gespeichert.',
};

function consentText(locale) {
  return TEXTS[locale] || TEXTS.en;
}

module.exports = { CONSENT_VERSION, consentText };
