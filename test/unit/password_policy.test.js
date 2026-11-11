'use strict';

// The NeoLabs password policy (same rules and messages as NeoAgent): at least
// 8 characters, a score of 3 or more, no personal info or obvious patterns.

const assert = require('node:assert/strict');
const { test } = require('node:test');

const {
  MIN_PASSWORD_LENGTH,
  MIN_PASSWORD_SCORE,
  evaluatePasswordStrength,
  passwordStrengthError,
} = require('../../server/services/account/password_policy');

test('the policy is 8 characters and a score of 3', () => {
  assert.equal(MIN_PASSWORD_LENGTH, 8);
  assert.equal(MIN_PASSWORD_SCORE, 3);
});

test('rejects weak, common, sequential and personal-info passwords', () => {
  assert.equal(evaluatePasswordStrength('short').isAcceptable, false);
  assert.equal(evaluatePasswordStrength('password123').isAcceptable, false);
  assert.equal(evaluatePasswordStrength('abcd1234').isAcceptable, false);
  assert.equal(evaluatePasswordStrength('Neocloud-2026!').isAcceptable, false);
  assert.equal(
    evaluatePasswordStrength('NeoUser2026!', { username: 'neouser', email: 'neo@example.com' }).isAcceptable,
    false,
  );
});

test('accepts a short but varied password and clamps the score', () => {
  const tenCharacters = evaluatePasswordStrength('Plumtree9!');
  assert.equal(tenCharacters.isAcceptable, true);
  assert.equal(tenCharacters.score, 3);
  const strong = evaluatePasswordStrength('CorrectHorse9!Battery');
  assert.equal(strong.isAcceptable, true);
  assert.equal(strong.score, 4);
  assert.equal(evaluatePasswordStrength('').score, 0);
});

test('the error names the reason that rejected the password', () => {
  assert.match(passwordStrengthError(evaluatePasswordStrength('short')), /Use at least 8 characters/);
  assert.equal(
    passwordStrengthError(evaluatePasswordStrength('Mypassword9!x')),
    'Password is too weak. Avoid common words, repeated characters, or obvious sequences.',
  );
  assert.equal(
    passwordStrengthError(evaluatePasswordStrength('Carol-Garden-73!', { username: 'carol' })),
    'Password is too weak. Avoid using your username or email in the password.',
  );
});
