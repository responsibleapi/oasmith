import 'dart:convert';

import 'package:http/http.dart' as http;
import 'package:oasmith_dart_fixture/public/api.dart';

void expect(bool condition, String message) {
  if (!condition) throw StateError(message);
}

Future<void> main() async {
  final requests = <http.Request>[];
  var reads = 0;
  final client = PublicApiClient(
    baseUri: Uri.parse('https://api.example.test/'),
    send: (baseRequest) async {
      final request = baseRequest as http.Request;
      requests.add(request);
      return http.StreamedResponse(
        Stream.value(utf8.encode('{"id":"thing-1","name":"example"}')),
        201,
      );
    },
    readBody: (response, maxBytes) async {
      expect(maxBytes == 4 << 20, 'body limit was not passed to transport');
      reads++;
      return response.stream.toBytes();
    },
  );
  final thing = await client.createThing(
    thingId: 'space/and?query',
    tag: 'one two',
    notify: true,
    label: ['first', 'second'],
    xRequestId: 'request-1',
    body: const CreateThing(name: 'example'),
  );
  expect(thing.id == 'thing-1', 'typed response did not decode');
  expect(reads == 1, 'transport body reader was not used');
  final request = requests.single;
  expect(request.url.path == '/things/space%2Fand%3Fquery', 'path parameter was not encoded');
  expect(request.url.toString().contains('space%2Fand%3Fquery'), 'path parameter was not encoded');
  expect(request.url.queryParametersAll['label']?.join(',') == 'first,second', 'array query was not repeated');
  expect(request.url.queryParameters['notify'] == 'true', 'boolean query was not encoded');
  expect(request.url.queryParameters['tag'] == 'one two', 'optional query was not encoded');
  expect(request.headers['x-request-id'] == 'request-1', 'header parameter was not passed');
  expect(jsonDecode(request.body)['name'] == 'example', 'typed body was not encoded');
  final terminal = TerminalResult.fromJson({
    'status': 'completed',
    'thing': {'id': 'thing-1', 'name': 'example'},
  });
  expect(terminal is CompletedResult && terminal.thing.id == 'thing-1', 'discriminated union did not decode');
  try {
    TerminalResult.fromJson({'status': 'unknown'});
    throw StateError('unknown discriminator was accepted');
  } on FormatException {
    // Expected: unknown variants must not silently decode.
  }
}
