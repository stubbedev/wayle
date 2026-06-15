mod circular_detection;
mod file_creation;
mod merging;

use std::{
    fs,
    path::{Path, PathBuf},
};

use circular_detection::CircularDetector;
use file_creation::create_default_config_file;
use merging::merge_toml_configs;
use toml::Value;

use super::error::{Error, IoOperation};
use crate::Config;

impl Config {
    /// Loads and deserializes configuration with imports resolved.
    ///
    /// # Errors
    ///
    /// Returns error on read failures, invalid TOML, import failures,
    /// deserialization failures, or circular imports.
    pub fn load_with_imports(path: &Path) -> Result<Config, Error> {
        let merged = Self::load_toml_with_imports(path)?;
        merged
            .try_into()
            .map_err(|source| Error::ConfigDeserialization { source })
    }

    /// Loads and merges configuration TOML with imports resolved.
    ///
    /// # Errors
    ///
    /// Returns error on read failures, invalid TOML, import failures,
    /// or circular imports.
    pub fn load_toml_with_imports(path: &Path) -> Result<Value, Error> {
        if !path.exists() {
            create_default_config_file(path)?;
        }

        let canonical_path = path.canonicalize().map_err(|source| Error::Io {
            operation: IoOperation::ResolvePath,
            path: path.to_path_buf(),
            source,
        })?;

        let mut detector = CircularDetector::new();
        Self::load_merged_toml(&canonical_path, path, &mut detector)
    }

    fn load_merged_toml(
        path: &Path,
        import_base: &Path,
        detector: &mut CircularDetector,
    ) -> Result<Value, Error> {
        detector.detect_circular_import(path)?;
        detector.push_to_chain(path);

        let main_config_content = fs::read_to_string(path)?;
        let import_paths = Self::extract_import_paths(&main_config_content, path)?;
        let imported_configs = Self::load_all_imports(import_base, &import_paths, detector)?;

        let main_config = Self::parse_config_str(&main_config_content, path)?;

        detector.pop_from_chain();
        Ok(merge_toml_configs(imported_configs, main_config))
    }

    /// Returns `true` if `path` should be parsed as YAML (`.yaml`/`.yml`),
    /// otherwise it is treated as TOML.
    fn is_yaml_path(path: &Path) -> bool {
        matches!(
            path.extension().and_then(|ext| ext.to_str()),
            Some("yaml" | "yml")
        )
    }

    /// Parses config file contents into a [`toml::Value`], selecting the
    /// format from the file extension. YAML configs are deserialized into the
    /// same value model as TOML so the rest of the merge/import pipeline is
    /// format-agnostic.
    fn parse_config_str(content: &str, path: &Path) -> Result<Value, Error> {
        if Self::is_yaml_path(path) {
            serde_yaml::from_str(content).map_err(|source| Error::YamlParse {
                path: path.to_path_buf(),
                source,
            })
        } else {
            toml::from_str(content).map_err(|source| Error::TomlParse {
                path: path.to_path_buf(),
                source,
            })
        }
    }

    fn load_all_imports(
        base_path: &Path,
        import_paths: &[String],
        detector: &mut CircularDetector,
    ) -> Result<Vec<Value>, Error> {
        import_paths
            .iter()
            .map(|import_path| {
                let resolved_path = Self::resolve_import_path(base_path, import_path)?;
                let canonical_import =
                    resolved_path
                        .canonicalize()
                        .map_err(|source| Error::Import {
                            path: resolved_path.clone(),
                            source: Box::new(Error::Io {
                                operation: IoOperation::ResolvePath,
                                path: resolved_path.clone(),
                                source,
                            }),
                        })?;

                Self::load_imported_file_with_tracking(&canonical_import, detector)
            })
            .collect()
    }

    fn load_imported_file_with_tracking(
        path: &Path,
        detector: &mut CircularDetector,
    ) -> Result<Value, Error> {
        detector.detect_circular_import(path)?;
        detector.push_to_chain(path);

        let result = Self::load_toml_file_with_imports(path, detector);
        detector.pop_from_chain();
        result
    }

