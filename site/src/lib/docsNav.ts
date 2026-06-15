// The pactify.dev docs directory — modeled on Dify's left-sidebar TOC, scaled to
// the content we actually have (three real pages: Introduction, Protocol,
// Onboarding). Cross-page items link to a page; same-page items deep-link to an
// anchored section on the Introduction page (the docs landing). Keep hrefs that
// point into /protocol pinned to real heading slugs.
export interface DocItem {
  label: string;
  href: string;
}
export interface DocGroup {
  title: string;
  items: DocItem[];
}

export const docsNav: DocGroup[] = [
  {
    title: "Getting started",
    items: [
      { label: "Introduction", href: "/introduction" },
      { label: "Key concepts", href: "/introduction#concepts" },
      { label: "How it works", href: "/introduction#how" },
      { label: "Quick start", href: "/introduction#quick-start" },
    ],
  },
  {
    title: "The protocol",
    items: [
      { label: "Overview", href: "/protocol" },
      { label: "The two rules", href: "/protocol#5-the-two-rules-the-pact" },
    ],
  },
  {
    title: "Agents",
    items: [
      { label: "Any agent, one protocol", href: "/introduction#agents" },
      { label: "Agent onboarding", href: "/onboarding" },
    ],
  },
  {
    title: "Orchestrate",
    items: [
      { label: "Run a squad", href: "/introduction#orchestrate" },
      { label: "Live dashboard", href: "/introduction#dashboard" },
    ],
  },
  {
    title: "Products",
    items: [{ label: "Base · Squad · Team", href: "/#ladder" }],
  },
  {
    title: "Resources",
    items: [
      { label: "Install", href: "/install.sh" },
      { label: "GitHub", href: "https://github.com/agentjoey/pactify" },
      { label: "Changelog", href: "https://github.com/agentjoey/pactify/releases" },
    ],
  },
];
