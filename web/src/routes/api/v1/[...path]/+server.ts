// PAT pass-through for the versioned public API (LOO-37): forwards any method to
// the Go API with only a `Bearer lkb_pat_…` credential (never the session cookie).
// See $lib/server/patProxy for the rules.
import type { RequestHandler } from './$types';
import { forwardPatRequest } from '$lib/server/patForward';

export const fallback: RequestHandler = (event) => forwardPatRequest(event);
