// LookupKB web is server-rendered by the Node adapter so hooks.server.ts can
// resolve the session and gate routes. Prerendering is disabled: every page depends on
// the authenticated session resolved per request.
export const ssr = true;
export const prerender = false;
