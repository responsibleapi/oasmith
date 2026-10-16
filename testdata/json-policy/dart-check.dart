import 'dart:convert';
import 'dart:io';
import 'api/models.dart' as api;
import 'strict/models.dart' as strict;

void main() {
  final cases = jsonDecode(File('cases.json').readAsStringSync()) as List;
  for (final row in cases) {
    final value = api.Policy.fromJson(row['input']);
    if (value.id != 'podcast' || value.source.url != 'playlist') {
      throw StateError('HTTP JSON lost known fields: ${row['name']}');
    }
    var accepted = false;
    try {
      final config = strict.Policy.fromJson(row['input']);
      if (config.id != 'podcast' || config.source.url != 'playlist') {
        throw StateError('config lost known fields: ${row['name']}');
      }
      accepted = true;
    } on FormatException { /* Expected for extra fields in closed configs. */ }
    if (accepted != row['strict']) {
      throw StateError('strict decode: ${row['name']}; accepted=$accepted');
    }
  }
}
