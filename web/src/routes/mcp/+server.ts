// PAT pass-through for the MCP endpoint.
// Forwards to the Go API with only a `Bearer lkb_pat_…` credential and streams
// the response back (the API answers JSON only: stateless Streamable HTTP, no SSE).
// See $lib/server/patProxy for the rules.
import type { RequestHandler } from './$types';
import { forwardPatRequest } from '$lib/server/patForward';

export const fallback: RequestHandler = (event) => forwardPatRequest(event);
