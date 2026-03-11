// Configuration constants for the Wayback archiver

export const CONFIG = {
  SERVER_URL: 'http://localhost:8080/api/archive',
  AUTH_PASSWORD: '',                    // Set this to enable HTTP Basic Auth
  DOM_STABILITY_DELAY: 2000,        // ms to wait before starting capture
  MUTATION_OBSERVER_TIMEOUT: 10000, // max ms to wait for DOM stability
  DOM_STABLE_TIME: 1000,            // ms of no mutations to consider DOM stable
  TIMER_CLEAR_RANGE: 10000,         // number of timer IDs to clear when freezing
  UPDATE_DEBOUNCE_DELAY: 5000,      // ms of no DOM changes before triggering an update
  UPDATE_MIN_MUTATIONS: 10,         // minimum mutation count before triggering an update
  UPDATE_MONITOR_TIMEOUT: 30000,    // max ms to keep monitoring DOM changes
} as const;