    fn load_toml_file_with_imports(
        path: &Path,
        detector: &mut CircularDetector,
    ) -> Result<Value, Error> {
        let content = fs::read_to_string(path).map_err(|source| Error::Import {
            path: path.to_path_buf(),
            source: Box::new(Error::Io {
                operation: IoOperation::ReadFile,
                path: path.to_path_buf(),
                source,
            }),
        })?;
        let import_paths = Self::extract_import_paths(&content, path)?;
        let imported_configs = Self::load_all_imports(path, &import_paths, detector)?;

        let main_value = Self::parse_config_str(&content, path)?;

        Ok(merge_toml_configs(imported_configs, main_value))
    }

    fn extract_import_paths(config_content: &str, path: &Path) -> Result<Vec<String>, Error> {
        let value = if Self::is_yaml_path(path) {
            serde_yaml::from_str(config_content)
                .map_err(|source| Error::YamlParseInline { source })?
        } else {
            toml::from_str(config_content).map_err(|source| Error::TomlParseInline { source })?
        };

        let import_paths = if let Value::Table(table) = value {
            if let Some(Value::Array(imports)) = table.get("imports") {
                imports
                    .iter()
                    .filter_map(|v| v.as_str())
                    .map(|s| s.to_owned())
                    .collect::<Vec<String>>()
            } else {
                Vec::new()
            }
        } else {
            Vec::new()
        };

        Ok(import_paths)
    }

    fn resolve_import_path(base_path: &Path, import_path: &str) -> Result<PathBuf, Error> {
        let parent_dir = base_path.parent().ok_or_else(|| Error::ImportNoParent {
            path: base_path.to_path_buf(),
        })?;

        let import_path_buf = PathBuf::from(import_path);
        if import_path_buf.extension().is_some() {
            return Ok(parent_dir.join(import_path_buf));
        }

        // No extension given: prefer an existing YAML sibling, then YML, and
        // fall back to TOML (the historical default) so bare imports keep
        // working regardless of the importing file's format.
        for ext in ["yaml", "yml", "toml"] {
            let candidate = parent_dir.join(import_path_buf.with_extension(ext));
            if candidate.exists() {
                return Ok(candidate);
            }
        }

        Ok(parent_dir.join(import_path_buf.with_extension("toml")))
    }
}

#[cfg(test)]
mod tests {
    use std::path::Path;

    use crate::Config;

    #[test]
    fn detects_yaml_by_extension() {
        assert!(Config::is_yaml_path(Path::new("config.yaml")));
        assert!(Config::is_yaml_path(Path::new("config.yml")));
        assert!(!Config::is_yaml_path(Path::new("config.toml")));
        assert!(!Config::is_yaml_path(Path::new("config")));
    }

    #[test]
    fn yaml_and_toml_parse_to_equal_values() {
        let toml_src = "\
a = 1
b = \"hi\"

[section]
nested = true
list = [1, 2, 3]
";
        let yaml_src = "\
a: 1
b: hi
section:
  nested: true
  list:
    - 1
    - 2
    - 3
";
        let from_toml = Config::parse_config_str(toml_src, Path::new("config.toml")).unwrap();
        let from_yaml = Config::parse_config_str(yaml_src, Path::new("config.yaml")).unwrap();
        assert_eq!(from_toml, from_yaml);
    }

    #[test]
    fn extracts_imports_from_yaml() {
        let yaml_src = "\
imports:
  - bar.yaml
  - baz
";
        let imports = Config::extract_import_paths(yaml_src, Path::new("config.yaml")).unwrap();
        assert_eq!(imports, vec!["bar.yaml".to_string(), "baz".to_string()]);
    }

    #[test]
    fn yaml_parse_error_is_reported() {
        let bad_yaml = "a: : :\n  - broken";
        let err = Config::parse_config_str(bad_yaml, Path::new("config.yaml"));
        assert!(err.is_err());
    }
}
