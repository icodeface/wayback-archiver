// DOMCollector tracks nodes removed by virtual scrolling and merges them
// back into the snapshot so we capture the union of all visible content.
//
// Uses arrays (not Sets) to preserve genuinely duplicated nodes
// (e.g. two retweets with identical outerHTML).

const MAX_COLLECTED_SIZE = 10 * 1024 * 1024; // 10MB cap
const MIN_NODE_SIZE = 2 * 1024; // Only collect nodes >= 2KB (filters out loading skeletons)

export class DOMCollector {
  // parent CSS selector -> array of removed node outerHTML (duplicates allowed)
  private removed: Map<string, string[]> = new Map();
  // Parallel arrays of text-based dedup keys for fast matching
  private removedKeys: Map<string, string[]> = new Map();
  private totalSize = 0;
  private _reachedLimit = false;

  /** Whether the collector has reached MAX_COLLECTED_SIZE */
  get reachedLimit(): boolean {
    return this._reachedLimit;
  }

  handleMutations(mutations: MutationRecord[]): void {
    for (const mutation of mutations) {
      // When a node is re-added (user scrolled back), remove ONE matching
      // entry from collected — not all of them, since duplicates are distinct items.
      for (const node of Array.from(mutation.addedNodes)) {
        if (node.nodeType !== Node.ELEMENT_NODE) continue;
        const html = (node as Element).outerHTML;
        if (html.length < MIN_NODE_SIZE) continue;
        this.removeOneMatch(html);
      }

      // Collect removed element nodes
      if (!mutation.target || !(mutation.target instanceof Element)) continue;
      const parentSel = this.selectorFor(mutation.target);

      for (const node of Array.from(mutation.removedNodes)) {
        if (node.nodeType !== Node.ELEMENT_NODE) continue;
        const html = (node as Element).outerHTML;
        // Skip small nodes (loading skeletons, placeholders) — real content is typically 2KB+
        if (html.length < MIN_NODE_SIZE) continue;
        if (this.totalSize + html.length > MAX_COLLECTED_SIZE) {
          this._reachedLimit = true;
          continue;
        }

        let arr = this.removed.get(parentSel);
        let keys = this.removedKeys.get(parentSel);
        if (!arr) {
          arr = [];
          keys = [];
          this.removed.set(parentSel, arr);
          this.removedKeys.set(parentSel, keys);
        }
        arr.push(html);
        keys!.push(this.textKey(html));
        this.totalSize += html.length;
      }
    }
  }

  /** Merge collected removed nodes into an HTML string and return the result. */
  mergeInto(html: string): string {
    if (this.removed.size === 0) return html;

    const doc = new DOMParser().parseFromString(html, 'text/html');
    let merged = 0;

    // Build a GLOBAL dedup map across all target parents AND their sibling containers.
    // nth-child selectors shift as virtual scrolling changes the DOM, so a node may
    // exist in the snapshot under a different parent than what the collector recorded.
    // By scanning sibling containers we cover these shifted-index cases.
    const globalExisting = new Map<string, number>();
    const parentElements = new Map<string, Element>();
    const scannedParents = new Set<Element>();

    for (const [selector] of this.removed) {
      const parent = doc.querySelector(selector);
      if (parent) {
        parentElements.set(selector, parent);
        this.scanChildren(parent, globalExisting, scannedParents);
        // Also scan sibling containers under the same grandparent
        if (parent.parentElement) {
          for (const sibling of Array.from(parent.parentElement.children)) {
            this.scanChildren(sibling, globalExisting, scannedParents);
          }
        }
      }
    }

    const globalSkipped = new Map<string, number>();

    for (const [selector, collected] of this.removed) {
      if (collected.length === 0) continue;
      const parent = parentElements.get(selector);
      if (!parent) continue;

      for (const nodeHTML of collected) {
        const key = this.textKey(nodeHTML);
        if (!key) continue; // skip empty-text nodes

        const existCount = globalExisting.get(key) || 0;
        const skippedSoFar = globalSkipped.get(key) || 0;

        if (skippedSoFar < existCount) {
          globalSkipped.set(key, skippedSoFar + 1);
          continue;
        }

        // Only allow merging this key once — increment existing but NOT skipped,
        // so subsequent duplicates see existCount > skippedSoFar and get skipped
        globalExisting.set(key, existCount + 1);

        const tpl = doc.createElement('template');
        tpl.innerHTML = nodeHTML;
        const child = tpl.content.firstElementChild;
        if (child) {
          parent.appendChild(child);
          merged++;
        }
      }
    }

    if (merged > 0) {
      console.log(`[Wayback] Merged ${merged} removed nodes back into snapshot`);
      // Fix virtual scroll layout: sort children by translateY and convert to normal flow
      this.fixVirtualScrollLayout(doc);
    }
    return doc.documentElement.outerHTML;
  }

