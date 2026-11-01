//! Read cache between the gateway and the API. It answers GET /me/orders from
//! each customer's newest half, kept in a mutable B-tree, and memoizes
//! /me/orders/summary. Everything else, and every miss, goes to the API, so the
//! API stays the only code that knows how to query orders.
//!
//! The gateway has already verified the session, and passes the result in
//! X-User-Id. Like the API's, this port must stay private.

mod config;
mod feed;
mod shelf;

use std::collections::HashMap;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, RwLock};
use std::time::Duration;

use axum::body::Body;
use axum::extract::{Request, State};
use axum::http::{header, HeaderMap, HeaderValue, Method, StatusCode};
use axum::response::{IntoResponse, Response};
use axum::Router;
use serde::Deserialize;

use config::Config;
use shelf::{decode_cursor, Key, Order, Shelf};

const USER_HEADER: &str = "x-user-id";
const COMPANY_HEADER: &str = "x-company-id";
const REF_HEADER: &str = "x-order-ref";
const CACHE_HEADER: &str = "x-read-cache";
const DEFAULT_PAGE: usize = 100; // defaultPage in internal/order/service.go
const MAX_PAGE: usize = 200; // maxPage
const LOAD_PAGE: usize = 200;

pub struct Cache {
    config: Config,
    http: reqwest::Client,
    shelves: RwLock<HashMap<String, Arc<RwLock<Shelf>>>>,
    /// Events are flowing, so a shelf can be trusted to stay current.
    subscribed: AtomicBool,
}

impl Cache {
    fn shelf(&self, user: &str) -> Option<Arc<RwLock<Shelf>>> {
        self.shelves.read().unwrap().get(user).cloned()
    }

    fn forget_all(&self) {
        self.subscribed.store(false, Ordering::SeqCst);
        self.shelves.write().unwrap().clear();
    }

    /// Starts a shelf for user, loaded in the background. A customer's first
    /// reads are misses, so they never wait on the load.
    fn start_load(self: &Arc<Self>, user: &str, company: &str) {
        if !self.subscribed.load(Ordering::SeqCst) {
            return;
        }
        let shelf = Arc::new(RwLock::new(Shelf::new(self.config.cache.min_rows, self.config.cache.max_rows)));
        {
            let mut shelves = self.shelves.write().unwrap();
            if shelves.contains_key(user) {
                return;
            }
            shelves.insert(user.to_string(), shelf.clone());
        }
        let cache = self.clone();
        let (user, company) = (user.to_string(), company.to_string());
        tokio::spawn(async move {
            if let Err(err) = cache.load(&shelf, &user, &company).await {
                eprintln!("readcache: load: {err}");
                cache.shelves.write().unwrap().remove(&user);
            }
        });
    }

    async fn load(&self, shelf: &RwLock<Shelf>, user: &str, company: &str) -> Result<(), String> {
        #[derive(Deserialize)]
        struct Totals {
            orders: usize,
        }
        #[derive(Deserialize)]
        struct OrderPage {
            orders: Vec<Order>,
            next_cursor: String,
        }

        let totals: Totals = self.read(user, company, "/me/orders/summary").await?;
        shelf.write().unwrap().set_total(totals.orders);

        let mut cursor = String::new();
        loop {
            let query = format!("/me/orders?limit={LOAD_PAGE}&cursor={cursor}");
            let page: OrderPage = self.read(user, company, &query).await?;
            let mut shelf = shelf.write().unwrap();
            shelf.extend(page.orders);
            if page.next_cursor.is_empty() || shelf.len() >= shelf.target() {
                shelf.finish_load(page.next_cursor.is_empty());
                return Ok(());
            }
            cursor = page.next_cursor;
        }
    }

    async fn read<T: serde::de::DeserializeOwned>(&self, user: &str, company: &str, path: &str) -> Result<T, String> {
        self.http
            .get(format!("{}{path}", self.config.upstream.api))
            .header(USER_HEADER, user)
            .header(COMPANY_HEADER, company)
            .send()
            .await
            .and_then(reqwest::Response::error_for_status)
            .map_err(|err| err.to_string())?
            .json()
            .await
            .map_err(|err| err.to_string())
    }
}

/// A request for a page, if this service can read it. The API answers anything
/// else, so a bad limit or cursor gets the API's own error.
fn page_request(query: Option<&str>) -> Option<(usize, Option<Key>)> {
    let mut limit = DEFAULT_PAGE;
    let mut after = None;
    for pair in query.unwrap_or("").split('&').filter(|p| !p.is_empty()) {
        match pair.split_once('=')? {
            ("limit", v) => limit = v.parse().ok().filter(|n| (1..=MAX_PAGE).contains(n))?,
            ("cursor", "") => {}
            ("cursor", v) => after = Some(decode_cursor(v)?),
            _ => return None,
        }
    }
    Some((limit, after))
}

fn json(body: Vec<u8>) -> Response {
    let headers = [
        (header::CONTENT_TYPE, HeaderValue::from_static("application/json")),
        (header::HeaderName::from_static(CACHE_HEADER), HeaderValue::from_static("hit")),
    ];
    (StatusCode::OK, headers, body).into_response()
}

