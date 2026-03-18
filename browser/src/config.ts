// Configuration constants for the Wayback archiver

export const CONFIG = {
  SERVER_URL: 'http://localhost:8080/api/archive',
  AUTH_PASSWORD: '',                    // Set this to enable HTTP Basic Auth
  ENABLE_COMPRESSION: false,            // Enable gzip compression for uploads (recommended for remote deployments)
  DOM_STABILITY_DELAY: 2000,        // ms to wait before starting capture
  SPA_TRANSITION_DELAY: 500,        // ms to wait after SPA navigation before starting collector (shorter than DOM_STABILITY_DELAY)
  MUTATION_OBSERVER_TIMEOUT: 10000, // max ms to wait for DOM stability
  DOM_STABLE_TIME: 1000,            // ms of no mutations to consider DOM stable
  TIMER_CLEAR_RANGE: 10000,         // number of timer IDs to clear when freezing
  UPDATE_CHECK_INTERVAL: 5000,      // ms between update checks
  UPDATE_MIN_MUTATIONS: 10,         // minimum mutation count before triggering an update
  UPDATE_MONITOR_TIMEOUT: 300000,   // max ms to keep monitoring DOM changes (5 minutes)
  REQUEST_TIMEOUT: 300000,          // max ms to wait for server response (5 minutes)
} as const;
