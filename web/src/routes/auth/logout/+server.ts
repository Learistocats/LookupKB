// Logout: clear the session cookie and return to the login page. The API holds no
// session, so dropping the cookie (and with it the token) is the whole logout.
// Accepts POST (CSRF-safer for a mutating action) and GET (convenience link).
import { redirect, type RequestHandler } from '@sveltejs/kit';
import { clearSessionCookie } from '$lib/server/session';

const logout: RequestHandler = ({ cookies }) => {
	clearSessionCookie(cookies);
	redirect(302, '/login');
};

export const POST = logout;
export const GET = logout;
