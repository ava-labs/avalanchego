// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

// Supports making the logging operations a true runtime no-op
// Since we're a library, we can't really use the logging level
// static shortcut

#[cfg(feature = "logger")]
pub use log::{debug, error, info, trace, warn};

#[cfg(not(feature = "logger"))]
pub use noop_logger::{debug, error, info, trace, warn};

#[cfg(not(feature = "logger"))]
mod noop_logger {
    #[macro_export]
    /// A noop logger, when the logger feature is disabled
    macro_rules! noop {
        ($($arg:tt)+) => {};
    }

    pub use noop as debug;
    pub use noop as error;
    pub use noop as info;
    pub use noop as trace;
    pub use noop as warn;
}
