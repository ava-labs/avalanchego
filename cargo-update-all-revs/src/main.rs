#![warn(clippy::all)]
#![warn(missing_docs, missing_debug_implementations, rust_2018_idioms)]
//! Release tool to update all versions of everything
//! inside the crate at the same time to the same version
use std::{collections::HashSet, fs, path::PathBuf};

use anyhow::{anyhow, bail, Context, Error};
use clap::Parser;
use toml_edit::{Document, Formatted, InlineTable, Item, KeyMut, Value};

#[derive(Debug, Parser)]
struct Args {
    /// how cargo invoked this; cargo chews up the first argument
    /// so this should be completely ignored
    #[clap(hide(true))]
    _cargo_invoked_as: String,
    /// Don't generate log entries
    #[arg(short, long)]
    quiet: bool,

    /// Don't write to the output file
    #[arg(short, long)]
    dryrun: bool,

    /// Fail if there were any differences
    #[arg(short, long)]
    check: bool,

    /// What the new version is supposed to be
    newver: String,
}

fn main() -> Result<(), Error> {
    let cli = Args::parse();

    // first read the top level Cargo.cli
    let base = std::fs::read_to_string("Cargo.toml")?;
    let doc = base.parse::<Document>()?;
    // get the [workspace] section
    let workspace = doc
        .get("workspace")
        .ok_or(anyhow!("No [workspace] section in top level"))?;
    // find the members array inside the workspace
    let members = workspace
        .get("members")
        .ok_or(anyhow!("No members in [workspace] section"))?
        .as_array()
        .ok_or(anyhow!("members must be an array"))?;

    // save these members into a hashmap for easy lookup later. We will
    // only change [dependencies] that point to one of these, and we need
    // to check each one to see if it's one we care about
    let members_lookup = members
        .iter()
        .map(|v| v.as_str().expect("member wasn't a string").to_string())
        .collect::<HashSet<String>>();

    let mut some_difference_found = false;

    // work on each subdirectory (each member of the workspace)
    for member in members {
        // calculate the path of the inner member
        let inner_path: PathBuf = [member.as_str().unwrap(), "Cargo.toml"].iter().collect();
        // and load into a parsed yaml document
        let inner = std::fs::read_to_string(&inner_path)
            .context(format!("Can't read {}", inner_path.display()))?;
        let mut inner = inner.parse::<Document>()?;

        // now find the [package] section
        let package = inner.get_mut("package").ok_or(anyhow!(format!(
            "no [package] section in {}",
            inner_path.display()
        )))?;
        // which contains: version = "xxx"; mutable since we might change it
        let version = package.get_mut("version");

        // keep track of if we changed anything, to avoid unnecessary rewrites
        let mut changed = false;

        // extract the value; we want a better error here in case we can't find
        // it or if the version couldn't be parsed as a string
        match version {
            None => {
                // TODO: We could just set the version...
                bail!(format!("No version in {}", inner_path.display()))
            }
            Some(Item::Value(v)) => {
                changed |= check_version(v, inner_path.display().to_string(), &cli);
            }
            Some(_) => bail!(format!(
                "version in {} wasn't a string",
                inner_path.display()
            )),
        }

        // now work on the [dependencies] section. We only care about
        // dependencies with names that are one of the subdirectories
        // we found when we parsed the members section at the top level
        // so we filter using the hashset created earlier
        // dependencies consist of a table of "name = { inline_table }"
        // entries. We skip those that don't have that format (the short
        // form of "name = version" for example)
        if let Some(deps) = inner.get_mut("dependencies") {
            if let Some(deps) = deps.as_table_mut() {
                // build an iterator of K,V pairs for each dependency
                // and do the filtering here for items in the members_lookup
                for dep in deps
                    .iter_mut()
                    .filter(|dep| members_lookup.contains(dep.0.get()))
                {
                    // call fixup_version for this dependency, which
                    // might make a change if the version was wrong
                    if let Some(inline_table) = dep.1.as_inline_table_mut() {
                        changed |= update_dep_ver(&dep.0, inline_table, &cli);
                    }
                }
            };
        }
        if changed {
            if !cli.quiet {
                println!("{} was changed", inner_path.display());
            }
            if !cli.dryrun {
                fs::write(inner_path, inner.to_string())?;
            }
        }
        some_difference_found |= changed;
    }
    if cli.check && some_difference_found {
        bail!("There were differences")
    } else {
        if cli.check {
            println!("All files had the correct version");
        }
        Ok(())
    }
}

/// Verify and/or update the version of a dependency
///
/// Given a dependency and the table of attributes, check the
/// "version" attribute and make sure it matches what we expect
/// from the command line arguments
///
/// * `key` - the name of this dependency
/// * `dep` - the table of K/V pairs describing the dependency
/// * `opts` - the command line arguments passed in
///
/// Returns true if any changes were made
fn update_dep_ver(key: &KeyMut<'_>, dep: &mut InlineTable, opts: &Args) -> bool {
    let v = dep.get_mut("version").unwrap();
    check_version(v, format!("dependency for {}", key.get()), opts)
}

/// Check and/or set the version
///
/// Check the version value provided and optionally
/// log and/or update it, based on the command line args
///
/// Arguments:
///
/// * `v` - the version to verify/change
/// * `source` - the text of where this version came from
/// * `opts` - the command line arguments
///
/// Returns `true` if a change was made, `false` otherwise
fn check_version<S: AsRef<str>>(v: &mut Value, source: S, opts: &Args) -> bool {
    if let Some(old) = v.as_str() {
        if old != opts.newver {
            if !opts.quiet {
                println!(
                    "Version for {} was {old} want {} (fixed)",
                    source.as_ref(),
                    opts.newver
                );
            }
            *v = Value::String(Formatted::new(opts.newver.clone()));
            return true;
        }
    }
    false
}
