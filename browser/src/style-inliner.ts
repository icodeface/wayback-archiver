// DOM 固化：将布局相关的 computed style 内联到元素的 style 属性中
// 这样即使 CSS 文件丢失，页面布局仍然能基本保持正确

const LAYOUT_PROPS = [
  // Box Model & Sizing
  'width', 'min-width', 'max-width',
  'height', 'min-height', 'max-height',
  'padding-top', 'padding-right', 'padding-bottom', 'padding-left',
  'margin-top', 'margin-right', 'margin-bottom', 'margin-left',
  'border-top-width', 'border-right-width', 'border-bottom-width', 'border-left-width',
  'box-sizing',
  // Positioning
  'position', 'top', 'right', 'bottom', 'left', 'z-index', 'float', 'clear',
  // Display & Flex
  'display',
  'flex-direction', 'flex-wrap', 'justify-content', 'align-items', 'align-self', 'align-content',
  'flex-grow', 'flex-shrink', 'flex-basis', 'order', 'gap', 'row-gap', 'column-gap',
  // Grid
  'grid-template-rows', 'grid-template-columns',
  'grid-auto-rows', 'grid-auto-columns', 'grid-auto-flow',
  'grid-row-start', 'grid-row-end', 'grid-column-start', 'grid-column-end',
  // Overflow & Transform
  'overflow-x', 'overflow-y', 'visibility', 'opacity',
  'transform',
];

// 跳过不需要内联的标签
const SKIP_TAGS = new Set([
  'SCRIPT', 'STYLE', 'LINK', 'META', 'TITLE', 'HEAD', 'BR', 'HR', 'NOSCRIPT',
  'BASE', 'COL', 'COLGROUP', 'PARAM', 'SOURCE', 'TRACK', 'WBR',
]);

// 默认值 — computed 值等于默认值时跳过，减少 HTML 体积
const DEFAULTS: Record<string, Set<string>> = {
  // Box Model
  'width': new Set(['auto']),
  'height': new Set(['auto']),
  'min-width': new Set(['auto', '0px']),
  'max-width': new Set(['none']),
  'min-height': new Set(['auto', '0px']),
  'max-height': new Set(['none']),
  'padding-top': new Set(['0px']),
  'padding-right': new Set(['0px']),
  'padding-bottom': new Set(['0px']),
  'padding-left': new Set(['0px']),
  'margin-top': new Set(['0px']),
  'margin-right': new Set(['0px']),
  'margin-bottom': new Set(['0px']),
  'margin-left': new Set(['0px']),
  'border-top-width': new Set(['0px']),
  'border-right-width': new Set(['0px']),
  'border-bottom-width': new Set(['0px']),
  'border-left-width': new Set(['0px']),
  'box-sizing': new Set(['content-box']),
  // Positioning
  'position': new Set(['static']),
  'top': new Set(['auto']),
  'right': new Set(['auto']),
  'bottom': new Set(['auto']),
  'left': new Set(['auto']),
  'z-index': new Set(['auto']),
  'float': new Set(['none']),
  'clear': new Set(['none']),
  // Flex
  'flex-direction': new Set(['row']),
  'flex-wrap': new Set(['nowrap']),
  'justify-content': new Set(['normal']),
  'align-items': new Set(['normal']),
  'align-self': new Set(['auto']),
  'align-content': new Set(['normal']),
  'flex-grow': new Set(['0']),
  'flex-shrink': new Set(['1']),
  'flex-basis': new Set(['auto']),
  'order': new Set(['0']),
  'gap': new Set(['normal', '0px']),
  'row-gap': new Set(['normal', '0px']),
  'column-gap': new Set(['normal', '0px']),
  // Grid
  'grid-template-rows': new Set(['none']),
  'grid-template-columns': new Set(['none']),
  'grid-auto-rows': new Set(['auto']),
  'grid-auto-columns': new Set(['auto']),
  'grid-auto-flow': new Set(['row']),
  'grid-row-start': new Set(['auto']),
  'grid-row-end': new Set(['auto']),
  'grid-column-start': new Set(['auto']),
  'grid-column-end': new Set(['auto']),
  // Overflow & Visual
  'overflow-x': new Set(['visible']),
  'overflow-y': new Set(['visible']),
  'visibility': new Set(['visible']),
  'opacity': new Set(['1']),
  'transform': new Set(['none']),
};

