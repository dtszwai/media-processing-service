// Hash-based routing. The URL hash is the single source of truth for which
// tab is active. We use the hash (not pathname) so the static dev server
// doesn't have to rewrite unknown paths back to index.html.
//
// Hash format: #/tab[/segment[/segment...]] — e.g. #/submit, #/trace/job_abc

const DEFAULT_HASH = "#/me";

function parseHash(raw: string): { tab: string; params: string[] } {
  const stripped = raw.startsWith("#") ? raw.slice(1) : raw;
  const clean = stripped.startsWith("/") ? stripped.slice(1) : stripped;
  if (!clean) return { tab: "me", params: [] };
  const parts = clean.split("/").filter(Boolean);
  return { tab: parts[0] ?? "me", params: parts.slice(1) };
}

function currentHash(): string {
  if (typeof window === "undefined") return DEFAULT_HASH;
  return window.location.hash || DEFAULT_HASH;
}

function createRouteState() {
  let raw = $state(currentHash());

  if (typeof window !== "undefined") {
    window.addEventListener("hashchange", () => {
      raw = window.location.hash || DEFAULT_HASH;
    });
    // If the page loaded without a hash, normalize it so subsequent reads
    // are stable.
    if (!window.location.hash) {
      window.location.hash = DEFAULT_HASH;
    }
  }

  return {
    get raw() {
      return raw;
    },
    get parsed() {
      return parseHash(raw);
    },
    get tab() {
      return parseHash(raw).tab;
    },
    get params() {
      return parseHash(raw).params;
    },
  };
}

export const route = createRouteState();

export function navigate(path: string): void {
  const hash = path.startsWith("#") ? path : `#${path.startsWith("/") ? path : `/${path}`}`;
  if (typeof window !== "undefined") {
    window.location.hash = hash;
  }
}
