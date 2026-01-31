/**
 * Authentication routes
 *
 * Handles OAuth login flows for Google and GitHub providers
 */

import Router from '@koa/router';
import type { Context } from 'koa';
import { OAuth2Client, type TokenPayload } from 'google-auth-library';
import crypto from 'crypto';

import type { AppConfig } from '../config.js';
import type { User } from '../../shared/types.js';
import { isEmailAuthorized } from '../middleware/auth.js';

/**
 * OAuth provider type
 */
type OAuthProvider = 'google' | 'github';

/** Debug flag for auth routes */
let authRoutesDebugEnabled = false;

/**
 * Debug logger for auth routes
 */
function authRoutesDebug(message: string, data?: Record<string, unknown>): void {
  if (!authRoutesDebugEnabled) return;
  const timestamp = new Date().toISOString();
  if (data) {
    console.log(`[AUTH-ROUTES ${timestamp}] ${message}`, JSON.stringify(data, null, 2));
  } else {
    console.log(`[AUTH-ROUTES ${timestamp}] ${message}`);
  }
}

/**
 * Create authentication routes
 */
export function createAuthRouter(config: AppConfig): Router {
  const router = new Router();

  // Enable debug if config says so
  if (config.debug) {
    authRoutesDebugEnabled = true;
  }

  // Google OAuth client (lazily initialized)
  let googleClient: OAuth2Client | null = null;

  /**
   * Get or create Google OAuth client
   */
  function getGoogleClient(): OAuth2Client {
    if (!googleClient) {
      if (!config.auth.googleClientId || !config.auth.googleClientSecret) {
        throw new Error('Google OAuth not configured');
      }
      googleClient = new OAuth2Client({
        clientId: config.auth.googleClientId,
        clientSecret: config.auth.googleClientSecret,
        redirectUri: `${config.baseUrl}/auth/callback/google`,
      });
    }
    return googleClient;
  }

  /**
   * Generate random state for CSRF protection
   */
  function generateState(): string {
    return crypto.randomBytes(32).toString('hex');
  }

  /**
   * GET /auth/login
   * Show login page (redirect to specific provider or show options)
   */
  router.get('/login', async (ctx: Context) => {
    // Store return URL if provided
    const returnTo = ctx.query.returnTo as string | undefined;
    if (returnTo && ctx.session) {
      ctx.session.returnTo = returnTo;
    }

    // Redirect to login page component
    ctx.redirect('/login');
  });

  /**
   * GET /auth/login/:provider
   * Initiate OAuth flow for specified provider
   */
  router.get('/login/:provider', async (ctx: Context) => {
    const provider = ctx.params.provider as string;

    // Store return URL if provided
    const returnTo = ctx.query.returnTo as string | undefined;
    if (returnTo && ctx.session) {
      ctx.session.returnTo = returnTo;
    }

    if (provider === 'google') {
      // Check if Google OAuth is configured
      if (!config.auth.googleClientId) {
        ctx.redirect('/auth/error?message=Google+OAuth+not+configured');
        return;
      }

      const client = getGoogleClient();

      // Generate state for CSRF protection
      const state = generateState();
      if (ctx.session) {
        ctx.session.oauthState = state;
      }

      // Generate authorization URL
      const authUrl = client.generateAuthUrl({
        access_type: 'offline',
        scope: ['email', 'profile'],
        state,
        prompt: 'select_account',
      });

      ctx.redirect(authUrl);
      return;
    }

    if (provider === 'github') {
      // Check if GitHub OAuth is configured
      if (!config.auth.githubClientId) {
        ctx.redirect('/auth/error?message=GitHub+OAuth+not+configured');
        return;
      }

      // Generate state for CSRF protection
      const state = generateState();
      if (ctx.session) {
        ctx.session.oauthState = state;
      }

      const redirectUri = `${config.baseUrl}/auth/callback/github`;
      const authUrl =
        `https://github.com/login/oauth/authorize?` +
        `client_id=${encodeURIComponent(config.auth.githubClientId)}` +
        `&redirect_uri=${encodeURIComponent(redirectUri)}` +
        `&scope=${encodeURIComponent('user:email')}` +
        `&state=${encodeURIComponent(state)}`;

      ctx.redirect(authUrl);
      return;
    }

    // Unknown provider
    ctx.redirect('/auth/error?message=Unknown+OAuth+provider');
  });

  /**
   * GET /auth/callback/:provider
   * Handle OAuth callback from provider
   */
  router.get('/callback/:provider', async (ctx: Context) => {
    const provider = ctx.params.provider as OAuthProvider;
    const code = ctx.query.code as string | undefined;
    const state = ctx.query.state as string | undefined;
    const error = ctx.query.error as string | undefined;

    authRoutesDebug(`OAuth callback received`, {
      provider,
      hasCode: !!code,
      hasState: !!state,
      hasError: !!error,
      sessionExists: !!ctx.session,
      sessionOauthState: ctx.session?.oauthState ? 'present' : 'missing',
    });

    // Check for OAuth errors
    if (error) {
      const errorDescription = (ctx.query.error_description as string) || 'Authentication failed';
      authRoutesDebug(`OAuth error from provider`, { error, errorDescription });
      ctx.redirect(`/auth/error?message=${encodeURIComponent(errorDescription)}`);
      return;
    }

    // Verify code is present
    if (!code) {
      authRoutesDebug(`Missing authorization code`);
      ctx.redirect('/auth/error?message=Missing+authorization+code');
      return;
    }

    // Verify state matches (CSRF protection)
    if (ctx.session?.oauthState !== state) {
      authRoutesDebug(`State mismatch`, {
        expectedState: ctx.session?.oauthState ? 'present' : 'missing',
        receivedState: state ? 'present' : 'missing',
        match: ctx.session?.oauthState === state,
      });
      ctx.redirect('/auth/error?message=Invalid+state+parameter');
      return;
    }

    // Clear the state from session
    if (ctx.session) {
      delete ctx.session.oauthState;
    }

    try {
      let user: User;

      authRoutesDebug(`Exchanging code for tokens`, { provider });

      if (provider === 'google') {
        user = await handleGoogleCallback(code, config, getGoogleClient());
      } else if (provider === 'github') {
        user = await handleGitHubCallback(code, config);
      } else {
        ctx.redirect('/auth/error?message=Unknown+OAuth+provider');
        return;
      }

      authRoutesDebug(`User authenticated via OAuth`, {
        userId: user.id,
        userEmail: user.email,
        userName: user.name,
      });

      // Check if user's email domain is authorized
      if (!isEmailAuthorized(user.email, config.auth.authorizedDomains)) {
        authRoutesDebug(`User email domain not authorized`, { email: user.email });
        ctx.redirect('/auth/error?message=Your+email+domain+is+not+authorized');
        return;
      }

      // Store user in session
      authRoutesDebug(`Storing user in session`, {
        sessionExists: !!ctx.session,
        userEmail: user.email,
      });

      if (ctx.session) {
        ctx.session.user = user;
        authRoutesDebug(`User stored in session`, {
          sessionUser: ctx.session.user?.email,
          sessionKeys: Object.keys(ctx.session),
        });
      } else {
        authRoutesDebug(`WARNING: No session available to store user!`);
      }

      // Also set user in state for immediate use
      ctx.state.user = user;

      // Redirect to original destination or home
      const returnTo = ctx.session?.returnTo || '/';
      if (ctx.session) {
        delete ctx.session.returnTo;
      }

      authRoutesDebug(`Redirecting after successful login`, {
        returnTo,
        sessionUserAfterSet: ctx.session?.user?.email,
      });

      ctx.redirect(returnTo);
    } catch (err) {
      console.error('OAuth callback error:', err);
      authRoutesDebug(`OAuth callback error`, {
        error: err instanceof Error ? err.message : String(err),
      });
      const message = err instanceof Error ? err.message : 'Authentication failed';
      ctx.redirect(`/auth/error?message=${encodeURIComponent(message)}`);
    }
  });

  /**
   * POST /auth/logout
   * Clear session and log out
   */
  router.post('/logout', async (ctx: Context) => {
    // Clear session
    if (ctx.session) {
      ctx.session = null;
    }

    // Clear user from state
    ctx.state.user = undefined;

    // For AJAX requests, return JSON
    if (ctx.accepts('json')) {
      ctx.body = { success: true };
      return;
    }

    // For browser requests, redirect to login
    ctx.redirect('/auth/login');
  });

  /**
   * GET /auth/me
   * Get current user info
   */
  router.get('/me', async (ctx: Context) => {
    const user = ctx.state.user || ctx.session?.user;

    if (!user) {
      ctx.status = 401;
      ctx.body = { error: 'Not authenticated' };
      return;
    }

    ctx.body = { user };
  });

  /**
   * GET /auth/debug
   * Debug endpoint showing current auth state (only available when debug mode is enabled)
   */
  router.get('/debug', async (ctx: Context) => {
    // Only allow in debug mode
    if (!config.debug) {
      ctx.status = 404;
      ctx.body = { error: 'Not found' };
      return;
    }

    const cookieHeader = ctx.headers.cookie || '';
    const cookies = cookieHeader.split(';').map((c) => {
      const [name, ...rest] = c.trim().split('=');
      return {
        name,
        valueLength: rest.join('=').length,
        hasValue: rest.length > 0,
      };
    });

    ctx.body = {
      debug: true,
      timestamp: new Date().toISOString(),
      auth: {
        stateUser: ctx.state.user
          ? {
              id: ctx.state.user.id,
              email: ctx.state.user.email,
              name: ctx.state.user.name,
            }
          : null,
        sessionUser: ctx.session?.user
          ? {
              id: ctx.session.user.id,
              email: ctx.session.user.email,
              name: ctx.session.user.name,
            }
          : null,
        devToken: ctx.state.devToken ? 'present' : 'absent',
        devAuthEnabled: ctx.state.devAuthEnabled || false,
      },
      session: {
        exists: !!ctx.session,
        isNew: ctx.session?.isNew,
        keys: ctx.session ? Object.keys(ctx.session) : [],
        hasUser: !!ctx.session?.user,
        hasReturnTo: !!ctx.session?.returnTo,
        hasOauthState: !!ctx.session?.oauthState,
      },
      cookies: {
        header: cookieHeader ? 'present' : 'missing',
        count: cookies.length,
        names: cookies.map((c) => c.name).filter(Boolean),
        hasSessionCookie: cookieHeader.includes('scion_sess'),
      },
      config: {
        production: config.production,
        debug: config.debug,
        baseUrl: config.baseUrl,
        hubApiUrl: config.hubApiUrl,
        hasGoogleOAuth: !!config.auth.googleClientId,
        hasGitHubOAuth: !!config.auth.githubClientId,
        authorizedDomains: config.auth.authorizedDomains,
      },
    };
  });

  /**
   * GET /auth/error
   * Display authentication error page
   */
  router.get('/error', async (ctx: Context) => {
    const message = (ctx.query.message as string) || 'Authentication failed';

    // Redirect to login page with error
    ctx.redirect(`/login?error=${encodeURIComponent(message)}`);
  });

  return router;
}

