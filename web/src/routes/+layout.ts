// LookupKB web is a client-rendered SPA (see docs/planning/01-architecture.md).
// Disable SSR and prerender the fallback shell; all data comes from the API.
export const ssr = false;
export const prerender = true;
