export interface Finding {
  rule: "overflow" | "icon-wrap" | "control-overlap" | "tap-target";
  selector: string;
  detail: string;
}

interface Located extends Finding {
  anchor: string | undefined;
}

/**
 * Runs inside the page, so it may reference nothing outside its own body:
 * Playwright ships it to the browser as source. `width` is the viewport the
 * caller asked for, not `innerWidth`, which a mobile browser widens to fit.
 */
export function audit(width: number): Finding[] {
  const findings: Located[] = [];

  // A selector without IDs, so one pattern repeated down a page groups as one
  // finding; `anchor` keeps the nearest ID, so the first instance can be found.
  const describe = (element: Element) => {
    const parts: string[] = [];

    for (let node: Element | null = element; node && parts.length < 4; node = node.parentElement) {
      if (node === document.body) {
        break;
      }

      const classes = Array.from(node.classList)
        .filter((name) => !name.includes(":") && !name.includes("["))
        .slice(0, 2)
        .map((name) => `.${name}`)
        .join("");

      parts.unshift(`${node.tagName.toLowerCase()}${classes}`);
    }

    return parts.join(" > ");
  };

  const anchor = (element: Element) => element.closest("[id]")?.id;

  const visible = (element: Element) => {
    const rect = element.getBoundingClientRect();

    // 1×1 is the screen-reader-only pattern, not a visible box.
    return rect.width > 1 && rect.height > 1 && getComputedStyle(element).visibility !== "hidden";
  };

  // A fixed box cannot widen the document, and a clipping one contains its
  // children, so overflow below either is not the page's.
  const contains = (element: Element) => {
    const { overflowX, position } = getComputedStyle(element);

    return overflowX !== "visible" || position === "fixed";
  };

  const intersects = (a: DOMRect, b: DOMRect) =>
    a.left < b.right - 1 && b.left < a.right - 1 && a.top < b.bottom - 1 && b.top < a.bottom - 1;

  // The element that escapes, not its descendants: one whose box ends past the
  // viewport while its parent's does not, outside any container that scrolls
  // or clips it.
  if (document.documentElement.scrollWidth > width) {
    const all = Array.from(document.body.querySelectorAll("*"));

    for (const element of all) {
      const right = element.getBoundingClientRect().right + window.scrollX;
      const parent = element.parentElement;

      if (right <= width + 1 || !visible(element) || !parent) {
        continue;
      }

      if (
        parent !== document.body &&
        parent.getBoundingClientRect().right + window.scrollX > width + 1
      ) {
        continue;
      }

      let contained = false;

      for (
        let node: Element | null = element;
        node && node !== document.body;
        node = node.parentElement
      ) {
        if (node !== element ? contains(node) : getComputedStyle(node).position === "fixed") {
          contained = true;
          break;
        }
      }

      if (!contained) {
        findings.push({
          rule: "overflow",
          selector: describe(element),
          anchor: anchor(element),
          detail: `ends ${Math.round(right - width)}px past the ${width}px viewport`,
        });
      }
    }
  }

  // An icon link breaks only when it is longer than the line it sits on, and
  // then never between the icon and its first word. In a table cell any break
  // is wrong: the column can widen, because the table scrolls.
  for (const link of document.querySelectorAll<HTMLAnchorElement>("main a")) {
    const hasIcon =
      link.querySelector("svg") !== null || getComputedStyle(link, "::before").content !== "none";

    if (
      !hasIcon ||
      !visible(link) ||
      !["inline", "inline-block"].includes(getComputedStyle(link).display)
    ) {
      continue;
    }

    const range = document.createRange();
    range.selectNodeContents(link);

    const text = Array.from(range.getClientRects()).filter(({ width }) => width > 0);
    const lines = new Set(text.map(({ top }) => Math.round(top)));

    if (lines.size < 2) {
      continue;
    }

    let container = link.parentElement;

    while (container && ["inline", "contents"].includes(getComputedStyle(container).display)) {
      container = container.parentElement;
    }

    const { whiteSpace } = link.style;
    link.style.whiteSpace = "nowrap";
    const needed = link.getBoundingClientRect().width;
    link.style.whiteSpace = whiteSpace;

    const style = container ? getComputedStyle(container) : null;
    const available =
      !container || !style || link.closest("td, th")
        ? Infinity
        : container.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight);
    const iconAlone =
      text[0].top - link.getClientRects()[0].top >
      parseFloat(getComputedStyle(link).lineHeight) / 2;

    if (needed <= available || iconAlone) {
      findings.push({
        rule: "icon-wrap",
        selector: describe(link),
        anchor: anchor(link),
        detail: `"${link.textContent?.trim()}" breaks across ${lines.size} lines${iconAlone ? ", away from its icon" : ""}`,
      });
    }
  }

  // A control laid over a code block must not cover its text.
  for (const button of document.querySelectorAll("main button")) {
    if (!visible(button) || getComputedStyle(button).position !== "absolute") {
      continue;
    }

    const container = button.parentElement;
    const box = button.getBoundingClientRect();

    for (const line of container?.querySelectorAll("pre .line") ?? []) {
      const range = document.createRange();
      range.selectNodeContents(line);

      if (Array.from(range.getClientRects()).some((rect) => intersects(rect, box))) {
        findings.push({
          rule: "control-overlap",
          selector: describe(button),
          anchor: anchor(button),
          detail: `covers "${line.textContent?.trim().slice(0, 40)}"`,
        });
        break;
      }
    }
  }

  // WCAG 2.5.8 on touch widths: an undersized target passes when a 24px circle
  // on its centre meets no other target and no other such circle. Inline links
  // in running text are exempt, but still count as targets to keep clear of.
  if (width < 768) {
    const targets = Array.from(document.querySelectorAll("button, a, input, [role='tab']"))
      .filter(visible)
      .map((element) => {
        const box = element.getBoundingClientRect();

        return {
          element,
          box,
          x: box.left + box.width / 2,
          y: box.top + box.height / 2,
          inline:
            element.tagName === "A" &&
            ["inline", "inline-block"].includes(getComputedStyle(element).display),
        };
      });
    const undersized = targets.filter(
      ({ box, inline }) => !inline && (box.width < 24 || box.height < 24),
    );
    const related = (a: Element, b: Element) => a === b || a.contains(b) || b.contains(a);

    for (const target of undersized) {
      const crowded =
        targets.some(({ element, box }) => {
          const dx = Math.max(box.left - target.x, 0, target.x - box.right);
          const dy = Math.max(box.top - target.y, 0, target.y - box.bottom);

          return !related(element, target.element) && Math.hypot(dx, dy) < 12;
        }) ||
        undersized.some(
          ({ element, x, y }) =>
            !related(element, target.element) && Math.hypot(x - target.x, y - target.y) < 24,
        );

      if (crowded) {
        findings.push({
          rule: "tap-target",
          selector: describe(target.element),
          anchor: anchor(target.element),
          detail: `${Math.round(target.box.width)}×${Math.round(target.box.height)}px, too close to another target`,
        });
      }
    }
  }

  const groups = new Map<string, Located & { count: number }>();

  for (const finding of findings) {
    const key = `${finding.rule} ${finding.selector}`;
    const seen = groups.get(key);

    if (seen) {
      seen.count++;
    } else {
      groups.set(key, { ...finding, count: 1 });
    }
  }

  return Array.from(groups.values(), ({ rule, selector, detail, anchor, count }) => {
    const where = [anchor ? `first at #${anchor}` : "", count > 1 ? `${count} in all` : ""]
      .filter(Boolean)
      .join(", ");

    return { rule, selector, detail: where ? `${detail} (${where})` : detail };
  });
}
