//! Per-mode run history with frecency ranking (sqlite).

use std::{
    collections::HashMap,
    path::Path,
    sync::{Arc, Mutex},
    time::{SystemTime, UNIX_EPOCH},
};

use rusqlite::{Connection, params};
use wayle_core::paths::ConfigPaths;

use crate::error::Error;

const DAY: i64 = 86_400;

/// Persistent launch history: `(mode, entry) -> uses, last_used`.
#[derive(Debug, Clone)]
pub struct HistoryStore {
    connection: Arc<Mutex<Connection>>,
}

impl HistoryStore {
    /// Open (creating if needed) the launcher history DB at
    /// `$XDG_DATA_HOME/wayle/launcher.db`.
    ///
    /// # Errors
    ///
    /// Fails if the data dir can't be resolved/created or sqlite errors.
    pub fn open() -> Result<Self, Error> {
        Self::open_at(ConfigPaths::data_dir()?.join("launcher.db"))
    }

    /// Open a store at an explicit path (tests use `:memory:`).
    ///
    /// # Errors
    ///
    /// Fails on sqlite errors.
    pub fn open_at(path: impl AsRef<Path>) -> Result<Self, Error> {
        let connection = Connection::open(path)?;
        connection.execute_batch(
            "PRAGMA journal_mode = WAL;
             PRAGMA synchronous = NORMAL;
             CREATE TABLE IF NOT EXISTS history (
                 mode      TEXT NOT NULL,
                 entry     TEXT NOT NULL,
                 uses      INTEGER NOT NULL DEFAULT 1,
                 last_used INTEGER NOT NULL,
                 PRIMARY KEY (mode, entry)
             );",
        )?;
        Ok(Self {
            connection: Arc::new(Mutex::new(connection)),
        })
    }

    /// Record a launch, pruning the mode's history beyond `max_size`.
    ///
    /// # Errors
    ///
    /// Fails on sqlite errors.
    pub fn record(&self, mode: &str, entry: &str, max_size: u32) -> Result<(), Error> {
        self.record_at(mode, entry, now(), max_size)
    }

    /// [`record`](Self::record) with an explicit timestamp (testable).
    ///
    /// # Errors
    ///
    /// Fails on sqlite errors.
    pub fn record_at(
        &self,
        mode: &str,
        entry: &str,
        now_secs: i64,
        max_size: u32,
    ) -> Result<(), Error> {
        let connection = self.lock();
        connection.execute(
            "INSERT INTO history (mode, entry, uses, last_used) VALUES (?1, ?2, 1, ?3)
             ON CONFLICT (mode, entry) DO UPDATE SET uses = uses + 1, last_used = ?3",
            params![mode, entry, now_secs],
        )?;
        connection.execute(
            "DELETE FROM history WHERE mode = ?1 AND entry NOT IN (
                 SELECT entry FROM history WHERE mode = ?1
                 ORDER BY last_used DESC, uses DESC LIMIT ?2
             )",
            params![mode, max_size],
        )?;
        Ok(())
    }

    /// Remove one entry (rofi shift-delete).
    ///
    /// # Errors
    ///
    /// Fails on sqlite errors.
    pub fn remove(&self, mode: &str, entry: &str) -> Result<(), Error> {
        self.lock().execute(
            "DELETE FROM history WHERE mode = ?1 AND entry = ?2",
            params![mode, entry],
        )?;
        Ok(())
    }

    /// Frecency weights for a mode: `uses × recency bucket`. Higher = rank
    /// earlier. Used by drun to pre-order items before injection.
    ///
    /// # Errors
    ///
    /// Fails on sqlite errors.
    pub fn frecency(&self, mode: &str) -> Result<HashMap<String, f64>, Error> {
        self.frecency_at(mode, now())
    }

    /// [`frecency`](Self::frecency) with an explicit "now" (testable).
    ///
    /// # Errors
    ///
    /// Fails on sqlite errors.
    pub fn frecency_at(&self, mode: &str, now_secs: i64) -> Result<HashMap<String, f64>, Error> {
        let connection = self.lock();
        let mut statement =
            connection.prepare("SELECT entry, uses, last_used FROM history WHERE mode = ?1")?;
        let rows = statement.query_map(params![mode], |row| {
            Ok((
                row.get::<_, String>(0)?,
                row.get::<_, i64>(1)?,
                row.get::<_, i64>(2)?,
            ))
        })?;
        let mut weights = HashMap::new();
        for row in rows {
            let (entry, uses, last_used) = row?;
            #[allow(clippy::cast_precision_loss)]
            let weight = uses as f64 * recency_bucket(now_secs - last_used);
            weights.insert(entry, weight);
        }
        Ok(weights)
    }

    /// Entries of a mode, most recently used first (rofi run ordering).
    ///
    /// # Errors
    ///
    /// Fails on sqlite errors.
    pub fn recent(&self, mode: &str) -> Result<Vec<String>, Error> {
        let connection = self.lock();
        let mut statement = connection
            .prepare("SELECT entry FROM history WHERE mode = ?1 ORDER BY last_used DESC")?;
        let rows = statement.query_map(params![mode], |row| row.get::<_, String>(0))?;
        rows.map(|row| row.map_err(Error::from)).collect()
    }

    #[allow(clippy::unwrap_used)] // mutex poisoning = a panicked writer; propagating is pointless
    fn lock(&self) -> std::sync::MutexGuard<'_, Connection> {
        self.connection.lock().unwrap()
    }
}

