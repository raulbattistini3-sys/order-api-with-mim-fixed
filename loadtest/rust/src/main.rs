//! Load generator for profiling the gateway's defences — a benchmark for your
//! own services, the Rust twin of cmd/loadgen. It fires many signed
//! POST /orders requests at a target, with duplicates per logical order, and
//! reports throughput, latency and the status mix.
//!
//!   cargo run --release -- --target http://localhost:8081 --orders 500 --dupes 5 --concurrency 100
//!
//! Expected against the gateway: one order per key (the rest 409 or a replayed
//! 201), and 429s once the per-IP rate limit is exceeded.

use std::collections::BTreeMap;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use hmac::{Hmac, Mac};
use sha2::{Digest, Sha256};
use tokio::sync::{Mutex, Semaphore};

type HmacSha256 = Hmac<Sha256>;

struct Config {
    target: String,
    secret: String,
    customer: String,
    orders: usize,
    dupes: usize,
    concurrency: usize,
    timeout_secs: u64,
}

fn parse_args() -> Config {
    let mut c = Config {
        target: "http://localhost:8081".into(),
        secret: "dev-secret-change-me-0123456789".into(),
        customer: format!("load-rs-{}", now_millis()),
        orders: 500,
        dupes: 5,
        concurrency: 100,
        timeout_secs: 15,
    };
    let mut args = std::env::args().skip(1);
    while let Some(flag) = args.next() {
        let mut value = || args.next().expect("missing value for flag");
        match flag.as_str() {
            "--target" => c.target = value(),
            "--secret" => c.secret = value(),
            "--customer" => c.customer = value(),
            "--orders" => c.orders = value().parse().expect("orders"),
            "--dupes" => c.dupes = value().parse().expect("dupes"),
            "--concurrency" => c.concurrency = value().parse().expect("concurrency"),
            "--timeout" => c.timeout_secs = value().parse().expect("timeout"),
            other => panic!("unknown flag {other}"),
        }
    }
    c
}

fn now_millis() -> u128 {
    SystemTime::now().duration_since(UNIX_EPOCH).unwrap().as_millis()
}

// Same canonical string as internal/signing/signing.go.
fn sign(secret: &str, method: &str, uri: &str, ts: &str, key: &str, body: &str) -> String {
    let body_hash = hex::encode(Sha256::digest(body.as_bytes()));
    let canonical = format!("{method}\n{uri}\n{ts}\n{key}\n{body_hash}");
    let mut mac = HmacSha256::new_from_slice(secret.as_bytes()).unwrap();
    mac.update(canonical.as_bytes());
    hex::encode(mac.finalize().into_bytes())
}

fn rand_key() -> String {
    // A unique, charset-valid key; uniqueness is all that matters here.
    let nanos = SystemTime::now().duration_since(UNIX_EPOCH).unwrap().as_nanos();
    let tid = format!("{:?}", std::thread::current().id());
    hex::encode(Sha256::digest(format!("{nanos}-{tid}-{:p}", &nanos).as_bytes()))[..32].to_string()
}

#[tokio::main]
async fn main() {
    let cfg = Arc::new(parse_args());
    let total = cfg.orders * cfg.dupes;
    println!(
        "target={} customer={} orders={} dupes={} total={} concurrency={}",
        cfg.target, cfg.customer, cfg.orders, cfg.dupes, total, cfg.concurrency
    );

    let client = reqwest::Client::builder()
        .timeout(Duration::from_secs(cfg.timeout_secs))
        .pool_max_idle_per_host(cfg.concurrency)
        .build()
        .expect("http client");

    let sem = Arc::new(Semaphore::new(cfg.concurrency));
    let latencies: Arc<Mutex<Vec<u128>>> = Arc::new(Mutex::new(Vec::with_capacity(total)));
    let status: Arc<Mutex<BTreeMap<u16, u64>>> = Arc::new(Mutex::new(BTreeMap::new()));
    let replays = Arc::new(AtomicU64::new(0));

    let start = Instant::now();
    let mut handles = Vec::with_capacity(total);
    for i in 0..cfg.orders {
        let key = rand_key();
        let body = format!(
            "{{\"customer\":\"{}\",\"item\":\"load order {}\",\"quantity\":1}}",
            cfg.customer, i
        );
        for _ in 0..cfg.dupes {
            let permit = sem.clone().acquire_owned().await.unwrap();
            let (client, cfg, key, body) = (client.clone(), cfg.clone(), key.clone(), body.clone());
            let (latencies, status, replays) = (latencies.clone(), status.clone(), replays.clone());
            handles.push(tokio::spawn(async move {
                let _permit = permit;
                let (code, replayed, elapsed) = fire(&client, &cfg, &key, &body).await;
                latencies.lock().await.push(elapsed.as_micros());
                *status.lock().await.entry(code).or_insert(0) += 1;
                if replayed {
                    replays.fetch_add(1, Ordering::Relaxed);
                }
            }));
        }
    }
    for h in handles {
        let _ = h.await;
    }
    let elapsed = start.elapsed();

    report(elapsed, total, latencies, status, replays.load(Ordering::Relaxed)).await;
}

async fn fire(client: &reqwest::Client, cfg: &Config, key: &str, body: &str) -> (u16, bool, Duration) {
    const URI: &str = "/orders";
    let ts = now_millis().to_string();
    let signature = sign(&cfg.secret, "POST", URI, &ts, key, body);
    let begin = Instant::now();
    let res = client
        .post(format!("{}{}", cfg.target, URI))
        .header("Content-Type", "application/json")
        .header("Idempotency-Key", key)
        .header("X-Timestamp", &ts)
        .header("X-Signature", signature)
        .body(body.to_string())
        .send()
        .await;
    let elapsed = begin.elapsed();
    match res {
        Ok(r) => {
            let replayed = r
                .headers()
                .get("Idempotent-Replayed")
                .map(|v| v == "true")
                .unwrap_or(false);
            (r.status().as_u16(), replayed, elapsed)
        }
        Err(_) => (0, false, elapsed),
    }
}

async fn report(
    elapsed: Duration,
    total: usize,
    latencies: Arc<Mutex<Vec<u128>>>,
    status: Arc<Mutex<BTreeMap<u16, u64>>>,
    replays: u64,
) {
    let mut lat = latencies.lock().await;
    lat.sort_unstable();
    let pct = |p: f64| -> f64 {
        if lat.is_empty() {
            return 0.0;
        }
        let mut i = (p / 100.0 * lat.len() as f64) as usize;
        if i >= lat.len() {
            i = lat.len() - 1;
        }
        lat[i] as f64 / 1000.0 // micros -> millis
    };

    println!("\nelapsed      {:.0?}", elapsed);
    println!("throughput   {:.0} req/s", total as f64 / elapsed.as_secs_f64());
    println!(
        "latency      p50 {:.2}ms  p95 {:.2}ms  p99 {:.2}ms  max {:.2}ms",
        pct(50.0),
        pct(95.0),
        pct(99.0),
        pct(100.0)
    );
    println!("replayed     {replays}");
    println!("status");
    for (code, count) in status.lock().await.iter() {
        let label = if *code == 0 { "err".to_string() } else { code.to_string() };
        println!("  {label:<3} {count}");
    }
}
