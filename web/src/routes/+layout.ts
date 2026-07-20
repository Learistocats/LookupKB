// LookupKB web is server-rendered by the Node adapter so hooks.server.ts can run
// the OIDC flow and gate routes. Prerendering is disabled: every page depends on
// the authenticated session resolved per request.
export const ssr = true;
export const prerender = false;