fn now() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|duration| i64::try_from(duration.as_secs()).unwrap_or(i64::MAX))
        .unwrap_or(0)
}

/// Mozilla-style recency buckets.
fn recency_bucket(age_secs: i64) -> f64 {
    match age_secs {
        ..=0 => 1.0,
        age if age <= 4 * DAY => 1.0,
        age if age <= 14 * DAY => 0.7,
        age if age <= 31 * DAY => 0.5,
        age if age <= 90 * DAY => 0.3,
        _ => 0.1,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn store() -> HistoryStore {
        HistoryStore::open_at(":memory:").unwrap()
    }

    #[test]
    fn record_accumulates_uses() {
        let store = store();
        store.record_at("run", "htop", 1000, 25).unwrap();
        store.record_at("run", "htop", 2000, 25).unwrap();
        let weights = store.frecency_at("run", 2000).unwrap();
        assert!((weights["htop"] - 2.0).abs() < f64::EPSILON);
    }

    #[test]
    fn frecency_decays_with_age() {
        let store = store();
        store.record_at("drun", "old.desktop", 0, 25).unwrap();
        store.record_at("drun", "new.desktop", 100 * DAY, 25).unwrap();
        let weights = store.frecency_at("drun", 100 * DAY).unwrap();
        assert!(weights["new.desktop"] > weights["old.desktop"]);
        assert!((weights["old.desktop"] - 0.1).abs() < f64::EPSILON);
    }

    #[test]
    fn prune_keeps_most_recent() {
        let store = store();
        for (index, name) in ["a", "b", "c", "d"].iter().enumerate() {
            store.record_at("run", name, index as i64, 2).unwrap();
        }
        let recent = store.recent("run").unwrap();
        assert_eq!(recent, vec!["d", "c"]);
    }

    #[test]
    fn remove_deletes_entry() {
        let store = store();
        store.record_at("run", "htop", 0, 25).unwrap();
        store.remove("run", "htop").unwrap();
        assert!(store.recent("run").unwrap().is_empty());
    }

    #[test]
    fn modes_are_isolated() {
        let store = store();
        store.record_at("run", "htop", 0, 25).unwrap();
        store.record_at("drun", "firefox.desktop", 0, 25).unwrap();
        assert_eq!(store.recent("run").unwrap(), vec!["htop"]);
        assert_eq!(store.recent("drun").unwrap(), vec!["firefox.desktop"]);
    }
}
