mod public;
mod models;
mod responses;

#[cfg(test)]
mod tests {
    use super::{public::*, models, responses};
    #[tokio::test]
    async fn operations_send_encoded_paths_queries_headers_and_json() {
        use wiremock::{Mock, MockServer, ResponseTemplate, matchers::method};
        let work = async {
            let server = MockServer::start().await;
            Mock::given(method("POST"))
                .respond_with(ResponseTemplate::new(201).set_body_json(serde_json::json!({"id":"a", "name":"Podcast"})))
                .expect(1).mount(&server).await;
            let http = reqwest_middleware::ClientBuilder::new(reqwest::Client::builder().no_proxy().build().unwrap()).build();
            let api = Client::new(http, server.uri(), Some("test-key".into()));
            let result = api.create_thing(CreateThingParams {
                thing_id: "a/b ?#%".into(), tag: Some("x & y".into()), notify: false,
                label: Some(vec!["one".into(), "two".into()]), x_request_id: "req-1".into(),
                body: CreateThing { name: "Podcast".into() },
            }).await.unwrap();
            assert!(matches!(result, CreateThingResponse::Status201(_)));
            server.verify().await;
            let requests = server.received_requests().await.unwrap();
            let request = &requests[0];
            assert_eq!(request.url.path(), "/things/a%2Fb%20%3F%23%25");
            assert_eq!(request.url.query_pairs().collect::<Vec<_>>(), vec![
                ("tag".into(), "x & y".into()), ("notify".into(), "false".into()),
                ("label".into(), "one".into()), ("label".into(), "two".into()),
            ]);
            assert_eq!(request.headers["authorization"], "Bearer test-key");
            assert_eq!(request.headers["x-request-id"], "req-1");
            assert_eq!(request.headers["content-type"], "application/json");
            assert_eq!(serde_json::from_slice::<serde_json::Value>(&request.body).unwrap(), serde_json::json!({"name":"Podcast"}));
        };
        tokio::time::timeout(std::time::Duration::from_secs(5), work).await.expect("HTTP input contract did not complete");
    }
    #[tokio::test]
    async fn operations_send_optional_bodies_and_raw_bytes() {
        use wiremock::{Mock, MockServer, ResponseTemplate, matchers::path};
        let work = async {
            let server = MockServer::start().await;
            for route in ["/optional-json", "/optional-raw"] {
                Mock::given(path(route)).respond_with(ResponseTemplate::new(204)).expect(1).mount(&server).await;
            }
            Mock::given(path("/uploads/owner"))
                .respond_with(ResponseTemplate::new(201).set_body_json(serde_json::json!({"id":"a", "name":"Podcast"})))
                .expect(1).mount(&server).await;
            let http = reqwest_middleware::ClientBuilder::new(reqwest::Client::builder().no_proxy().build().unwrap()).build();
            let api = Client::new(http, server.uri(), None);
            assert!(matches!(api.patch_thing(PatchThingParams {body: None}).await.unwrap(), PatchThingResponse::Status204(())));
            assert!(matches!(api.upload_optional_media(UploadOptionalMediaParams {body: None}).await.unwrap(), UploadOptionalMediaResponse::Status204(())));
            assert!(matches!(api.upload_media(UploadMediaParams {owner: "owner".into(), upload_type: "media".into(), body: vec![0, 255, 10]}).await.unwrap(), UploadMediaResponse::Status201(_)));
            server.verify().await;
            let requests = server.received_requests().await.unwrap();
            for request in requests.iter().filter(|r| r.url.path().starts_with("/optional-")) {
                assert!(request.body.is_empty());
                assert!(!request.headers.contains_key("content-type"));
            }
            let request = requests.iter().find(|r| r.url.path() == "/uploads/owner").unwrap();
            assert_eq!(request.headers["content-type"], "application/octet-stream");
            assert_eq!(request.body, &[0, 255, 10]);
        };
        tokio::time::timeout(std::time::Duration::from_secs(5), work).await.expect("HTTP body contract did not complete");
    }
    #[tokio::test]
    async fn generated_operations_decode_http_statuses_and_bodies() {
        use wiremock::{Mock, MockServer, ResponseTemplate, matchers::{method, path}};
        let work = async {
            let server = MockServer::start().await;
            let http = reqwest_middleware::ClientBuilder::new(reqwest::Client::builder().no_proxy().build().unwrap()).build();
            let api = Client::new(http, server.uri(), None);
            for (status, body) in [(201, r#"{"id":"a","name":"Podcast"}"#), (400, r#"{"message":"invalid"}"#), (401, ""), (403, ""), (502, "unavailable")] {
                Mock::given(method("POST")).and(path("/things/a"))
                    .respond_with(ResponseTemplate::new(status).set_body_string(body)).expect(1).mount(&server).await;
                let result = api.create_thing(CreateThingParams {
                    thing_id: "a".into(), tag: None, notify: false, label: None,
                    x_request_id: "request".into(), body: CreateThing { name: "Podcast".into() },
                }).await.unwrap();
                assert_eq!(result.status(), status);
                match (status, result) {
                    (201, CreateThingResponse::Status201(thing)) => assert_eq!(thing.name, "Podcast"),
                    (400, CreateThingResponse::Status400(problem)) => assert_eq!(problem.message, "invalid"),
                    (401, CreateThingResponse::Status401(())) | (403, CreateThingResponse::Status403(())) => {},
                    (502, CreateThingResponse::Unexpected(response)) => assert_eq!(response.text().await.unwrap(), "unavailable"),
                    _ => panic!("wrong response variant for {status}"),
                }
                server.verify().await;
                server.reset().await;
            }
            Mock::given(path("/optional-json")).respond_with(ResponseTemplate::new(204)).expect(1).mount(&server).await;
            assert!(matches!(api.patch_thing(PatchThingParams { body: None }).await.unwrap(), PatchThingResponse::Status204(())));
            for (body, oversized) in [("invalid JSON", false), (r#"{"id":"a","name":"Podcast"}"#, true)] {
                Mock::given(path("/things/a")).respond_with(ResponseTemplate::new(201).set_body_string(body)).expect(1).mount(&server).await;
                let bounded = api.clone().body_limit(if oversized { 8 } else { 1024 });
                let result = bounded.create_thing(CreateThingParams {
                    thing_id: "a".into(), tag: None, notify: false, label: None,
                    x_request_id: "request".into(), body: CreateThing { name: "Podcast".into() },
                }).await;
                if oversized { assert!(matches!(result, Err(Error::BodyTooLarge { .. }))); }
                else { assert!(matches!(result, Err(Error::Decode(_)))); }
                server.verify().await;
                server.reset().await;
            }
            let payloads = responses::Client::new(reqwest_middleware::ClientBuilder::new(reqwest::Client::builder().no_proxy().build().unwrap()).build(), server.uri(), None);
            for (status, body) in [(200, r#""Podcast""#), (202, "42"), (206, "raw media")] {
                Mock::given(path("/payload")).respond_with(ResponseTemplate::new(status).set_body_string(body)).expect(1).mount(&server).await;
                let result = payloads.get_payload().await.unwrap();
                assert_eq!(responses::Response::status(&result), status);
                match result {
                    responses::GetPayloadResponse::Status200(text) => assert_eq!(text, "Podcast"),
                    responses::GetPayloadResponse::Status202(number) => assert_eq!(number, 42),
                    responses::GetPayloadResponse::Status206(bytes) => assert_eq!(bytes, b"raw media"),
                    _ => panic!("wrong response variant for {status}"),
                }
                server.verify().await;
                server.reset().await;
            }
            let event = "event: progress\ndata: {}\n\n";
            Mock::given(path("/events")).respond_with(ResponseTemplate::new(200).set_body_string(event)).expect(1).mount(&server).await;
            let WatchEventsResponse::Status200(stream) = api.watch_events().await.unwrap() else { panic!("wrong event variant") };
            assert_eq!(stream.text().await.unwrap(), event);
            server.verify().await;
        };
        tokio::time::timeout(std::time::Duration::from_secs(5), work).await.expect("typed HTTP contract did not complete");
    }
    #[test]
    fn discriminated_oneof_is_a_named_tagged_enum() {
        let imported = models::Connection::Import { source_url: "https://youtube.com/playlist?list=PLabc".into() };
        let wire = serde_json::to_value(&imported).unwrap();
        assert_eq!(wire, serde_json::json!({"kind":"import","source_url":"https://youtube.com/playlist?list=PLabc"}));
        assert!(matches!(serde_json::from_value::<models::Connection>(wire).unwrap(), models::Connection::Import { .. }));
        assert!(matches!(serde_json::from_str::<models::Connection>(r#"{"kind":"none"}"#).unwrap(), models::Connection::None { .. }));
        for invalid in [r#"{}"#, r#"{"kind":"unknown"}"#, r#"{"kind":"import"}"#, r#"{"kind":"none","source_url":"unexpected"}"#, r#"{"kind":"none","kind":"import"}"#] {
            assert!(serde_json::from_str::<models::Connection>(invalid).is_err(), "accepted {invalid}");
        }
    }
    #[test]
    fn nested_and_implicit_discriminators_roundtrip_without_duplicate_tags() {
        let event = models::Event::Connection(models::EventConnection::Import { source_url: "playlist".into() });
        let wire = serde_json::to_string(&event).unwrap();
        assert_eq!(wire, r#"{"type":"connection","kind":"import","source_url":"playlist"}"#);
        assert!(matches!(serde_json::from_str::<models::Event>(&wire).unwrap(), models::Event::Connection(models::EventConnection::Import { .. })));
        assert!(serde_json::from_str::<models::Event>(r#"{"type":"connection","kind":"import"}"#).is_err());
        let implicit = models::ImplicitEvent::ImplicitPayload { value: "data".into() };
        let wire = serde_json::to_value(implicit).unwrap();
        assert_eq!(wire, serde_json::json!({"type":"ImplicitPayload","value":"data"}));
        assert!(matches!(serde_json::from_value::<models::ImplicitEvent>(wire).unwrap(), models::ImplicitEvent::ImplicitPayload { .. }));
    }
    #[test]
    fn models_preserve_wire_names_nullability_and_discriminators() {
        let value: models::Record = serde_json::from_value(serde_json::json!({"type":"podcast", "nullable":null, "self":"me"})).unwrap();
        assert_eq!(value.r#type, "podcast");
        assert!(value.nullable.is_none());
        assert_eq!(value.self_value, "me");
        assert!(value.optional.is_none());
        let _: models::String = "string alias".into();
        assert!(serde_json::from_value::<TerminalResult>(serde_json::json!({"status":"completed","thing":{"id":"a","name":"b"}})).is_ok());
        assert!(serde_json::from_value::<TerminalResult>(serde_json::json!({"status":"unknown","message":"no"})).is_err());
    }

    #[tokio::test]
    async fn configured_client_propagates_the_context_active_at_send() {
        use opentelemetry::{global, Context};
        use opentelemetry::trace::{SpanContext, SpanId, TraceContextExt, TraceFlags, TraceId, TraceState, TracerProvider};
        use opentelemetry_sdk::{propagation::TraceContextPropagator, trace::{InMemorySpanExporter, SdkTracerProvider}};
        use tracing::{Instrument, instrument::WithSubscriber};
        use tracing_opentelemetry::OpenTelemetrySpanExt;
        use tracing_subscriber::prelude::*;
        use wiremock::{Mock, MockServer, ResponseTemplate, matchers::{method, path, header, body_json}};

        // This is the only test using the process-wide propagator. The provider,
        // subscriber, HTTP server, and requests all belong to this test.
        global::set_text_map_propagator(TraceContextPropagator::new());
        let exporter = InMemorySpanExporter::default();
        let provider = SdkTracerProvider::builder().with_simple_exporter(exporter.clone()).build();
        let subscriber = tracing_subscriber::registry()
            .with(tracing_opentelemetry::layer().with_tracer(provider.tracer("contract-test")));

        let work = async {
            let server = MockServer::start().await;
            Mock::given(method("POST")).and(path("/things/a%2Fb"))
                .and(header("authorization", "Bearer test-key"))
                .and(header("x-request-id", "request-one"))
                .and(body_json(serde_json::json!({"name":"Podcast"})))
                .respond_with(ResponseTemplate::new(400).set_body_json(serde_json::json!({"message":"invalid"})))
                .expect(1).mount(&server).await;
            let event = "event: progress\ndata: {}\n\n";
            Mock::given(method("GET")).and(path("/events"))
                .and(header("accept", "text/event-stream"))
                .and(header("authorization", "Bearer test-key"))
                .respond_with(ResponseTemplate::new(200).set_body_string(event))
                .expect(1).mount(&server).await;

            let http = reqwest_middleware::ClientBuilder::new(reqwest::Client::builder().no_proxy().build().unwrap())
                .with(reqwest_tracing::TracingMiddleware::default()).build();
            let api = Client::new(http, server.uri(), Some("test-key".into()));
            // Build before entering either span: injection must happen at send,
            // including through a clone of the shared generated client.
            let post = api.create_thing(CreateThingParams {
                thing_id: "a/b".into(), tag: None, notify: false, label: None,
                x_request_id: "request-one".into(), body: CreateThing { name: "Podcast".into() },
            });
            let cloned = api.clone();
            let events = cloned.watch_events();
            let first = tracing::info_span!("first operation");
            let second = tracing::info_span!("second operation");
            for (span, trace_id, span_id) in [(&first, 1u128, 2u64), (&second, 3u128, 4u64)] {
                span.set_parent(Context::new().with_remote_span_context(SpanContext::new(
                    TraceId::from(trace_id), SpanId::from(span_id), TraceFlags::SAMPLED, true,
                    TraceState::from_key_value([("acme", "state")]).unwrap(),
                )));
            }
            let parents = [first.context().span().span_context().clone(), second.context().span().span_context().clone()];
            let (post, events) = tokio::join!(post.instrument(first), events.instrument(second));
            assert!(matches!(post.unwrap(), CreateThingResponse::Status400(_)));
            let WatchEventsResponse::Status200(stream) = events.unwrap() else { panic!("wrong event variant") };
            assert_eq!(stream.text().await.unwrap(), event);
            server.verify().await;

            let requests = server.received_requests().await.unwrap();
            let spans = exporter.get_finished_spans().unwrap();
            for (route, parent) in [("/things/a%2Fb", &parents[0]), ("/events", &parents[1])] {
                let request = requests.iter().find(|r| r.url.path() == route).unwrap();
                let cx = global::get_text_map_propagator(|p| p.extract(&opentelemetry_http::HeaderExtractor(&request.headers)));
                let propagated = cx.span().span_context().clone();
                assert!(propagated.is_valid());
                assert_eq!(propagated.trace_id(), parent.trace_id());
                assert_ne!(propagated.span_id(), parent.span_id());
                assert_eq!(request.headers["tracestate"], "acme=state");
                let http_span = spans.iter().find(|s| s.span_context.span_id() == propagated.span_id()).unwrap();
                assert_eq!(http_span.span_kind, opentelemetry::trace::SpanKind::Client);
                assert_eq!(http_span.parent_span_id, parent.span_id());
            }
        }.with_subscriber(subscriber);
        tokio::time::timeout(std::time::Duration::from_secs(5), work).await.expect("trace propagation did not complete");
        provider.shutdown().unwrap();
    }
}