  /**
   * Virtual scroll containers use position:absolute + translateY to position children.
   * After merging, we sort children by their translateY value and convert to static flow
   * so the archived page renders correctly without the virtual scroll JS.
   */
  private fixVirtualScrollLayout(doc: Document): void {
    // Find containers whose children use position:absolute + translateY
    // (the parent typically has position:relative + large min-height)
    const candidates = doc.querySelectorAll('[style*="position: relative"]');
    for (const container of Array.from(candidates)) {
      const children = Array.from(container.children) as HTMLElement[];
      if (children.length < 2) continue;

      // Check if children use translateY positioning
      const withTranslateY = children.filter(c =>
        c.style.position === 'absolute' && /translateY\([\d.]+px\)/.test(c.getAttribute('style') || '')
      );
      if (withTranslateY.length < children.length * 0.5) continue;

      // Sort all children by translateY value
      const sorted = children
        .map(c => {
          const match = (c.getAttribute('style') || '').match(/translateY\(([\d.]+)px\)/);
          return { el: c, y: match ? parseFloat(match[1]) : 0 };
        })
        .sort((a, b) => a.y - b.y);

      // Remove all children and re-append in sorted order
      // Convert from absolute positioning to normal document flow
      for (const { el } of sorted) {
        el.style.position = 'static';
        el.style.transform = 'none';
        el.style.width = '100%';
        container.appendChild(el);
      }

      // Remove the large min-height from the container
      container.setAttribute('style',
        (container.getAttribute('style') || '').replace(/min-height:\s*[\d.]+px;?/g, '')
      );

      console.log(`[Wayback] Fixed virtual scroll layout: ${sorted.length} children sorted`);
    }
  }

  clear(): void {
    this.removed.clear();
    this.removedKeys.clear();
    this.totalSize = 0;
    this._reachedLimit = false;
  }

  get collectedCount(): number {
    let n = 0;
    for (const [, arr] of this.removed) n += arr.length;
    return n;
  }

  /** Remove one matching entry from any parent's array (style-insensitive). */
  private removeOneMatch(html: string): void {
    const key = this.textKey(html);
    for (const [sel, keys] of this.removedKeys) {
      const idx = keys.indexOf(key);
      if (idx !== -1) {
        const arr = this.removed.get(sel)!;
        this.totalSize -= arr[idx].length;
        arr.splice(idx, 1);
        keys.splice(idx, 1);
        return;
      }
    }
  }

  /** Scan direct children of an element and add their textKeys to the dedup map. */
  private scanChildren(el: Element, map: Map<string, number>, visited: Set<Element>): void {
    if (visited.has(el)) return;
    visited.add(el);
    for (const child of Array.from(el.children)) {
      const key = this.textKey(child.outerHTML);
      if (key) {
        map.set(key, (map.get(key) || 0) + 1);
      }
    }
  }

  /** Extract a stable dedup key from HTML: text content + image sources.
   *  Strips dynamic content (video timestamps, relative times, ticker prices)
   *  that changes between virtual scroll re-renders. */
  private textKey(html: string): string {
    // Extract text (strip tags)
    let text = html.replace(/<[^>]+>/g, '').replace(/\s+/g, ' ').trim();
    // Strip video player timestamps: "0:03 / 2:14", "1:23:45 / 2:00:00"
    text = text.replace(/\d+:\d+(:\d+)?\s*\/\s*\d+:\d+(:\d+)?/g, '');
    // Strip relative timestamps: "·14h", "·40m", "·2s", "·3d" (X.com format: ·Nunit)
    text = text.replace(/(?<=·)\d+[smhd]/g, '');
    // Extract img src values for image-only nodes
    const imgs: string[] = [];
    const imgRe = /<img[^>]+src="([^"]+)"/g;
    let m: RegExpExecArray | null;
    while ((m = imgRe.exec(html)) !== null) {
      imgs.push(m[1]);
    }
    return text + (imgs.length > 0 ? '\0' + imgs.join('\0') : '');
  }

  /** Build a CSS selector path for an element. */
  private selectorFor(el: Element): string {
    const parts: string[] = [];
    let cur: Element | null = el;
    while (cur && cur !== document.documentElement) {
      if (cur.id) {
        parts.unshift('#' + CSS.escape(cur.id));
        break;
      }
      const parent: Element | null = cur.parentElement;
      if (!parent) break;
      const idx = Array.from(parent.children).indexOf(cur) + 1;
      parts.unshift(`${cur.tagName.toLowerCase()}:nth-child(${idx})`);
      cur = parent;
    }
    return parts.join(' > ') || 'body';
  }
}
