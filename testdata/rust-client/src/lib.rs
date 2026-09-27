mod public;
mod models;

#[cfg(test)]
mod tests {
    use super::{public::*, models};
    fn client() -> Client {
        Client::new(reqwest::Client::new(), "http://localhost:1234/".into(), Some("test-key".into()))
    }
    #[test]
    fn encodes_path_repeated_query_headers_and_json() {
        let request = client().create_thing(CreateThingParams {
            thing_id: "a/b ?#%".into(), tag: Some("x & y".into()), notify: false,
            label: Some(vec!["one".into(), "two".into()]), x_request_id: "req-1".into(),
            body: CreateThing { name: "Podcast".into() },
        }).build().unwrap();
        assert_eq!(request.url().path(), "/things/a%2Fb%20%3F%23%25");
        assert_eq!(request.url().query_pairs().collect::<Vec<_>>(), vec![
            ("tag".into(), "x & y".into()), ("notify".into(), "false".into()),
            ("label".into(), "one".into()), ("label".into(), "two".into()),
        ]);
        assert_eq!(request.headers()["authorization"], "Bearer test-key");
        assert_eq!(request.headers()["x-request-id"], "req-1");
        assert_eq!(request.headers()["content-type"], "application/json");
        let body: serde_json::Value = serde_json::from_slice(request.body().unwrap().as_bytes().unwrap()).unwrap();
        assert_eq!(body, serde_json::json!({"name":"Podcast"}));
    }
    #[test]
    fn optional_bodies_are_absent_and_raw_bytes_are_unchanged() {
        for request in [client().patch_thing(PatchThingParams {body: None}), client().upload_optional_media(UploadOptionalMediaParams {body: None})] {
            let request = request.build().unwrap();
            assert!(request.body().is_none());
            assert!(!request.headers().contains_key("content-type"));
        }
        let request = client().upload_media(UploadMediaParams {owner: "owner".into(), upload_type: "media".into(), body: vec![0, 255, 10]}).build().unwrap();
        assert_eq!(request.headers()["content-type"], "application/octet-stream");
        assert_eq!(request.body().unwrap().as_bytes().unwrap(), &[0, 255, 10]);
    }
    #[tokio::test]
    async fn decodes_statuses_and_leaves_event_stream_unconsumed() {
        let response = reqwest::Response::from(http_response(201, r#"{"id":"a","name":"Podcast"}"#));
        let CreateThingResponse::Status201(thing) = CreateThingResponse::decode(response).await.unwrap() else { panic!("wrong success variant") };
        assert_eq!(thing.name, "Podcast");
        let response = reqwest::Response::from(http_response(400, r#"{"message":"invalid"}"#));
        assert!(matches!(CreateThingResponse::decode(response).await.unwrap(), CreateThingResponse::Status400(_)));
        let response = reqwest::Response::from(http_response(204, ""));
        assert!(matches!(PatchThingResponse::decode(response).await.unwrap(), PatchThingResponse::Status204(())));
        let response = reqwest::Response::from(http_response(502, "unavailable"));
        assert!(matches!(CreateThingResponse::decode(response).await.unwrap(), CreateThingResponse::Unexpected(_)));
        let request = client().watch_events().build().unwrap();
        assert_eq!(request.headers()["accept"], "text/event-stream");
        let event = "event: progress\ndata: {}\n\n";
        let response = reqwest::Response::from(http_response(200, event));
        let WatchEventsResponse::Status200(stream) = WatchEventsResponse::decode(response).await.unwrap() else { panic!("wrong event variant") };
        assert_eq!(stream.text().await.unwrap(), event);
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
    fn http_response(status: u16, body: &str) -> http::Response<String> {
        http::Response::builder().status(status).body(body.into()).unwrap()
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
}
