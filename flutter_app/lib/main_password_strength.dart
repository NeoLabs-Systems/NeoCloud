part of 'main.dart';

class _PasswordStrengthInfo {
  const _PasswordStrengthInfo({
    required this.score,
    required this.label,
    required this.message,
    required this.color,
    required this.acceptable,
  });

  final int score;
  final String label;
  final String message;
  final Color color;

  /// Whether the server will accept the password; forms disable submit
  /// otherwise.
  final bool acceptable;
}

_PasswordStrengthInfo _passwordStrengthInfo({
  required String password,
  String username = '',
  String email = '',
}) {
  if (password.isEmpty) {
    return _PasswordStrengthInfo(
      score: 0,
      label: appStrings.strengthEmpty,
      message: appStrings.passwordHintEmpty,
      color: _borderLight,
      acceptable: false,
    );
  }
  final evaluation = evaluatePasswordStrength(
    password: password,
    username: username,
    email: email,
  );
  final score = evaluation.score;

  if (!evaluation.isAcceptable) {
    final String reason;
    if (!evaluation.hasMinimumLength) {
      reason = appStrings.passwordHintLength;
    } else if (evaluation.containsUserInfo) {
      reason = appStrings.passwordHintUserInfo;
    } else if (evaluation.obviousPattern) {
      reason = appStrings.passwordHintPattern;
    } else {
      reason = appStrings.passwordHintMore;
    }
    return _PasswordStrengthInfo(
      score: score.clamp(1, 2),
      label: appStrings.strengthRejected,
      message: reason,
      color: _danger,
      acceptable: false,
    );
  }
  if (score >= 4) {
    return _PasswordStrengthInfo(
      score: 4,
      label: appStrings.strengthStrong,
      message: appStrings.passwordHintStrong,
      color: _success,
      acceptable: true,
    );
  }
  return _PasswordStrengthInfo(
    score: 3,
    label: appStrings.strengthGood,
    message: appStrings.passwordHintGood,
    color: _success,
    acceptable: true,
  );
}

class _PasswordStrengthIndicator extends StatelessWidget {
  const _PasswordStrengthIndicator({required this.info});

  final _PasswordStrengthInfo info;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Row(
          children: <Widget>[
            Flexible(
              child: Text(
                appStrings.passwordStrength(info.label),
                overflow: TextOverflow.ellipsis,
                style: TextStyle(
                  color: info.color,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ),
            const SizedBox(width: 10),
            Expanded(
              child: ClipRRect(
                borderRadius: BorderRadius.circular(999),
                child: LinearProgressIndicator(
                  minHeight: 8,
                  value: info.score / 4,
                  backgroundColor: _borderLight,
                  valueColor: AlwaysStoppedAnimation<Color>(info.color),
                ),
              ),
            ),
          ],
        ),
        const SizedBox(height: 6),
        Text(
          info.message,
          style: TextStyle(color: _textSecondary, fontSize: 12, height: 1.35),
        ),
      ],
    );
  }
}
