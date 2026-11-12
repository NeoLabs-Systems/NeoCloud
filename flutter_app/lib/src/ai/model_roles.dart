import '../api/models.dart';

/// The kinds of work an account picks a model for (Settings › AI). Each is
/// `auto` or a `provider::model` id, stored under [settingKey].
enum AiRole { text, smart, vision, embedding }

extension AiRoleInfo on AiRole {
  String get settingKey => 'ai_model_$name';

  /// Whether [model] can do this work: text models chat, vision models
  /// also see images, embedding models are their own kind.
  bool accepts(ModelMeta model) {
    switch (this) {
      case AiRole.text:
      case AiRole.smart:
        return model.kind == 'chat';
      case AiRole.vision:
        return model.kind == 'chat' && model.supportsVision;
      case AiRole.embedding:
        return model.kind == 'embedding';
    }
  }
}

/// The models a picker for [role] offers: available ones that can do the work.
List<ModelMeta> modelsForRole(AiRole role, List<ModelMeta> models) =>
    models.where((model) => model.available && role.accepts(model)).toList();

/// The stored choice for [role]; `auto` when none is stored.
String selectedModel(AiRole role, Map<String, String> settings) {
  final value = settings[role.settingKey]?.trim() ?? '';
  return value.isEmpty ? 'auto' : value;
}
