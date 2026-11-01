//! One customer's memoized orders: the newest half, in a mutable B-tree.
//!
//! The rows always run unbroken from the customer's newest order down to the
//! floor, so a page that fits above the floor is exactly what the database
//! would return. Anything below it is a miss. To order by something else, such
//! as price, change `Key`.

use std::collections::BTreeMap;

use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use base64::Engine;
use serde::{Deserialize, Serialize};

/// The same fields, in the same order, as the Go API's order JSON.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct Order {
    pub id: String,
    pub customer: String,
    pub item: String,
    pub quantity: i64,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub idempotency_key: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub company: String,
    #[serde(default)]
    pub created: String,
}

/// (created, id), the API's sort. The tree ascends, so pages read it backwards.
pub type Key = (String, String);

fn key(o: &Order) -> Key {
    (o.created.clone(), o.id.clone())
}

/// Must match encodeCursor in internal/order/service.go.
pub fn encode_cursor(k: &Key) -> String {
    URL_SAFE_NO_PAD.encode(format!("{}|{}", k.0, k.1))
}

/// None for a cursor this service can't read, which the API then rejects.
pub fn decode_cursor(s: &str) -> Option<Key> {
    let raw = String::from_utf8(URL_SAFE_NO_PAD.decode(s).ok()?).ok()?;
    let (created, id) = raw.split_once('|')?;
    (!created.is_empty() && !id.is_empty()).then(|| (created.to_string(), id.to_string()))
}

#[derive(Serialize)]
pub struct Page {
    pub orders: Vec<Order>,
    pub next_cursor: String,
}

pub struct Shelf {
    rows: BTreeMap<Key, Order>,
    total: usize,
    min_rows: usize,
    max_rows: usize,
    /// The first load finished, so the rows can be served.
    ready: bool,
    /// The rows reach the customer's oldest order.
    complete: bool,
    summary: Option<Vec<u8>>,
    /// Bumped by every change, so a summary fetched across one isn't stored.
    version: u64,
}

impl Shelf {
    pub fn new(min_rows: usize, max_rows: usize) -> Self {
        Shelf { rows: BTreeMap::new(), total: 0, min_rows, max_rows, ready: false, complete: false, summary: None, version: 0 }
    }

    /// How many rows to keep: the first half, but enough for a small customer's
    /// first page to be served from memory.
    pub fn target(&self) -> usize {
        (self.total / 2).max(self.min_rows).min(self.total).min(self.max_rows)
    }

    pub fn set_total(&mut self, total: usize) {
        self.total = total;
    }

    pub fn len(&self) -> usize {
        self.rows.len()
    }

    /// Adds a page from the first load, which walks down from the newest row.
    pub fn extend(&mut self, orders: Vec<Order>) {
        for o in orders {
            self.rows.insert(key(&o), o);
        }
    }

    /// Ends the first load. `reached_end` says it ran out of orders to read.
    pub fn finish_load(&mut self, reached_end: bool) {
        self.complete = reached_end;
        self.ready = true;
        self.trim();
    }

    fn touch(&mut self) {
        self.summary = None;
        self.version += 1;
    }

    /// Applies a created order. Below the floor it would leave a gap in the
    /// rows, so only the total moves.
    pub fn created(&mut self, o: Order) {
        self.total += 1;
        self.touch();
        let k = key(&o);
        let above_floor = self.rows.keys().next().is_none_or(|floor| k > *floor);
        if !self.ready || self.complete || above_floor {
            self.rows.insert(k, o);
        }
        self.trim();
    }

    pub fn updated(&mut self, o: Order) {
        self.touch();
        if let Some(row) = self.rows.get_mut(&key(&o)) {
            *row = o;
        }
    }

    pub fn deleted(&mut self, o: &Order) {
        self.total = self.total.saturating_sub(1);
        self.touch();
        self.rows.remove(&key(o));
    }

    fn trim(&mut self) {
        if !self.ready {
            return;
        }
        let target = self.target();
        while self.rows.len() > target {
            self.rows.pop_first();
            self.complete = false;
        }
    }

    /// One page, or None when memory can't answer it exactly.
    pub fn page(&self, after: Option<&Key>, limit: usize) -> Option<Page> {
        if !self.ready {
            return None;
        }
        let rows: Box<dyn Iterator<Item = (&Key, &Order)>> = match after {
            Some(k) => Box::new(self.rows.range(..k.clone()).rev()),
            None => Box::new(self.rows.iter().rev()),
        };
        let mut page: Vec<(&Key, &Order)> = rows.take(limit + 1).collect();
        let more = page.len() > limit;
        if !more && !self.complete {
            return None; // the page crosses the floor
        }
        page.truncate(limit);
        let next_cursor = if more { encode_cursor(page[limit - 1].0) } else { String::new() };
        Some(Page { orders: page.into_iter().map(|(_, o)| o.clone()).collect(), next_cursor })
    }

    pub fn summary(&self) -> Option<&[u8]> {
        self.summary.as_deref()
    }

    pub fn version(&self) -> u64 {
        self.version
    }

