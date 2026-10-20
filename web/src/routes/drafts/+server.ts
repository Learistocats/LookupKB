// The drafts list moved into the history panel; keep old links working.
import { redirect, type RequestHandler } from '@sveltejs/kit';

export const GET: RequestHandler = () => redirect(308, '/');
