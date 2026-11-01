//! Typed configuration: `Config::default()`, then a TOML file, then the
//! environment. Credentials only ever come from the environment.

use std::env;
use std::error::Error;
use std::fmt::{self, Display};
use std::fs;
use std::io;
use std::path::PathBuf;
use std::str::FromStr;

use serde::Deserialize;

const DEFAULT_PATH: &str = "config.toml";

#[derive(Debug, Clone, Default, PartialEq, Deserialize)]
#[serde(default, deny_unknown_fields)]
pub struct Config {
    pub server: Server,
    pub cache: Cache,
    pub upstream: Upstream,
    #[serde(skip)]
    pub credentials: Credentials,
}

#[derive(Debug, Clone, PartialEq, Deserialize)]
#[serde(default, deny_unknown_fields)]
pub struct Server {
    pub port: u16,
}

impl Default for Server {
    fn default() -> Self {
        Self { port: 8082 }
    }
}

#[derive(Debug, Clone, PartialEq, Deserialize)]
#[serde(default, deny_unknown_fields)]
pub struct Cache {
    /// Keep at least this many rows, so small customers are served whole.
    pub min_rows: usize,
    /// Keep at most this many rows per customer, whatever half of them is.
    pub max_rows: usize,
}

impl Default for Cache {
    fn default() -> Self {
        Self { min_rows: 500, max_rows: 200_000 }
    }
}

#[derive(Debug, Clone, PartialEq, Deserialize)]
#[serde(default, deny_unknown_fields)]
pub struct Upstream {
    pub api: String,
    pub pocketbase: String,
}

impl Default for Upstream {
    fn default() -> Self {
        Self { api: "http://localhost:8080".into(), pocketbase: "http://localhost:8090".into() }
    }
}

#[derive(Clone, Default, PartialEq)]
pub struct Credentials {
    pub email: String,
    pub password: String,
}

// Written by hand so a logged config can't show the password.
impl fmt::Debug for Credentials {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("Credentials").field("email", &self.email).field("password", &"<redacted>").finish()
    }
}

#[derive(Debug)]
pub enum ConfigError {
    Read { path: PathBuf, source: io::Error },
    Parse { path: PathBuf, source: toml::de::Error },
    Env { key: &'static str, message: String },
    Missing(&'static str),
    Invalid(&'static str),
}

impl Display for ConfigError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Read { path, source } => write!(f, "{}: {source}", path.display()),
            Self::Parse { path, source } => write!(f, "{}: {source}", path.display()),
            Self::Env { key, message } => write!(f, "{key}: {message}"),
            Self::Missing(key) => write!(f, "{key} is required"),
            Self::Invalid(message) => f.write_str(message),
        }
    }
}

impl Error for ConfigError {
    fn source(&self) -> Option<&(dyn Error + 'static)> {
        match self {
            Self::Read { source, .. } => Some(source),
            Self::Parse { source, .. } => Some(source),
            _ => None,
        }
    }
}

impl FromStr for Config {
    type Err = toml::de::Error;

    fn from_str(raw: &str) -> Result<Self, Self::Err> {
        toml::from_str(raw)
    }
}

impl Config {
    /// Loads `READCACHE_CONFIG`, or `config.toml`. A missing file means the
    /// defaults, but an unknown key is an error, so a typo can't be ignored.
    pub fn load() -> Result<Self, ConfigError> {
        let path = PathBuf::from(env::var("READCACHE_CONFIG").unwrap_or_else(|_| DEFAULT_PATH.into()));
        let mut config = match fs::read_to_string(&path) {
            Ok(raw) => raw.parse().map_err(|source| ConfigError::Parse { path, source })?,
            Err(err) if err.kind() == io::ErrorKind::NotFound => Self::default(),
            Err(source) => return Err(ConfigError::Read { path, source }),
        };
        config.apply_env()?;
        config.validate()?;
        Ok(config)
    }

    fn apply_env(&mut self) -> Result<(), ConfigError> {
        override_from("READCACHE_PORT", &mut self.server.port)?;
        override_from("READCACHE_MIN_ROWS", &mut self.cache.min_rows)?;
        override_from("READCACHE_MAX_ROWS", &mut self.cache.max_rows)?;
        override_from("UPSTREAM", &mut self.upstream.api)?;
        override_from("PB_URL", &mut self.upstream.pocketbase)?;
        self.credentials.email = required("PB_EMAIL")?;
        self.credentials.password = required("PB_PASSWORD")?;
        Ok(())
    }

    fn validate(&self) -> Result<(), ConfigError> {
        if self.cache.max_rows == 0 || self.cache.min_rows > self.cache.max_rows {
            return Err(ConfigError::Invalid("cache.min_rows must not exceed cache.max_rows, which must not be 0"));
        }
        Ok(())
    }
}

fn override_from<T>(key: &'static str, slot: &mut T) -> Result<(), ConfigError>
where
    T: FromStr,
    T::Err: Display,
{
    if let Some(value) = env::var(key).ok().filter(|value| !value.is_empty()) {
        *slot = value.parse().map_err(|err: T::Err| ConfigError::Env { key, message: err.to_string() })?;
    }
    Ok(())
}

fn required(key: &'static str) -> Result<String, ConfigError> {
    env::var(key).ok().filter(|value| !value.is_empty()).ok_or(ConfigError::Missing(key))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn shipped_files_match_the_defaults() {
        for raw in [include_str!("../config.toml"), include_str!("../config.toml.example")] {
            assert_eq!(raw.parse::<Config>().unwrap(), Config::default());
        }
    }

    #[test]
    fn a_partial_file_keeps_the_other_defaults() {
        let config: Config = "[cache]\nmin_rows = 7\n".parse().unwrap();
        assert_eq!(config.cache.min_rows, 7);
        assert_eq!(config.cache.max_rows, Cache::default().max_rows);
        assert_eq!(config.server, Server::default());
    }

    #[test]
    fn rejects_unknown_keys_and_wrong_types() {
        for raw in ["[cache]\nmin_rowz = 1\n", "[nope]\n", "[server]\nport = \"http\"\n", "[server]\nport = 70000\n"] {
            assert!(raw.parse::<Config>().is_err(), "{raw}");
        }
    }

    #[test]
    fn credentials_cannot_come_from_the_file() {
        assert!("[credentials]\npassword = \"x\"\n".parse::<Config>().is_err());
    }

    #[test]
    fn debug_output_hides_the_password() {
        let credentials = Credentials { email: "a".into(), password: "hunter2".into() };
        assert!(!format!("{credentials:?}").contains("hunter2"));
    }

    #[test]
    fn min_rows_must_fit_under_max_rows() {
        let mut config = Config::default();
        config.cache.min_rows = config.cache.max_rows + 1;
        assert!(matches!(config.validate(), Err(ConfigError::Invalid(_))));
    }
}
