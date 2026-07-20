// Server-only OIDC client (the confidential relying party). Wraps openid-client
// for discovery, Authorization Code + PKCE, and refresh. The browser is only ever
// redirected here; tokens are exchanged server-side and sealed into the session
// cookie (see session.ts). Endpoints come from Authentik discovery
// (.well-known/openid-configuration) rather than being hardcoded.
import * as client from 'openid-client';
import { env } from '$env/dynamic/private';
import type { Session } from './session';

export interface OidcConfig {
	issuer: string;
	clientId: string;
	clientSecret: string;
	redirectUri: string;
	postLogoutRedirectUri: string;
	scopes: string;
	allowInsecure: boolean;
	sessionSecret: string;
	/** Session cookie lifetime (seconds); bounds how long a refresh chain lives. */
	sessionTtl: number;
}

/**
 * getConfig returns the OIDC configuration, or null when auth is not configured
 * (empty issuer) — mirroring the API's "empty issuer disables auth" so the web
 * app still boots for `pnpm dev` without Authentik.
 */
export function getConfig(): OidcConfig | null {
	const issuer = env.OIDC_ISSUER?.trim();
	if (!issuer) return null;

	const missing: string[] = [];
	const require = (name: string): string => {
		const v = env[name]?.trim();
		if (!v) missing.push(name);
		return v ?? '';
	};

	const cfg: OidcConfig = {
		issuer,
		clientId: require('OIDC_CLIENT_ID'),
		clientSecret: require('OIDC_CLIENT_SECRET'),
		redirectUri: require('OIDC_REDIRECT_URI'),
		postLogoutRedirectUri: env.OIDC_POST_LOGOUT_REDIRECT_URI?.trim() || '/',
		scopes: env.OIDC_SCOPES?.trim() || 'openid profile email groups',
		allowInsecure: env.OIDC_ALLOW_INSECURE === 'true',
		sessionSecret: require('SESSION_COOKIE_SECRET'),
		sessionTtl: Number(env.SESSION_TTL_SECONDS) || 60 * 60 * 12
	};

	if (missing.length > 0) {
		throw new Error(`OIDC is enabled but these env vars are missing: ${missing.join(', ')}`);
	}
	return cfg;
}

// Discovery is comparatively expensive; cache the Configuration per issuer for
// the process. openid-client caches the JWKS internally with the server TTL.
let discoveryPromise: Promise<client.Configuration> | null = null;

function discover(cfg: OidcConfig): Promise<client.Configuration> {
	if (!discoveryPromise) {
		discoveryPromise = client.discovery(
			new URL(cfg.issuer),
			cfg.clientId,
			cfg.clientSecret,
			undefined,
			cfg.allowInsecure ? { execute: [client.allowInsecureRequests] } : undefined
		);
	}
	return discoveryPromise;
}

export interface LoginStart {
	authorizationUrl: string;
	state: string;
	nonce: string;
	codeVerifier: string;
}

/** startLogin builds the Authentik authorization URL and the PKCE/state/nonce to stash. */
export async function startLogin(cfg: OidcConfig): Promise<LoginStart> {
	const config = await discover(cfg);
	const codeVerifier = client.randomPKCECodeVerifier();
	const codeChallenge = await client.calculatePKCECodeChallenge(codeVerifier);
	const state = client.randomState();
	const nonce = client.randomNonce();

	const url = client.buildAuthorizationUrl(config, {
		redirect_uri: cfg.redirectUri,
		scope: cfg.scopes,
		state,
		nonce,
		code_challenge: codeChallenge,
		code_challenge_method: 'S256'
	});

	return { authorizationUrl: url.href, state, nonce, codeVerifier };
}

function toGroups(value: unknown): string[] {
	if (!Array.isArray(value)) return [];
	return value.filter((g): g is string => typeof g === 'string' && g.length > 0);
}

// sessionFromTokens builds a Session from a token response. On refresh the
// server may omit the id_token, so identity (sub/email/groups) falls back to the
// previous session; on initial login there is no prior session and the id_token
// claims are required.
function sessionFromTokens(
	tokens: client.TokenEndpointResponse & client.TokenEndpointResponseHelpers,
	prev?: Session
): Session {
	const claims = tokens.claims();
	const sub = claims?.sub ? String(claims.sub) : prev?.sub;
	if (!sub) {
		throw new Error('token response missing id_token sub claim');
	}
	const expiresIn = tokens.expiresIn() ?? 0;
	return {
		accessToken: tokens.access_token,
		refreshToken: tokens.refresh_token ?? prev?.refreshToken,
		idToken: tokens.id_token ?? prev?.idToken,
		expiresAt: Date.now() + expiresIn * 1000,
		sub,
		email: typeof claims?.email === 'string' ? claims.email : (prev?.email ?? ''),
		groups: claims ? toGroups((claims as Record<string, unknown>).groups) : (prev?.groups ?? [])
	};
}

/** completeLogin validates the callback and exchanges the code for a session. */
export async function completeLogin(
	cfg: OidcConfig,
	currentUrl: URL,
	expected: { state: string; nonce: string; codeVerifier: string }
): Promise<Session> {
	const config = await discover(cfg);
	const tokens = await client.authorizationCodeGrant(config, currentUrl, {
		expectedState: expected.state,
		expectedNonce: expected.nonce,
		pkceCodeVerifier: expected.codeVerifier
	});
	return sessionFromTokens(tokens);
}

/** refreshSession exchanges the refresh token for a fresh access token. */
export async function refreshSession(cfg: OidcConfig, session: Session): Promise<Session> {
	if (!session.refreshToken) {
		throw new Error('no refresh token available');
	}
	const config = await discover(cfg);
	const tokens = await client.refreshTokenGrant(config, session.refreshToken);
	// Pass the prior session so identity + refresh token/id token survive a
	// response that (per spec) may omit them.
	return sessionFromTokens(tokens, session);
}

/** buildLogoutUrl builds the RP-initiated end-session URL that revokes the SSO session. */
export async function buildLogoutUrl(cfg: OidcConfig, idTokenHint?: string): Promise<string> {
	const config = await discover(cfg);
	const params: Record<string, string> = {
		post_logout_redirect_uri: cfg.postLogoutRedirectUri
	};
	if (idTokenHint) params.id_token_hint = idTokenHint;
	return client.buildEndSessionUrl(config, params).href;
}