// display 的默认值取决于标签类型，需要按标签判断
// 列出常见的 inline 标签，其余默认为 block
const INLINE_TAGS = new Set([
  'A', 'ABBR', 'ACRONYM', 'B', 'BDO', 'BIG', 'CITE', 'CODE', 'DFN',
  'EM', 'FONT', 'I', 'IMG', 'INPUT', 'KBD', 'LABEL', 'Q', 'S',
  'SAMP', 'SELECT', 'SMALL', 'SPAN', 'STRIKE', 'STRONG', 'SUB', 'SUP',
  'TEXTAREA', 'TT', 'U', 'VAR',
]);
const TABLE_DISPLAY_TAGS: Record<string, string> = {
  'TABLE': 'table', 'TR': 'table-row', 'TD': 'table-cell', 'TH': 'table-cell',
  'THEAD': 'table-header-group', 'TBODY': 'table-row-group', 'TFOOT': 'table-footer-group',
  'CAPTION': 'table-caption', 'COL': 'table-column', 'COLGROUP': 'table-column-group',
};
const LIST_ITEM_TAG = 'LI';

/**
 * 在克隆的 DOM 上内联布局样式，返回带内联样式的 outerHTML。
 * 不修改原始 DOM，避免影响页面显示。
 *
 * 原理：从原始 DOM 读取 getComputedStyle()，写入到克隆节点的 style 属性。
 * 两棵树结构相同，通过 TreeWalker 同步遍历。
 */
export function inlineLayoutStyles(): string {
  const startTime = performance.now();
  let count = 0;

  // 视口尺寸 — 用于跳过 width/height 接近视口的值（来自 100%/100vh）
  const vw = window.innerWidth;
  const vh = window.innerHeight;

  // 深克隆整棵 DOM 树（不会触发 reflow/repaint）
  const clone = document.documentElement.cloneNode(true) as HTMLElement;

  // 同步遍历原始 DOM 和克隆 DOM
  const origWalker = document.createTreeWalker(
    document.documentElement,
    NodeFilter.SHOW_ELEMENT
  );
  const cloneWalker = document.createTreeWalker(
    clone,
    NodeFilter.SHOW_ELEMENT
  );

  let origNode: Node | null;
  let cloneNode: Node | null;
  while ((origNode = origWalker.nextNode()) && (cloneNode = cloneWalker.nextNode())) {
    const origEl = origNode as Element;
    const cloneEl = cloneNode as Element;
    if (SKIP_TAGS.has(origEl.tagName)) continue;

    // 从原始 DOM 读取 computed style（克隆节点不在文档中，无法 getComputedStyle）
    const computed = window.getComputedStyle(origEl);
    const existing = cloneEl.getAttribute('style') || '';
    const parts: string[] = [];

    for (const prop of LAYOUT_PROPS) {
      const value = computed.getPropertyValue(prop);
      if (!value) continue;
      if (DEFAULTS[prop]?.has(value)) continue;
      if (prop === 'display') {
        const tag = origEl.tagName;
        if (TABLE_DISPLAY_TAGS[tag] === value) continue;
        if (tag === LIST_ITEM_TAG && value === 'list-item') continue;
        if (INLINE_TAGS.has(tag) && value === 'inline') continue;
        if (!INLINE_TAGS.has(tag) && !TABLE_DISPLAY_TAGS[tag] && tag !== LIST_ITEM_TAG && value === 'block') continue;
      }
      // 跳过接近视口尺寸的 width/height（来自 100%/100vh，固化为像素值会截断内容）
      if (prop === 'width' || prop === 'height') {
        const px = parseFloat(value);
        if (px > 0) {
          const ref = prop === 'width' ? vw : vh;
          if (Math.abs(px - ref) / ref < 0.05) continue;
        }
      }
      parts.push(`${prop}:${value}`);
    }

    if (parts.length === 0) continue;

    const inlined = parts.join(';');
    cloneEl.setAttribute('style', existing ? existing + ';' + inlined : inlined);
    count++;
  }

  const elapsed = (performance.now() - startTime).toFixed(0);
  console.log(`[Wayback] Inlined layout styles for ${count} elements in ${elapsed}ms`);

  return clone.outerHTML;
}
