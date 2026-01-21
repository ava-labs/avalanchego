//! Timer utilities for measuring elapsed time.

use std::time::{Duration, Instant};

/// A simple timer for measuring elapsed time.
///
/// # Examples
///
/// ```
/// use avalanche_utils::timer::Timer;
/// use std::thread::sleep;
/// use std::time::Duration;
///
/// let timer = Timer::start();
/// sleep(Duration::from_millis(10));
/// let elapsed = timer.elapsed();
/// assert!(elapsed >= Duration::from_millis(10));
/// ```
#[derive(Debug, Clone, Copy)]
pub struct Timer {
    start: Instant,
}

impl Timer {
    /// Starts a new timer.
    #[must_use]
    pub fn start() -> Self {
        Self {
            start: Instant::now(),
        }
    }

    /// Returns the elapsed time since the timer was started.
    #[must_use]
    pub fn elapsed(&self) -> Duration {
        self.start.elapsed()
    }

    /// Returns the elapsed time in milliseconds.
    #[must_use]
    pub fn elapsed_millis(&self) -> u128 {
        self.elapsed().as_millis()
    }

    /// Returns the elapsed time in microseconds.
    #[must_use]
    pub fn elapsed_micros(&self) -> u128 {
        self.elapsed().as_micros()
    }

    /// Returns the elapsed time in nanoseconds.
    #[must_use]
    pub fn elapsed_nanos(&self) -> u128 {
        self.elapsed().as_nanos()
    }

    /// Returns the elapsed time in seconds as a float.
    #[must_use]
    pub fn elapsed_secs_f64(&self) -> f64 {
        self.elapsed().as_secs_f64()
    }

    /// Resets the timer and returns the elapsed time.
    pub fn reset(&mut self) -> Duration {
        let elapsed = self.elapsed();
        self.start = Instant::now();
        elapsed
    }
}

impl Default for Timer {
    fn default() -> Self {
        Self::start()
    }
}

/// An adaptive timeout that adjusts based on response times.
///
/// This is useful for network operations where response times can vary.
///
/// # Examples
///
/// ```
/// use avalanche_utils::timer::AdaptiveTimeout;
/// use std::time::Duration;
///
/// let mut timeout = AdaptiveTimeout::new(
///     Duration::from_secs(1),   // initial
///     Duration::from_millis(100), // minimum
///     Duration::from_secs(30),  // maximum
/// );
///
/// // Observe some response times
/// timeout.observe(Duration::from_millis(500));
/// timeout.observe(Duration::from_millis(600));
///
/// // Get the current timeout value
/// let current = timeout.timeout();
/// ```
#[derive(Debug, Clone)]
pub struct AdaptiveTimeout {
    current: Duration,
    minimum: Duration,
    maximum: Duration,
    alpha: f64, // Smoothing factor (0-1)
}

impl AdaptiveTimeout {
    /// Creates a new adaptive timeout.
    ///
    /// # Arguments
    ///
    /// * `initial` - The initial timeout value
    /// * `minimum` - The minimum allowed timeout
    /// * `maximum` - The maximum allowed timeout
    #[must_use]
    pub fn new(initial: Duration, minimum: Duration, maximum: Duration) -> Self {
        Self {
            current: initial.clamp(minimum, maximum),
            minimum,
            maximum,
            alpha: 0.1, // Default smoothing factor
        }
    }

    /// Sets the smoothing factor (alpha).
    ///
    /// Higher values (closer to 1) make the timeout more responsive to recent observations.
    /// Lower values (closer to 0) make the timeout more stable.
    ///
    /// # Panics
    ///
    /// Panics if alpha is not in the range [0, 1].
    pub fn with_alpha(mut self, alpha: f64) -> Self {
        assert!((0.0..=1.0).contains(&alpha), "alpha must be in [0, 1]");
        self.alpha = alpha;
        self
    }

    /// Observes a response time and updates the timeout accordingly.
    pub fn observe(&mut self, response_time: Duration) {
        // Exponential moving average
        let current_secs = self.current.as_secs_f64();
        let observed_secs = response_time.as_secs_f64();

        // Add some margin (2x the observed time)
        let target_secs = observed_secs * 2.0;

        let new_secs = current_secs * (1.0 - self.alpha) + target_secs * self.alpha;
        let new_duration = Duration::from_secs_f64(new_secs);

        self.current = new_duration.clamp(self.minimum, self.maximum);
    }

    /// Returns the current timeout value.
    #[must_use]
    pub fn timeout(&self) -> Duration {
        self.current
    }

    /// Resets the timeout to the initial value.
    pub fn reset(&mut self, initial: Duration) {
        self.current = initial.clamp(self.minimum, self.maximum);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_timer() {
        let timer = Timer::start();
        std::thread::sleep(Duration::from_millis(1));
        let elapsed = timer.elapsed();
        assert!(elapsed >= Duration::from_millis(1));
    }

    #[test]
    fn test_timer_reset() {
        let mut timer = Timer::start();
        std::thread::sleep(Duration::from_millis(1));
        let first = timer.reset();
        assert!(first >= Duration::from_millis(1));

        std::thread::sleep(Duration::from_millis(1));
        let second = timer.elapsed();
        assert!(second >= Duration::from_millis(1));
        assert!(second < first + Duration::from_millis(10)); // Should be similar
    }

    #[test]
    fn test_adaptive_timeout_new() {
        let timeout = AdaptiveTimeout::new(
            Duration::from_secs(1),
            Duration::from_millis(100),
            Duration::from_secs(30),
        );
        assert_eq!(timeout.timeout(), Duration::from_secs(1));
    }

    #[test]
    fn test_adaptive_timeout_clamp_initial() {
        // Initial below minimum
        let timeout = AdaptiveTimeout::new(
            Duration::from_millis(10),
            Duration::from_millis(100),
            Duration::from_secs(30),
        );
        assert_eq!(timeout.timeout(), Duration::from_millis(100));

        // Initial above maximum
        let timeout = AdaptiveTimeout::new(
            Duration::from_secs(60),
            Duration::from_millis(100),
            Duration::from_secs(30),
        );
        assert_eq!(timeout.timeout(), Duration::from_secs(30));
    }

    #[test]
    fn test_adaptive_timeout_observe() {
        let mut timeout = AdaptiveTimeout::new(
            Duration::from_secs(1),
            Duration::from_millis(100),
            Duration::from_secs(30),
        );

        // Observe fast responses - should decrease timeout
        for _ in 0..100 {
            timeout.observe(Duration::from_millis(50));
        }
        assert!(timeout.timeout() < Duration::from_secs(1));

        // Observe slow responses - should increase timeout
        for _ in 0..100 {
            timeout.observe(Duration::from_secs(5));
        }
        assert!(timeout.timeout() > Duration::from_secs(1));
    }

    #[test]
    fn test_adaptive_timeout_bounds() {
        let mut timeout = AdaptiveTimeout::new(
            Duration::from_secs(1),
            Duration::from_millis(500),
            Duration::from_secs(10),
        );

        // Very fast observations shouldn't go below minimum
        for _ in 0..1000 {
            timeout.observe(Duration::from_millis(1));
        }
        assert!(timeout.timeout() >= Duration::from_millis(500));

        // Very slow observations shouldn't go above maximum
        for _ in 0..1000 {
            timeout.observe(Duration::from_secs(100));
        }
        assert!(timeout.timeout() <= Duration::from_secs(10));
    }
}