    /// Stores a summary read while the shelf was at `version`.
    pub fn store_summary(&mut self, version: u64, body: Vec<u8>) {
        if version == self.version {
            self.summary = Some(body);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn order(n: usize) -> Order {
        Order {
            id: format!("id{n:05}"),
            customer: "u1".into(),
            item: format!("item{n}"),
            quantity: 1,
            idempotency_key: String::new(),
            company: String::new(),
            created: format!("2026-11-01 10:00:{:02}.{:03}Z", n / 1000, n % 1000),
        }
    }

    /// A loaded shelf of orders 0..total, of which the newest are n-1, n-2...
    fn loaded(total: usize, min_rows: usize) -> Shelf {
        let mut s = Shelf::new(min_rows, usize::MAX);
        s.set_total(total);
        let target = s.target();
        s.extend((0..total).rev().take(target).map(order).collect());
        s.finish_load(target == total);
        s
    }

    #[test]
    fn keeps_the_newest_half() {
        let s = loaded(100, 0);
        assert_eq!(s.len(), 50);
        assert_eq!(s.page(None, 1).unwrap().orders[0].id, "id00099");
        assert!(s.rows.values().all(|o| o.id.as_str() >= "id00050"));
    }

    #[test]
    fn small_customers_are_kept_whole() {
        let s = loaded(10, 500);
        let p = s.page(None, 100).unwrap();
        assert_eq!(p.orders.len(), 10);
        assert_eq!(p.next_cursor, "");
    }

    #[test]
    fn max_rows_caps_a_huge_customer() {
        let mut s = Shelf::new(0, 10);
        s.set_total(1000);
        assert_eq!(s.target(), 10);
    }

    #[test]
    fn nothing_is_served_before_the_load_ends() {
        let mut s = Shelf::new(0, usize::MAX);
        s.set_total(2);
        s.extend(vec![order(1), order(0)]);
        assert!(s.page(None, 1).is_none());
    }

    #[test]
    fn pages_walk_down_to_the_floor_then_miss() {
        let s = loaded(100, 0);
        let first = s.page(None, 20).unwrap();
        assert_eq!(first.orders.len(), 20);
        let cursor = decode_cursor(&first.next_cursor).unwrap();
        let second = s.page(Some(&cursor), 20).unwrap();
        assert_eq!(second.orders[0].id, "id00079");
        let third = s.page(Some(&decode_cursor(&second.next_cursor).unwrap()), 20);
        assert!(third.is_none(), "only 10 rows remain above the floor");
    }

    #[test]
    fn a_page_ending_exactly_on_the_last_row_says_so() {
        let s = loaded(4, 500);
        let p = s.page(None, 4).unwrap();
        assert_eq!((p.orders.len(), p.next_cursor.as_str()), (4, ""));
    }

    #[test]
    fn a_page_needs_one_row_past_it_to_know_there_are_more() {
        let s = loaded(100, 0);
        assert!(s.page(None, 49).is_some());
        assert!(s.page(None, 50).is_none());
    }

    #[test]
    fn new_orders_land_on_top_and_the_half_keeps_its_shape() {
        let mut s = loaded(100, 0);
        for n in 100..120 {
            s.created(order(n));
        }
        assert_eq!(s.len(), 60);
        assert_eq!(s.page(None, 1).unwrap().orders[0].id, "id00119");
    }

    #[test]
    fn an_order_below_the_floor_leaves_no_gap() {
        let mut s = loaded(100, 0);
        let mut old = order(1);
        old.id = "old".into();
        s.created(old);
        assert!(s.rows.values().all(|o| o.id != "old"));
    }

    #[test]
    fn events_during_the_load_are_not_lost() {
        let mut s = Shelf::new(0, usize::MAX);
        s.set_total(4);
        s.created(order(100));
        s.extend(vec![order(3), order(2)]);
        s.finish_load(false);
        assert_eq!(s.page(None, 1).unwrap().orders[0].id, "id00100");
    }

    #[test]
    fn updates_and_deletes_apply() {
        let mut s = loaded(4, 500);
        let mut changed = order(3);
        changed.quantity = 9;
        s.updated(changed);
        assert_eq!(s.page(None, 1).unwrap().orders[0].quantity, 9);
        s.deleted(&order(3));
        assert_eq!(s.page(None, 1).unwrap().orders[0].id, "id00002");
    }

    #[test]
    fn a_change_drops_the_summary_and_blocks_a_stale_one() {
        let mut s = loaded(4, 500);
        let before = s.version();
        s.store_summary(before, b"{}".to_vec());
        assert!(s.summary().is_some());
        s.created(order(50));
        assert!(s.summary().is_none());
        s.store_summary(before, b"stale".to_vec());
        assert!(s.summary().is_none());
    }

    #[test]
    fn cursors_round_trip_and_match_go() {
        let k = ("2026-11-01 10:00:00.000Z".to_string(), "abc".to_string());
        assert_eq!(decode_cursor(&encode_cursor(&k)), Some(k));
        assert_eq!(encode_cursor(&("t".to_string(), "i".to_string())), "dHxp");
        assert_eq!(decode_cursor("!!!"), None);
        assert_eq!(decode_cursor("dA"), None); // "t", no separator
    }

    #[test]
    fn orders_serialize_like_the_api() {
        let json = serde_json::to_string(&order(1)).unwrap();
        assert_eq!(
            json,
            r#"{"id":"id00001","customer":"u1","item":"item1","quantity":1,"created":"2026-11-01 10:00:00.001Z"}"#
        );
    }
}