fn header_value<'a>(headers: &'a HeaderMap, name: &str) -> &'a str {
    headers.get(name).and_then(|v| v.to_str().ok()).unwrap_or("")
}

/// Passes a request to the API as it came. The body is the API's own bytes.
async fn forward(cache: &Cache, method: Method, uri: &str, headers: &HeaderMap, body: Body) -> Result<(StatusCode, Option<HeaderValue>, Vec<u8>), String> {
    let body = axum::body::to_bytes(body, usize::MAX).await.map_err(|err| err.to_string())?;
    let mut upstream = cache.http.request(method, format!("{}{uri}", cache.config.upstream.api)).body(body);
    for name in [USER_HEADER, COMPANY_HEADER, REF_HEADER, header::CONTENT_TYPE.as_str()] {
        if let Some(value) = headers.get(name) {
            upstream = upstream.header(name, value);
        }
    }
    let res = upstream.send().await.map_err(|err| err.to_string())?;
    let status = StatusCode::from_u16(res.status().as_u16()).map_err(|err| err.to_string())?;
    let kind = res.headers().get(header::CONTENT_TYPE).cloned();
    let bytes = res.bytes().await.map_err(|err| err.to_string())?;
    Ok((status, kind, bytes.to_vec()))
}

fn answer(status: StatusCode, kind: Option<HeaderValue>, body: Vec<u8>) -> Response {
    let mut res = (status, body).into_response();
    if let Some(kind) = kind {
        res.headers_mut().insert(header::CONTENT_TYPE, kind);
    }
    res.headers_mut().insert(CACHE_HEADER, HeaderValue::from_static("miss"));
    res
}

async fn handle(State(cache): State<Arc<Cache>>, req: Request) -> Response {
    let (parts, body) = req.into_parts();
    let path = parts.uri.path().to_string();
    let uri = parts.uri.path_and_query().map(|p| p.to_string()).unwrap_or_else(|| path.clone());
    let user = header_value(&parts.headers, USER_HEADER).to_string();
    let company = header_value(&parts.headers, COMPANY_HEADER).to_string();
    let cacheable = parts.method == Method::GET && !user.is_empty();
    let trusted = cache.subscribed.load(Ordering::SeqCst);
    let shelf = if trusted { cache.shelf(&user) } else { None };

    if cacheable && trusted && path == "/me/orders" {
        if let Some((limit, after)) = page_request(parts.uri.query()) {
            match &shelf {
                Some(shelf) => {
                    if let Some(page) = shelf.read().unwrap().page(after.as_ref(), limit) {
                        return json(serde_json::to_vec(&page).expect("a page always serializes"));
                    }
                }
                None => cache.start_load(&user, &company),
            }
        }
    }

    let mut version = None;
    if cacheable && trusted && path == "/me/orders/summary" {
        match &shelf {
            Some(shelf) => {
                let shelf = shelf.read().unwrap();
                if let Some(body) = shelf.summary() {
                    return json(body.to_vec());
                }
                version = Some(shelf.version());
            }
            None => cache.start_load(&user, &company),
        }
    }

    match forward(&cache, parts.method, &uri, &parts.headers, body).await {
        Ok((status, kind, body)) => {
            if let (Some(version), Some(shelf), StatusCode::OK) = (version, &shelf, status) {
                shelf.write().unwrap().store_summary(version, body.clone());
            }
            answer(status, kind, body)
        }
        Err(err) => {
            eprintln!("readcache: forward: {err}");
            StatusCode::BAD_GATEWAY.into_response()
        }
    }
}

#[tokio::main]
async fn main() {
    let config = Config::load().unwrap_or_else(|err| {
        eprintln!("readcache: {err}");
        std::process::exit(1);
    });
    let addr = ("0.0.0.0", config.server.port);
    let http = reqwest::Client::builder()
        .timeout(Duration::from_secs(30))
        .pool_max_idle_per_host(64)
        .build()
        .expect("the HTTP client always builds");
    let cache = Arc::new(Cache { config, http, shelves: RwLock::new(HashMap::new()), subscribed: AtomicBool::new(false) });

    tokio::spawn(feed::follow(cache.clone()));

    let app = Router::new().fallback(handle).with_state(cache);
    let listener = tokio::net::TcpListener::bind(addr).await.expect("the port must be free");
    println!("readcache: listening on port {}", addr.1);
    axum::serve(listener, app)
        .with_graceful_shutdown(async {
            let _ = tokio::signal::ctrl_c().await;
        })
        .await
        .expect("the server stopped");
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reads_page_requests_the_api_would_accept() {
        assert_eq!(page_request(None), Some((100, None)));
        assert_eq!(page_request(Some("limit=20&cursor=")), Some((20, None)));
        let cursor = page_request(Some("cursor=dHxp")).unwrap().1;
        assert_eq!(cursor, Some(("t".to_string(), "i".to_string())));
    }

    #[test]
    fn leaves_everything_else_to_the_api() {
        for query in ["limit=0", "limit=201", "limit=x", "cursor=!!!", "other=1", "limit"] {
            assert_eq!(page_request(Some(query)), None, "{query}");
        }
    }
}
