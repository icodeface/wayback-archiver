# Virtual Scroll Capture (DOMCollector)

## Problem

Modern SPAs like X.com use virtual scrolling: only visible DOM nodes are rendered, and nodes scrolled out of the viewport are removed. This causes archived snapshots to miss content the user has scrolled past.

Specific issues:
1. **Missing content**: The main tweet or top replies disappear when the user scrolls down before/during capture
2. **Loading overlays**: X.com's `#placeholder` (black screen with X logo) covers the archived page since there's no JS to hide it
3. **Broken layout**: Virtual scroll containers use `position: absolute` + `translateY`, which renders incorrectly in static archives

## Solution

### DOMCollector (`browser/src/dom-collector.ts`)

Tracks nodes removed by virtual scrolling and merges them back into the snapshot.

#### Collection Phase

```
Page load → collectorObserver starts immediately
         → MutationObserver watches childList + subtree on document.body
         → Removed nodes are stored by parent CSS selector
         → Re-added nodes (user scrolls back) are removed from the collection
```

Key filters:
- **MIN_NODE_SIZE (2KB)**: Skips loading skeletons, placeholders, and separator elements. Real content nodes (tweets, comments) are typically 10KB+.
- **MAX_COLLECTED_SIZE (10MB)**: Caps total memory usage. When reached, triggers a final update upload and stops monitoring.

#### Deduplication

Virtual scrolling re-renders nodes with different attributes each time (dynamic IDs, `aria-labelledby`, inline styles, `translateY` values). Exact `outerHTML` matching fails.

Solution: `textKey` — a stable dedup key composed of:
- Text content (all HTML tags stripped, whitespace normalized)
- Image `src` values (for image-only nodes)
- Dynamic content stripped: video timestamps (`0:03 / 2:14`), relative times (`·14h`, `·40m`)

This is used in two places:
1. **`removeOneMatch`**: When a node is re-added to the DOM, find and remove the matching collected entry using `textKey` comparison (via cached `removedKeys` array for performance).
2. **`mergeInto`**: Before appending a collected node, check if an equivalent already exists. Uses **global dedup** across all parent selectors and their sibling containers, because `nth-child` indices shift as virtual scrolling adds/removes nodes, causing the same logical parent to have different CSS selectors at different times.

#### Merge Logic (`mergeInto`)

```
1. Parse snapshot HTML into a DOM document
2. Build globalExisting map: textKey → count (from target parents' children AND sibling containers)
3. For each collected node:
   a. Compute textKey
   b. Skip if already exists (existCount > skippedCount)
   c. Otherwise append to parent and increment existCount
4. Fix virtual scroll layout (if any nodes were merged)
5. Serialize back to HTML string
```

#### Virtual Scroll Layout Fix (`fixVirtualScrollLayout`)

After merging, detects containers whose children use `position: absolute` + `translateY`:
1. Sorts all children by their `translateY` value
2. Converts each child to `position: static; transform: none`
3. Removes the container's large `min-height`

This converts the virtual scroll layout into normal document flow, so the archived page renders correctly without JavaScript.

### Observer Lifecycle (`browser/src/main.ts`)

```
initializeArchiver()
  │
  ├─ Create DOMCollector + collectorObserver (immediate)
  │
  ├─ Wait DOM_STABILITY_DELAY (2s)
  │
  ├─ prepareCapture()
  │    ├─ waitForDOMStable (up to 10s)
  │    ├─ serializeCSSOMToDOM()
  │    ├─ inlineLayoutStyles() → HTML snapshot
  │    └─ mergeInto() → merge collected nodes into snapshot
  │
  ├─ sendCapture() → POST /api/archive
  │    │  (collectorObserver still running, collecting removals)
  │    │
  │    └─ startDOMChangeMonitor()
  │         ├─ Disconnect collectorObserver
  │         ├─ New observer takes over (feeds DOMCollector + counts mutations)
  │         ├─ Every 5s: check mutations ≥ 10 → upload update
  │         └─ Stops after 5min timeout or collector reaches 10MB
  │
  └─ SPA navigation
       ├─ sendCapture() (save current page)
       ├─ resetState() (clear collector, rebuild collectorObserver)
       └─ Restart capture cycle for new page
```

### Server-side: Loading Overlay Removal (`server/internal/api/view_handler.go`)

SPA loading screens (e.g., X.com's `#placeholder`) are full-screen overlays hidden by JavaScript after the app loads. Since archived pages have no JS, these overlays permanently cover the content.

`removeLoadingOverlays` removes known overlay elements by ID:
- `#placeholder` — X.com's black loading screen with X logo
- `#ScriptLoadFailure` — X.com's "Something went wrong" error form

Uses nested `<div>` depth counting to correctly find the matching closing tag, handling arbitrarily deep nesting.

## Files Changed

| File | Changes |
|------|---------|
| `browser/src/dom-collector.ts` | DOMCollector: MIN_NODE_SIZE filter, textKey dedup, global dedup, fixVirtualScrollLayout |
| `browser/src/main.ts` | collectorObserver lifecycle, mergeInto in initial capture, no clear on monitor start |
| `server/internal/api/view_handler.go` | removeLoadingOverlays function |