/**
 * Handle Google OAuth callback
 */
async function handleGoogleCallback(
  code: string,
  config: AppConfig,
  client: OAuth2Client
): Promise<User> {
  // Exchange code for tokens
  const { tokens } = await client.getToken(code);

  if (!tokens.id_token) {
    throw new Error('No ID token received from Google');
  }

  // Verify the ID token
  const ticket = await client.verifyIdToken({
    idToken: tokens.id_token,
    audience: config.auth.googleClientId,
  });

  const payload = ticket.getPayload();
  if (!payload) {
    throw new Error('Invalid token payload');
  }

  return googlePayloadToUser(payload);
}

/**
 * Convert Google token payload to User
 */
function googlePayloadToUser(payload: TokenPayload): User {
  if (!payload.email) {
    throw new Error('Email not provided by Google');
  }

  return {
    id: `google:${payload.sub}`,
    email: payload.email,
    name: payload.name || payload.email.split('@')[0],
    avatar: payload.picture,
  };
}

/**
 * Handle GitHub OAuth callback
 */
async function handleGitHubCallback(code: string, config: AppConfig): Promise<User> {
  // Exchange code for access token
  // Note: redirect_uri must match exactly what was sent in the authorization request
  const redirectUri = `${config.baseUrl}/auth/callback/github`;
  const tokenResponse = await fetch('https://github.com/login/oauth/access_token', {
    method: 'POST',
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      client_id: config.auth.githubClientId,
      client_secret: config.auth.githubClientSecret,
      code,
      redirect_uri: redirectUri,
    }),
  });

  const tokenData = (await tokenResponse.json()) as {
    access_token?: string;
    error?: string;
    error_description?: string;
  };

  if (tokenData.error || !tokenData.access_token) {
    throw new Error(tokenData.error_description || 'Failed to get access token');
  }

  // Get user info
  const userResponse = await fetch('https://api.github.com/user', {
    headers: {
      Authorization: `Bearer ${tokenData.access_token}`,
      Accept: 'application/json',
    },
  });

  const userData = (await userResponse.json()) as {
    id: number;
    login: string;
    name?: string;
    email?: string;
    avatar_url?: string;
  };

  // Get email if not provided in user response
  let email = userData.email;
  if (!email) {
    const emailResponse = await fetch('https://api.github.com/user/emails', {
      headers: {
        Authorization: `Bearer ${tokenData.access_token}`,
        Accept: 'application/json',
      },
    });

    const emails = (await emailResponse.json()) as Array<{
      email: string;
      primary: boolean;
      verified: boolean;
    }>;

    const primaryEmail = emails.find((e) => e.primary && e.verified);
    if (primaryEmail) {
      email = primaryEmail.email;
    }
  }

  if (!email) {
    throw new Error('Could not get email from GitHub');
  }

  return {
    id: `github:${userData.id}`,
    email,
    name: userData.name || userData.login,
    avatar: userData.avatar_url,
  };
}
