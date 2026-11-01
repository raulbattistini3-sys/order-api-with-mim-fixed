//! Keeps the shelves true by following PocketBase's realtime feed, which also
//! sees writes that never pass the gateway, such as queued imports.
//!
//! A dropped connection can hide events, so the cache forgets everything until
//! it is subscribed again. It never serves what it can't prove is current.

use std::sync::atomic::Ordering;
use std::sync::Arc;
use std::time::Duration;

use futures_util::StreamExt;
use serde::Deserialize;

use crate::shelf::Order;
use crate::Cache;

#[derive(Deserialize)]
struct Connected {
    #[serde(rename = "clientId")]
    client_id: String,
}

#[derive(Deserialize)]
struct Change {
    action: String,
    record: Order,
}

#[derive(Deserialize)]
struct Auth {
    token: String,
}

const RECONNECT: Duration = Duration::from_secs(1);

/// Follows the feed for as long as the process runs.
pub async fn follow(cache: Arc<Cache>) {
    // No timeout here: this request is meant to stay open.
    let http = reqwest::Client::new();
    loop {
        if let Err(err) = session(&cache, &http).await {
            eprintln!("readcache: feed: {err}");
        }
        cache.forget_all();
        tokio::time::sleep(RECONNECT).await;
    }
}

async fn session(cache: &Cache, http: &reqwest::Client) -> Result<(), String> {
    let base = &cache.config.upstream.pocketbase;
    let text = |err: reqwest::Error| err.to_string();
    let auth: Auth = http
        .post(format!("{base}/api/collections/_superusers/auth-with-password"))
        .json(&serde_json::json!({"identity": cache.config.credentials.email, "password": cache.config.credentials.password}))
        .send()
        .await
        .and_then(reqwest::Response::error_for_status)
        .map_err(text)?
        .json()
        .await
        .map_err(text)?;

    let mut stream = http
        .get(format!("{base}/api/realtime"))
        .send()
        .await
        .and_then(reqwest::Response::error_for_status)
        .map_err(text)?
        .bytes_stream();

    let mut buffer = Vec::new();
    while let Some(chunk) = stream.next().await {
        buffer.extend_from_slice(&chunk.map_err(text)?);
        while let Some(end) = buffer.windows(2).position(|w| w == b"\n\n") {
            let frame: Vec<u8> = buffer.drain(..end + 2).collect();
            let frame = String::from_utf8_lossy(&frame);
            let (event, data) = parse(&frame);
            if event == "PB_CONNECT" {
                let connected: Connected = serde_json::from_str(&data).map_err(|err| err.to_string())?;
                http.post(format!("{base}/api/realtime"))
                    .bearer_auth(&auth.token)
                    .json(&serde_json::json!({"clientId": connected.client_id, "subscriptions": ["orders/*"]}))
                    .send()
                    .await
                    .and_then(reqwest::Response::error_for_status)
                    .map_err(text)?;
                cache.subscribed.store(true, Ordering::SeqCst);
            } else if event.starts_with("orders/") {
                apply(cache, &data);
            }
        }
    }
    Err("connection closed".into())
}

fn parse(frame: &str) -> (&str, String) {
    let mut event = "";
    let mut data = String::new();
    for line in frame.lines() {
        if let Some(value) = line.strip_prefix("event:") {
            event = value.trim();
        } else if let Some(value) = line.strip_prefix("data:") {
            data.push_str(value.trim_start());
        }
    }
    (event, data)
}

fn apply(cache: &Cache, data: &str) {
    let change: Change = match serde_json::from_str(data) {
        Ok(change) => change,
        Err(err) => {
            // Applying nothing would leave the shelf silently wrong.
            eprintln!("readcache: unreadable event: {err}");
            cache.forget_all();
            return;
        }
    };
    let Some(shelf) = cache.shelf(&change.record.customer) else { return };
    let mut shelf = shelf.write().unwrap();
    match change.action.as_str() {
        "create" => shelf.created(change.record),
        "update" => shelf.updated(change.record),
        "delete" => shelf.deleted(&change.record),
        _ => {}
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_a_frame() {
        let (event, data) = parse("id:abc\nevent:orders/*\ndata:{\"action\":\"create\"}\n\n");
        assert_eq!(event, "orders/*");
        assert_eq!(data, r#"{"action":"create"}"#);
    }

    #[test]
    fn reads_a_record_with_extra_fields() {
        let change: Change = serde_json::from_str(
            r#"{"action":"create","record":{"collectionId":"x","collectionName":"orders","id":"a","customer":"u","item":"i","quantity":2,"idempotency_key":"","company":"c","created":"t"}}"#,
        )
        .unwrap();
        assert_eq!((change.record.id.as_str(), change.record.quantity), ("a", 2));
    }
}
